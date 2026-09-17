import { MailboxRequestError, onAccountFailure, onAuthFailure } from '../api'
import { onMailboxWorkspaceClear } from '../session'
import type { Account, Credential, CredentialKind } from '../types'
import { credentialScope, readCredential } from './credentials'
import { otpSeconds } from './guards'

type Field = {
  value?: Credential
  receivedAt?: number
  error?: unknown
  loading?: boolean
}
type Snapshot = Partial<Record<CredentialKind, Field>>
let active = 0
const queue: (() => void)[] = []
export function credentialRequest<T>(
  signal: AbortSignal,
  run: () => Promise<T>
): Promise<T> {
  return new Promise((resolve, reject) => {
    const start = () => {
      signal.removeEventListener('abort', cancel)
      if (signal.aborted) {
        reject(new DOMException('Aborted', 'AbortError'))
        return
      }
      active++
      void run()
        .then(resolve, reject)
        .finally(() => {
          active--
          queue.shift()?.()
        })
    }
    const cancel = () => {
      const index = queue.indexOf(start)
      if (index >= 0) queue.splice(index, 1)
      reject(new DOMException('Aborted', 'AbortError'))
    }
    if (signal.aborted) {
      cancel()
      return
    }
    if (active < 4) start()
    else {
      queue.push(start)
      signal.addEventListener('abort', cancel, { once: true })
    }
  })
}

// Page-owned memory only. Visibility controls OTP refresh, not cache lifetime.
const entries = new Map<string, VisibleCredentials>()
let pageGeneration = 0
export function credentialPageGeneration() {
  return pageGeneration
}
export function clearPageCredentials() {
  pageGeneration++
  for (const entry of entries.values()) entry.stop()
  entries.clear()
}
export function failPageCredentials(error: unknown) {
  for (const entry of entries.values()) entry.stop(error)
}
export function reconcilePageCredentials(
  accounts: Account[],
  generation: number
) {
  if (generation !== pageGeneration) return
  const current = new Map(accounts.map((account) => [account.id, account]))
  for (const entry of entries.values()) {
    const account = current.get(entry.account.id)
    if (
      !account ||
      credentialScope(account) !== credentialScope(entry.account) ||
      account.version !== entry.account.version
    ) {
      entry.stop(new MailboxRequestError('mailbox_assignment_changed', 409))
    }
  }
}
onAuthFailure(clearPageCredentials)
onMailboxWorkspaceClear(clearPageCredentials)
onAccountFailure((id, error) => {
  for (const entry of entries.values()) {
    if (entry.account.id === id) entry.stop(error)
  }
})
if (typeof window !== 'undefined') {
  window.addEventListener('offline', () =>
    failPageCredentials(new MailboxRequestError('mailbox_request_failed', 0))
  )
}
export class VisibleCredentials {
  private generation = pageGeneration
  snapshot: Snapshot = {}
  private listeners = new Set<() => void>()
  private controller = new AbortController()
  private timers = new Set<ReturnType<typeof setTimeout>>()
  private started = false
  private viewers = 0
  private kinds: CredentialKind[]
  get readable() {
    return !this.controller.signal.aborted
  }
  constructor(
    readonly account: Account,
    private csrf: string
  ) {
    this.kinds =
      account.account_type === 'opening'
        ? ['password', 'otp', 'card', 'cvv']
        : ['password', 'otp']
  }
  private emit() {
    for (const listener of this.listeners) listener()
  }
  subscribe(listener: () => void, active = true) {
    this.listeners.add(listener)
    const deactivate = active ? this.activate() : undefined
    return () => {
      this.listeners.delete(listener)
      deactivate?.()
    }
  }
  activate() {
    this.viewers++
    if (!this.started) {
      this.started = true
      for (const kind of this.kinds) void this.load(kind)
    } else {
      const otp = this.snapshot.otp
      if (
        this.readable &&
        otp?.value?.available !== false &&
        otp?.value &&
        otpSeconds(otp.value, otp.receivedAt ?? 0, Date.now()) <= 0
      ) {
        void this.load('otp')
      }
    }
    let active = true
    return () => {
      if (active) this.viewers--
      active = false
    }
  }
  private later(run: () => void, delay: number) {
    const timer = setTimeout(() => {
      this.timers.delete(timer)
      if (!this.controller.signal.aborted) run()
    }, delay)
    this.timers.add(timer)
  }
  stop(
    error: unknown = new MailboxRequestError('mailbox_credentials_revoked', 403)
  ) {
    this.controller.abort()
    for (const timer of this.timers) clearTimeout(timer)
    this.timers.clear()
    this.snapshot = Object.fromEntries(
      this.kinds.map((kind) => [kind, { error }])
    )
    this.emit()
  }
  async load(kind: CredentialKind) {
    if (this.controller.signal.aborted || this.snapshot[kind]?.loading) return
    const controller = this.controller
    this.snapshot = { ...this.snapshot, [kind]: { loading: true } }
    this.emit()
    try {
      const value = await credentialRequest(controller.signal, () =>
        readCredential(this.csrf, this.account, kind, controller.signal)
      )
      if (controller.signal.aborted) return
      const receivedAt = Date.now()
      this.snapshot = { ...this.snapshot, [kind]: { value, receivedAt } }
      this.emit()
      if ((kind === 'otp' || kind === 'cvv') && value.available !== false) {
        const remaining = otpSeconds(value, receivedAt, receivedAt)
        if (remaining > 0) {
          this.later(() => {
            this.snapshot = {
              ...this.snapshot,
              [kind]: {
                value: { available: true, server_time: 0 },
                receivedAt: Date.now(),
              },
            }
            this.emit()
            if (kind === 'otp' && this.viewers) void this.load(kind)
          }, remaining * 1000)
        }
      }
    } catch (error) {
      if (!controller.signal.aborted) {
        this.snapshot = { ...this.snapshot, [kind]: { error } }
        this.emit()
      }
    }
  }
  retry(kind: CredentialKind) {
    if (this.generation !== pageGeneration) return
    if (this.controller.signal.aborted) {
      this.controller = new AbortController()
      this.snapshot = {}
      for (const field of this.kinds) void this.load(field)
    } else void this.load(kind)
  }
  async copy(field: 'email' | 'expiry' | CredentialKind) {
    const kind = field === 'expiry' ? 'card' : field
    if (kind === 'otp' || kind === 'cvv') {
      const current = this.snapshot[kind]
      if (
        !current?.value ||
        (!(kind === 'cvv' && current.value.persistent) &&
          otpSeconds(current.value, current.receivedAt ?? 0, Date.now()) <= 0)
      ) {
        throw new MailboxRequestError(
          kind === 'cvv'
            ? 'mailbox_cvv_unavailable'
            : 'mailbox_credentials_unavailable',
          400
        )
      }
    }
    if (
      this.controller.signal.aborted ||
      document.visibilityState !== 'visible'
    ) {
      throw new DOMException('Aborted', 'AbortError')
    }
    const value = kind === 'email' ? undefined : this.snapshot[kind]?.value
    const text = {
      email: this.account.email,
      expiry: value?.card_expiry,
      card: value?.card_number,
      password: value?.password,
      otp: value?.code,
      cvv: value?.cvv,
    }[field]
    if (!text || value?.available === false) {
      throw new MailboxRequestError('mailbox_credentials_unavailable', 503)
    }
    await navigator.clipboard.writeText(text)
  }
}

export function visibleCredentials(account: Account, csrf: string) {
  const key = `${csrf}:${credentialScope(account)}:${account.version}`
  let entry = entries.get(key)
  if (!entry) {
    for (const [oldKey, old] of entries) {
      if (old.account.id === account.id) {
        old.stop()
        entries.delete(oldKey)
      }
    }
    entry = new VisibleCredentials(account, csrf)
    entries.set(key, entry)
  }
  return entry
}
