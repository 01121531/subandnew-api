import { describe, expect, test } from 'bun:test'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'

import { mailboxEn, mailboxZh } from '@/i18n/mailbox'
import { ROLE } from '@/lib/roles'
import type { AuthUser } from '@/stores/auth-store'

import { statusLabelKey } from './display'
import { errorKey, MailboxError, safeCode } from './errors'
import {
  canAccessMailbox,
  canMailbox,
  mailboxActions,
  mailboxTabs,
} from './permissions'
import {
  operatorSchema,
  otpRemaining,
  parseVersionedIDs,
  passwordSchema,
  reviewSchema,
} from './schemas'

describe('mailbox grants', () => {
  const ordinary: AuthUser = { id: 7, username: 'admin', role: ROLE.ADMIN }
  test('root defaults to every action, ordinary and anonymous default to none', () => {
    for (const action of mailboxActions) {
      expect(canMailbox({ ...ordinary, role: ROLE.SUPER_ADMIN }, action)).toBe(
        true
      )
      expect(canMailbox(ordinary, action)).toBe(false)
      expect(canMailbox(null, action)).toBe(false)
    }
    expect(canAccessMailbox(ordinary)).toBe(false)
    expect(mailboxTabs(ordinary)).toEqual([])
  })
  test('every independent grant exposes the menu without granting view', () => {
    for (const action of mailboxActions) {
      const user = {
        ...ordinary,
        permissions: {
          admin_permissions: { mailbox_management: { [action]: true } },
        },
      }
      expect(canAccessMailbox(user)).toBe(true)
      expect(canMailbox(user, 'view')).toBe(action === 'view')
      const tab = ['review', 'operators', 'audit'].includes(action)
        ? action
        : 'pool'
      expect(mailboxTabs(user)).toEqual([tab])
    }
  })
})
describe('atomic assignment and form contracts', () => {
  test('explicit IDs retain optimistic versions, reject duplicates and unsafe values', () => {
    expect(parseVersionedIDs('1,7\n2\t9\n3:11')).toEqual([
      { id: 1, version: 7 },
      { id: 2, version: 9 },
      { id: 3, version: 11 },
    ])
    for (const text of [
      '',
      '1',
      '0,1',
      '1,0',
      '1,2\n1,3',
      '9007199254740992,1',
      '1,2,3',
      '1,-1',
      Array.from({ length: 1001 }, (_, index) => `${index + 1},1`).join('\n'),
    ]) {
      expect(() => parseVersionedIDs(text)).toThrow()
    }
  })
  test('password constraints use UTF-8 bytes and permit explicit generation', () => {
    expect(passwordSchema.safeParse({ password: '' }).success).toBe(true)
    expect(passwordSchema.safeParse({ password: '密码密码密码' }).success).toBe(
      true
    )
    expect(
      passwordSchema.safeParse({ password: '密'.repeat(25) }).success
    ).toBe(false)
    expect(passwordSchema.safeParse({ password: 'short' }).success).toBe(false)
    expect(
      operatorSchema.safeParse({
        username: ' ',
        display_name: 'Name',
        enabled: true,
        version: 0,
        password: '',
      }).success
    ).toBe(false)
  })
  test('rejection requires a bounded nonblank reason', () => {
    expect(
      reviewSchema.safeParse({ status: 'approved', reason: '' }).success
    ).toBe(true)
    expect(
      reviewSchema.safeParse({ status: 'rejected', reason: ' ' }).success
    ).toBe(false)
    expect(
      reviewSchema.safeParse({
        status: 'rejected',
        reason: 'Please upload a clear image',
      }).success
    ).toBe(true)
    expect(
      reviewSchema.safeParse({ status: 'rejected', reason: 'x'.repeat(2001) })
        .success
    ).toBe(false)
  })
  test('OTP expiry uses the server offset and never shows negative time', () => {
    expect(otpRemaining(130, 10000, 100000)).toBe(20)
    expect(otpRemaining(130, -10000, 100000)).toBe(40)
    expect(otpRemaining(130, 10000, 121000)).toBe(0)
  })
})
describe('safe, localized failures', () => {
  test('pending has separate review and account labels without changing its wire value', () => {
    expect(statusLabelKey('pending')).toBe('mailbox.admin.statuses.pending')
    expect(statusLabelKey('pending', true)).toBe('mailbox.admin.awaitingReview')
    expect(statusLabelKey('approved', true)).toBe(
      'mailbox.admin.statuses.approved'
    )
  })
  test('untrusted diagnostics are never surfaced as error messages', () => {
    expect(safeCode('password=private')).toBe('mailbox_request_failed')
    expect(errorKey(new Error('private transport diagnostic'))).toBe(
      'mailbox.errors.mailbox_request_failed'
    )
    expect(errorKey(new MailboxError('mailbox_version_conflict', 409))).toBe(
      'mailbox.errors.mailbox_version_conflict'
    )
  })
  test('all production mailbox error codes have English and Chinese translations', () => {
    const dir = resolve(import.meta.dir, '../../../../../../service/mailbox')
    const codes = new Set<string>()
    for (const file of readdirSync(dir).filter(
      (name) => name.endsWith('.go') && !name.endsWith('_test.go')
    )) {
      const source = readFileSync(resolve(dir, file), 'utf8')
      for (const match of source.matchAll(/"(mailbox_[a-z0-9_]+)"/g)) {
        if (match[1] !== 'mailbox_assignments') codes.add(match[1])
      }
    }
    expect(codes.size).toBeGreaterThan(30)
    for (const code of codes) {
      expect(Object.hasOwn(mailboxEn.errors, code)).toBe(true)
      expect(Object.hasOwn(mailboxZh.errors, code)).toBe(true)
    }
  })
})
