import { describe, expect, test } from 'bun:test'

import type { Account } from '../types'
import {
  assertCurrentAssignment,
  canReadCredentials,
  canSubmit,
  otpSeconds,
  validateImages,
} from './guards'
import { passwordSchema } from './schemas'

const account: Account = {
  id: 3,
  email: 'fixture@example.test',
  version: 2,
  assignment_id: 4,
  assignment_version: 8,
  status: 'pending',
  credentials_available: true,
}
describe('mailbox workflow guards', () => {
  test('approved accounts never expose credentials, even if availability flag is inconsistent', () => {
    expect(canReadCredentials({ ...account, status: 'approved' })).toBe(false)
    expect(
      canReadCredentials({ ...account, credentials_available: false })
    ).toBe(false)
    expect(canReadCredentials({ ...account, assignment_id: 0 })).toBe(false)
    expect(canReadCredentials({ ...account, status: 'submitted' })).toBe(true)
  })
  test('only pending/rejected current assignments with known versions can submit', () => {
    expect(canSubmit(account)).toBe(true)
    expect(canSubmit({ ...account, status: 'rejected' })).toBe(true)
    expect(canSubmit({ ...account, status: 'submitted' })).toBe(false)
    expect(canSubmit({ ...account, status: 'approved' })).toBe(false)
    expect(canSubmit({ ...account, assignment_version: 0 })).toBe(false)
  })
  test('account identity, assignment identity and assignment version are all checked', () => {
    for (const changed of [
      { id: 9 },
      { assignment_id: 9 },
      { assignment_version: 9 },
      { account_type: 'opening' as const },
    ]) {
      expect(() =>
        assertCurrentAssignment(account, { ...account, ...changed }, 'submit')
      ).toThrow('mailbox_assignment_changed')
    }
    expect(() =>
      assertCurrentAssignment(account, account, 'submit')
    ).not.toThrow()
  })
  test('draft-upload account version increments allow subsequent uploads, submit and credential reads', () => {
    const afterUpload = { ...account, version: account.version + 1 }
    const afterSecondUpload = { ...account, version: account.version + 2 }
    for (const current of [afterUpload, afterSecondUpload]) {
      expect(() =>
        assertCurrentAssignment(account, current, 'submit')
      ).not.toThrow()
      expect(() =>
        assertCurrentAssignment(account, current, 'credentials')
      ).not.toThrow()
    }
    expect(() =>
      assertCurrentAssignment(
        account,
        { ...afterUpload, assignment_version: account.assignment_version + 1 },
        'submit'
      )
    ).toThrow('mailbox_assignment_changed')
    expect(() =>
      assertCurrentAssignment(
        account,
        { ...afterUpload, status: 'submitted' },
        'submit'
      )
    ).toThrow('mailbox_assignment_changed')
    expect(() =>
      assertCurrentAssignment(
        account,
        { ...afterUpload, status: 'approved' },
        'credentials'
      )
    ).toThrow('mailbox_credentials_revoked')
  })
  test('OTP expiration uses server offset rather than the local epoch', () => {
    const credential = { code: '123456', server_time: 100, expires_at: 130 }
    expect(otpSeconds(credential, 9_000_000, 9_000_000)).toBe(30)
    expect(otpSeconds(credential, 9_000_000, 9_005_000)).toBe(25)
    expect(otpSeconds(credential, 9_000_000, 9_030_000)).toBe(0)
    expect(otpSeconds(credential, 9_000_000, 9_090_000)).toBe(0)
  })
  test('upload selection enforces image types, maximum file size and five total drafts', () => {
    const image = new File(['fixture'], 'fixture.png', { type: 'image/png' })
    expect(() => validateImages([image], 4)).not.toThrow()
    expect(() => validateImages([image], 5)).toThrow('mailbox_attachment_count')
    expect(() => validateImages([], 0)).toThrow('mailbox_attachment_count')
    expect(() =>
      validateImages([new File(['svg'], 'x.svg', { type: 'image/svg+xml' })], 0)
    ).toThrow('mailbox_invalid_image')
    expect(() =>
      validateImages(
        [
          new File([new Uint8Array(10 * 1024 * 1024 + 1)], 'big.png', {
            type: 'image/png',
          }),
        ],
        0
      )
    ).toThrow('mailbox_attachment_too_large')
  })
  test('new password validation checks byte limits and confirmation without trimming password content', () => {
    expect(
      passwordSchema.safeParse({
        current_password: 'old',
        password: ' 12345678 ',
        confirm: ' 12345678 ',
      }).success
    ).toBe(true)
    expect(
      passwordSchema.safeParse({
        current_password: 'old',
        password: 'a'.repeat(73),
        confirm: 'a'.repeat(73),
      }).success
    ).toBe(false)
    expect(
      passwordSchema.safeParse({
        current_password: 'old',
        password: '12345678',
        confirm: '87654321',
      }).success
    ).toBe(false)
    expect(
      passwordSchema.safeParse({
        current_password: '',
        password: '12345678',
        confirm: '12345678',
      }).success
    ).toBe(false)
  })
})
