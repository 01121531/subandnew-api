import { expect, test } from 'bun:test'

import type { Account } from '../types'
import { credentialScope, revealRemaining } from './credentials'

test('password/card reveals expire at 60 seconds without depending on OTP or an upstream clock', () => {
  const start = 1_000_000
  expect(revealRemaining(start, start)).toBe(60_000)
  expect(revealRemaining(start, start + 59_999)).toBe(1)
  expect(revealRemaining(start, start + 60_000)).toBe(0)
  expect(revealRemaining(start, start + 90_000)).toBe(0)
  expect(revealRemaining(start, start - 10_000)).toBe(60_000)
})

test('credential scopes reset reveals for assignment/status/pool/availability changes, not ordinary row refreshes', () => {
  const account: Account = {
    id: 3,
    email: 'fixture@example.test',
    version: 1,
    assignment_id: 4,
    assignment_version: 8,
    status: 'pending',
    credentials_available: true,
  }
  const scope = credentialScope(account)
  expect(
    credentialScope({ ...account, version: 2, account_type: 'refund' })
  ).toBe(scope)
  for (const changed of [
    { account_type: 'opening' as const },
    { assignment_id: 9 },
    { assignment_version: 9 },
    { status: 'submitted' as const },
    { credentials_available: false },
    { operator_id: 9 },
  ]) {
    expect(credentialScope({ ...account, ...changed })).not.toBe(scope)
  }
})
