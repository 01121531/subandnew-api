import { afterEach, beforeEach, expect, spyOn, test } from 'bun:test'

import { mailboxApi, MailboxRequestError } from '../api'
import { clearMailboxWorkspace } from '../session'
import type { Account } from '../types'
import {
  clearPageCredentials,
  credentialPageGeneration,
  reconcilePageCredentials,
  failPageCredentials,
  credentialRequest,
  visibleCredentials,
} from './visible-credentials'

const account: Account = {
  id: 100,
  email: 'fixture@example.test',
  version: 1,
  assignment_id: 1,
  assignment_version: 1,
  account_type: 'opening',
  status: 'pending',
  credentials_available: true,
}
let read: ReturnType<typeof spyOn<typeof mailboxApi, 'credentials'>>
let detail: ReturnType<typeof spyOn<typeof mailboxApi, 'account'>>
const cleanup: (() => void)[] = []
const tick = () => new Promise((resolve) => setTimeout(resolve, 5))
beforeEach(() => {
  detail = spyOn(mailboxApi, 'account').mockResolvedValue(account)
  read = spyOn(mailboxApi, 'credentials').mockImplementation(
    async (_csrf, _id, kind) => ({
      available: true,
      server_time: Date.now() / 1000,
      expires_at: Date.now() / 1000 + 100,
      ...{
        password: { password: '  exact password  ' },
        card: { card_number: '4242424242424242', card_expiry: '12/30' },
        otp: { code: '001234' },
        cvv: { cvv: '007' },
      }[kind],
    })
  )
})
afterEach(async () => {
  cleanup.splice(0).forEach((fn) => fn())
  clearMailboxWorkspace()
  await tick()
  read.mockRestore()
  detail.mockRestore()
})

test('scrolling and detail close retain page-owned credentials without rereading', async () => {
  const entry = visibleCredentials(account, 'scope-one')
  expect(read).not.toHaveBeenCalled()
  const first = entry.subscribe(() => {})
  cleanup.push(first)
  await tick()
  const second = visibleCredentials(account, 'scope-one').subscribe(() => {})
  cleanup.push(second)
  await tick()
  expect(read).toHaveBeenCalledTimes(4)
  expect(entry.snapshot.password?.value?.password).toBe('  exact password  ')
  first()
  await tick()
  expect(entry.snapshot.cvv?.value?.cvv).toBe('007')
  second()
  await tick()
  expect(entry.snapshot.password?.value?.password).toBe('  exact password  ')
  cleanup.push(visibleCredentials(account, 'scope-one').subscribe(() => {}))
  await tick()
  expect(read).toHaveBeenCalledTimes(4)
  clearPageCredentials()
  expect(entry.snapshot.password?.value).toBeUndefined()
})

test('queue caps concurrency at four and cancels waiting jobs before reading', async () => {
  let running = 0,
    peak = 0
  const release: (() => void)[] = []
  const jobs = Array.from({ length: 8 }, () =>
    credentialRequest(new AbortController().signal, async () => {
      running++
      peak = Math.max(peak, running)
      await new Promise<void>((resolve) => release.push(resolve))
      running--
    })
  )
  const aborted = new AbortController()
  let ran = false
  const cancelled = credentialRequest(aborted.signal, async () => {
    ran = true
  }).catch(() => {})
  aborted.abort()
  expect(release.length).toBe(4)
  release.splice(0).forEach((fn) => fn())
  await tick()
  release.splice(0).forEach((fn) => fn())
  await Promise.all([...jobs, cancelled])
  expect(peak).toBe(4)
  expect(ran).toBe(false)
})

test('failed CVV leaves password and card available; workspace clear removes them', async () => {
  read.mockImplementation(async (_csrf, _id, kind) => {
    if (kind === 'cvv') throw new Error('unavailable')
    return {
      password: 'test',
      card_number: '4242424242424242',
      server_time: 0,
      available: kind !== 'otp',
    }
  })
  const entry = visibleCredentials(account, 'scope-two')
  cleanup.push(entry.subscribe(() => {}))
  await tick()
  expect(entry.snapshot.cvv?.error).toBeDefined()
  expect(entry.snapshot.password?.value?.password).toBe('test')
  clearMailboxWorkspace()
  expect(entry.snapshot.password?.value).toBeUndefined()
})

test('late results cannot repopulate a previous page entry', async () => {
  let finish!: (v: { password: string; server_time: number }) => void
  read.mockReturnValue(
    new Promise((resolve) => {
      finish = resolve
    })
  )
  const entry = visibleCredentials(account, 'scope-three')
  const release = entry.subscribe(() => {})
  cleanup.push(release)
  await tick()
  release()
  clearPageCredentials()
  await tick()
  finish({ password: 'late', server_time: 0 })
  await tick()
  expect(entry.snapshot.password?.value).toBeUndefined()
})

test('submitted CVV restriction does not revoke password, OTP or card', async () => {
  const submitted: Account = { ...account, status: 'submitted' }
  detail.mockResolvedValue(submitted)
  const entry = visibleCredentials(submitted, 'submitted-scope')
  cleanup.push(entry.subscribe(() => {}))
  await tick()
  expect(entry.readable).toBe(true)
  expect(entry.snapshot.password?.value?.password).toBe('  exact password  ')
  expect(entry.snapshot.otp?.value?.code).toBe('001234')
  expect(entry.snapshot.card?.value?.card_number).toBe('4242424242424242')
  expect(entry.snapshot.cvv?.error).toMatchObject({
    code: 'mailbox_cvv_task_restricted',
  })
  expect(read.mock.calls.some((call) => call[2] === 'cvv')).toBe(false)
})

test('real revocation still clears all visible credentials', async () => {
  read.mockImplementation(async () => {
    throw new MailboxRequestError('mailbox_credentials_revoked', 403)
  })
  const entry = visibleCredentials(account, 'revoked-scope')
  cleanup.push(entry.subscribe(() => {}))
  await tick()
  expect(entry.readable).toBe(false)
  for (const state of Object.values(entry.snapshot)) {
    expect(state.value).toBeUndefined()
  }
})

test('all six copy fields are local and preserve exact strings', async () => {
  const documentDescriptor = Object.getOwnPropertyDescriptor(
    globalThis,
    'document'
  )
  const clipboardDescriptor = Object.getOwnPropertyDescriptor(
    navigator,
    'clipboard'
  )
  const copied: string[] = []
  Object.defineProperty(globalThis, 'document', {
    configurable: true,
    value: { visibilityState: 'visible' },
  })
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: {
      writeText: async (text: string) => {
        copied.push(text)
      },
    },
  })
  try {
    const entry = visibleCredentials(account, 'copy-scope')
    cleanup.push(entry.subscribe(() => {}))
    await tick()
    read.mockClear()
    detail.mockClear()
    for (const field of [
      'email',
      'password',
      'otp',
      'card',
      'expiry',
      'cvv',
    ] as const) {
      await entry.copy(field)
    }
    expect(copied).toEqual([
      account.email,
      '  exact password  ',
      '001234',
      '4242424242424242',
      '12/30',
      '007',
    ])
    expect(read).not.toHaveBeenCalled()
    expect(detail).not.toHaveBeenCalled()
    entry.snapshot.otp = {
      value: { code: 'expired', server_time: 1, expires_at: 1 },
      receivedAt: Date.now(),
    }
    await expect(entry.copy('otp')).rejects.toBeDefined()
    expect(read).not.toHaveBeenCalled()
    expect(detail).not.toHaveBeenCalled()
  } finally {
    if (documentDescriptor) {
      Object.defineProperty(globalThis, 'document', documentDescriptor)
    } else Reflect.deleteProperty(globalThis, 'document')
    if (clipboardDescriptor) {
      Object.defineProperty(navigator, 'clipboard', clipboardDescriptor)
    } else Reflect.deleteProperty(navigator, 'clipboard')
  }
})

test('page recheck revokes changed accounts and ignores old-page responses', async () => {
  const generation = credentialPageGeneration()
  const entry = visibleCredentials(account, 'recheck')
  cleanup.push(entry.subscribe(() => {}))
  await tick()
  read.mockClear()
  detail.mockClear()
  reconcilePageCredentials([account], generation)
  expect(entry.readable).toBe(true)
  reconcilePageCredentials([{ ...account, version: 2 }], generation)
  expect(entry.readable).toBe(false)
  expect(entry.snapshot.password?.value).toBeUndefined()
  expect(read).not.toHaveBeenCalled()
  expect(detail).not.toHaveBeenCalled()
  clearPageCredentials()
  const next = visibleCredentials(account, 'recheck-next')
  cleanup.push(next.subscribe(() => {}))
  await tick()
  reconcilePageCredentials([], generation)
  expect(next.readable).toBe(true)
  failPageCredentials(new Error('offline'))
  expect(next.readable).toBe(false)
  expect(next.snapshot.cvv?.value).toBeUndefined()
})

test('passive observers retain loaded data without starting requests', async () => {
  const entry = visibleCredentials(account, 'passive')
  cleanup.push(entry.subscribe(() => {}, false))
  await tick()
  expect(read).not.toHaveBeenCalled()
  const deactivate = entry.activate()
  await tick()
  const password = entry.snapshot.password?.value
  expect(password).toBeDefined()
  deactivate()
  deactivate()
  read.mockClear()
  detail.mockClear()
  cleanup.push(entry.activate())
  await tick()
  expect(entry.snapshot.password?.value).toBe(password)
  expect(read).not.toHaveBeenCalled()
  expect(detail).not.toHaveBeenCalled()
  entry.stop()
  expect(entry.snapshot.password?.value).toBeUndefined()
})

test('returning to expired OTP refreshes only OTP; absent OTP is not reloaded', async () => {
  const entry = visibleCredentials(account, 'otp-return')
  const release = entry.subscribe(() => {})
  cleanup.push(release)
  await tick()
  release()
  entry.snapshot.otp = {
    value: { code: 'expired', server_time: 1, expires_at: 1 },
    receivedAt: Date.now(),
  }
  read.mockClear()
  const releaseAgain = entry.subscribe(() => {})
  cleanup.push(releaseAgain)
  await tick()
  expect(read.mock.calls.map((call) => call[2])).toEqual(['otp'])
  releaseAgain()
  entry.snapshot.otp = { value: { available: false, server_time: 0 } }
  read.mockClear()
  cleanup.push(entry.subscribe(() => {}))
  await tick()
  expect(read).not.toHaveBeenCalled()
})
