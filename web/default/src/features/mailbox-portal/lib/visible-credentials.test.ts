import { afterEach, beforeEach, expect, spyOn, test } from 'bun:test'

import { mailboxApi } from '../api'
import { clearMailboxWorkspace } from '../session'
import type { Account } from '../types'
import { credentialRequest, visibleCredentials } from './visible-credentials'

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

test('visible list and detail share credentials, release removes all secrets', async () => {
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
  expect(entry.snapshot).toEqual({})
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

test('late results cannot repopulate an offscreen entry', async () => {
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
  await tick()
  finish({ password: 'late', server_time: 0 })
  await tick()
  expect(entry.snapshot).toEqual({})
})
