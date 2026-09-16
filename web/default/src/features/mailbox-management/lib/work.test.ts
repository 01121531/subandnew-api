import { describe, expect, test } from 'bun:test'

import { mailboxEn, mailboxZh } from '@/i18n/mailbox'
import { ROLE } from '@/lib/roles'
import type { AuthUser } from '@/stores/auth-store'

import type { Submission, WorkAccount, WorkSummary } from '../types'
import { canMailbox, canMailboxWork } from './permissions'
import {
  canReviewWorkSubmission,
  workMetricSelection,
  workMetricValue,
  workPeriods,
  workRangeParams,
  workRangeSchema,
  workScopes,
} from './work'

describe('operator statistics permissions', () => {
  test('requires an administrator and all three grants, leaving operator CRUD independent', () => {
    for (let mask = 0; mask < 8; mask++) {
      const user: AuthUser = {
        id: 4,
        username: 'admin',
        role: ROLE.ADMIN,
        permissions: {
          admin_permissions: {
            mailbox_management: {
              operators: !!(mask & 1),
              view: !!(mask & 2),
              review: !!(mask & 4),
            },
          },
        },
      }
      expect(canMailboxWork(user)).toBe(mask === 7)
      expect(canMailbox(user, 'operators')).toBe(!!(mask & 1))
      expect(canMailboxWork({ ...user, role: ROLE.USER })).toBe(false)
    }
    expect(canMailboxWork(null)).toBe(false)
    expect(
      canMailboxWork({ id: 1, username: 'root', role: ROLE.SUPER_ADMIN })
    ).toBe(true)
  })
})

describe('work range and drill-down controls', () => {
  test('custom accepts real ISO dates including a same-day range, but rejects reversed or impossible dates', () => {
    for (const date of ['2026-09-16', '2024-02-29']) {
      expect(
        workRangeSchema.safeParse({
          period: 'custom',
          start_date: date,
          end_date: date,
        }).success
      ).toBe(true)
    }
    for (const dates of [
      {},
      { start_date: '2026-09-16' },
      { start_date: '2026-09-17', end_date: '2026-09-16' },
      { start_date: '2026-02-29', end_date: '2026-03-01' },
      { start_date: '2026-9-1', end_date: '2026-09-16' },
    ]) {
      expect(
        workRangeSchema.safeParse({ period: 'custom', ...dates }).success
      ).toBe(false)
    }
    expect(workRangeSchema.safeParse({ period: 'week' }).success).toBe(false)
  })
  test('presets discard custom drafts and send calendar dates unchanged for server-side Shanghai boundaries', () => {
    expect(workRangeParams({})).toEqual({ period: 'all' })
    for (const period of workPeriods.filter((value) => value !== 'custom')) {
      expect(
        workRangeParams({
          period,
          start_date: '2026-09-01',
          end_date: '2026-09-16',
        })
      ).toEqual({ period })
    }
    expect(
      workRangeParams({
        period: 'custom',
        start_date: '2026-09-01',
        end_date: '2026-09-16',
      })
    ).toEqual({
      period: 'custom',
      start_date: '2026-09-01',
      end_date: '2026-09-16',
    })
  })
  test('distinct count drill-downs select the matching pool and scope, not raw submission counts', () => {
    const summary: WorkSummary = {
      submitted_accounts: 5,
      refund_submitted: 2,
      opening_submitted: 3,
      submission_count: 19,
      issue_accounts: 4,
      current: {
        pending: 8,
        submitted: 3,
        approved: 1,
        rejected: 2,
        issue_pending: 6,
      },
    }
    expect(workMetricSelection('submitted_accounts')).toEqual({
      scope: 'submitted',
      account_type: 'all',
    })
    expect(workMetricSelection('refund_submitted')).toEqual({
      scope: 'submitted',
      account_type: 'refund',
    })
    expect(workMetricSelection('opening_submitted')).toEqual({
      scope: 'submitted',
      account_type: 'opening',
    })
    expect(workMetricSelection('issue_accounts')).toEqual({
      scope: 'issues',
      account_type: 'all',
    })
    expect(workMetricSelection('current_issue_pending')).toEqual({
      scope: 'current_issue_pending',
      account_type: 'all',
    })
    expect(workMetricValue(summary, 'submitted_accounts')).toBe(5)
    expect(workMetricValue(summary, 'current_issue_pending')).toBe(6)
  })
  test('revoked assignments and earlier assignments never offer a pending review action', () => {
    const account = {
      assignment_active: true,
      assignment_id: 10,
    } as WorkAccount
    const submission = { assignment_id: 10, status: 'pending' } as Submission
    expect(canReviewWorkSubmission(account, submission)).toBe(true)
    expect(
      canReviewWorkSubmission(
        { ...account, assignment_active: false },
        submission
      )
    ).toBe(false)
    expect(
      canReviewWorkSubmission(account, { ...submission, assignment_id: 9 })
    ).toBe(false)
    expect(
      canReviewWorkSubmission(account, { ...submission, status: 'approved' })
    ).toBe(false)
    expect(
      canReviewWorkSubmission(account, { ...submission, status: 'rejected' })
    ).toBe(false)
    expect(
      canReviewWorkSubmission(account, { ...submission, status: 'submitted' })
    ).toBe(true)
  })
  test('every selectable scope and preset has English and Chinese labels', () => {
    for (const locale of [mailboxEn, mailboxZh]) {
      for (const scope of workScopes) {
        expect(locale.work.scopes[scope]).toBeTruthy()
      }
      for (const period of workPeriods) {
        expect(locale.work.periods[period]).toBeTruthy()
      }
      expect(locale.errors.mailbox_invalid_work_query).toBeTruthy()
    }
  })
})
