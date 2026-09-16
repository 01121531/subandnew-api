import {
  mailboxApi,
  MailboxRequestError,
  onAccountFailure,
  onAuthFailure,
} from '../api'
import { onMailboxWorkspaceClear } from '../session'
import type { Account, Credential, CredentialKind } from '../types'
import { credentialScope, readCredential } from './credentials'
import { assertCurrentAssignment, otpSeconds } from './guards'

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

// Values exist only while visible consumers own this entry, never in the query cache.
const entries = new Map<string, VisibleCredentials>()
export class VisibleCredentials {
  snapshot: Snapshot = {}
  private listeners = new Set<() => void>()
  private controller = new AbortController()
  private timers = new Set<ReturnType<typeof setTimeout>>()
  private unsubscribers: (() => void)[] = []
  private checking = false
  private started = false
  private kinds: CredentialKind[]
  get readable() {
    return !this.controller.signal.aborted
  }
  constructor(
    readonly account: Account,
    private csrf: string,
    private key: string
  ) {
    this.kinds =
      account.account_type === 'opening'
        ? ['password', 'otp', 'card', 'cvv']
        : ['password', 'otp']
  }
  private emit() {
    for (const listener of this.listeners) listener()
  }
  subscribe(listener: () => void) {
    this.listeners.add(listener)
    if (!this.started) {
      this.started = true
      if (typeof window !== 'undefined') {
        const offline = () =>
          this.stop(new MailboxRequestError('mailbox_request_failed', 0))
        window.addEventListener('offline', offline)
        this.unsubscribers.push(() =>
          window.removeEventListener('offline', offline)
        )
      }
      this.unsubscribers.push(
        onAuthFailure(() => this.stop()),
        onMailboxWorkspaceClear(() => this.stop()),
        onAccountFailure((id) => {
          if (id === this.account.id) this.stop()
        })
      )
      for (const kind of this.kinds) void this.load(kind)
      this.later(() => void this.check(), 30000)
    }
    return () => {
      this.listeners.delete(listener)
      queueMicrotask(() => {
        if (!this.listeners.size) this.dispose()
      })
    }
  }
  private later(run: () => void, delay: number) {
    const timer = setTimeout(() => {
      this.timers.delete(timer)
      if (!this.controller.signal.aborted) run()
    }, delay)
    this.timers.add(timer)
  }
  private stop(
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
  private dispose() {
    this.stop()
    this.unsubscribers.forEach((unsubscribe) => unsubscribe())
    this.unsubscribers = []
    this.snapshot = {}
    if (entries.get(this.key) === this) entries.delete(this.key)
  }
  private async validate() {
    const current = await credentialRequest(this.controller.signal, () =>
      mailboxApi.account(
        this.account.id,
        this.controller.signal,
        this.account.account_type ?? 'refund'
      )
    )
    assertCurrentAssignment(this.account, current, 'credentials')
    if (current.version !== this.account.version) {
      throw new MailboxRequestError('mailbox_credentials_changed', 409)
    }
    if (this.controller.signal.aborted) {
      throw new DOMException('Aborted', 'AbortError')
    }
  }
  private async check() {
    if (this.checking || this.controller.signal.aborted) return
    this.checking = true
    try {
      await this.validate()
    } catch (error) {
      this.stop(error)
    } finally {
      this.checking = false
      if (!this.controller.signal.aborted) {
        this.later(() => void this.check(), 30000)
      }
    }
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
              [kind]: { value: { available: false, server_time: 0 } },
            }
            this.emit()
            if (kind === 'otp') void this.load(kind)
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
    if (this.controller.signal.aborted) {
      this.controller = new AbortController()
      this.snapshot = {}
      for (const field of this.kinds) void this.load(field)
      this.later(() => void this.check(), 30000)
    } else void this.load(kind)
  }
  async copy(field: 'email' | 'expiry' | CredentialKind) {
    try {
      await this.validate()
    } catch (error) {
      this.stop(error)
      throw error
    }
    const kind = field === 'expiry' ? 'card' : field
    if (kind === 'otp' || kind === 'cvv') {
      const current = this.snapshot[kind]
      if (
        !current?.value ||
        otpSeconds(current.value, current.receivedAt ?? 0, Date.now()) <= 0
      ) {
        if (kind === 'otp') await this.load(kind)
        else throw new MailboxRequestError('mailbox_cvv_unavailable', 400)
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
    entry = new VisibleCredentials(account, csrf, key)
    entries.set(key, entry)
  }
  return entry
}
