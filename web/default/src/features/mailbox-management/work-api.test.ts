import { afterEach, beforeEach, describe, expect, spyOn, test } from 'bun:test'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'

import { mailboxApi } from './api'
import type {
  Page,
  WorkAccount,
  WorkAssignment,
  WorkHistory,
  WorkRange,
  WorkSummary,
} from './types'

const admin: AuthUser = { id: 1, username: 'admin', role: ROLE.SUPER_ADMIN }
const operator = {
  id: 7,
  username: 'operator',
  display_name: 'Operator',
  enabled: false,
  version: 3,
  created_at: 1,
  updated_at: 2,
}
const summary: WorkSummary = {
  submitted_accounts: 2,
  refund_submitted: 1,
  opening_submitted: 1,
  submission_count: 9,
  issue_accounts: 1,
  current: {
    pending: 1,
    submitted: 2,
    approved: 3,
    rejected: 4,
    issue_pending: 0,
  },
}
const account: WorkAccount = {
  id: 8,
  account_type: 'opening',
  email: 'fixture@example.test',
  card_last4: '4242',
  archived_at: 4,
  assignment_id: 12,
  assignment_version: 3,
  status: 'rejected',
  assignment_active: false,
  assigned_at: 1,
  revoked_at: 3,
  submission_count: 9,
  last_submitted_at: 2,
  last_issue_at: 0,
}
function page<T>(items: T[]): Page<T> {
  return { items, total: items.length, page: 1, page_size: 20, has_more: false }
}
function response(data: unknown) {
  return { data: { success: true, data } }
}
let request: ReturnType<typeof spyOn<typeof api, 'request'>>
let previous: AuthUser | null
beforeEach(() => {
  previous = useAuthStore.getState().auth.user
  useAuthStore.getState().auth.setUser(admin)
  request = spyOn(api, 'request')
})
afterEach(() => {
  request.mockRestore()
  useAuthStore.getState().auth.setUser(previous)
})

describe('opt-in statistics wire contract', () => {
  test('ordinary operator requests stay unchanged without requiring stats permissions', async () => {
    useAuthStore.getState().auth.setUser({ ...admin, role: ROLE.ADMIN })
    request.mockResolvedValue(response(page([operator])))
    await mailboxApi.operators({ page: 2, page_size: 20, search: 'op' })
    expect(request.mock.calls[0][0].params).toEqual({
      page: 2,
      page_size: 20,
      search: 'op',
    })
  })
  test('stats opt-in forwards the range and allowlists operator and summary fields', async () => {
    request.mockResolvedValue(
      response(
        page([
          {
            ...operator,
            generated_password: 'must-not-cache',
            work_summary: { ...summary, password: 'must-not-cache' },
          },
        ])
      )
    )
    const params = {
      page: 2,
      page_size: 20,
      include_stats: true,
      period: 'today' as const,
    }
    const signal = new AbortController().signal
    const result = await mailboxApi.operators(params, signal)
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/operators',
      method: 'GET',
      params,
      signal,
    })
    expect(result.items).toEqual([{ ...operator, work_summary: summary }])
  })
  test('summary preserves Shanghai range seconds without client timezone conversion', async () => {
    const range: WorkRange = {
      period: 'custom',
      start_at: 1789488000,
      end_at: 1789574400,
      timezone: 'Asia/Shanghai',
    }
    request.mockResolvedValue(
      response({
        operator: { ...operator, password: 'hidden' },
        summary,
        range,
      })
    )
    expect(
      await mailboxApi.workSummary(7, {
        period: 'custom',
        start_date: '2026-09-16',
        end_date: '2026-09-16',
      })
    ).toEqual({ operator, summary, range })
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/operators/7/work-summary',
      params: {
        period: 'custom',
        start_date: '2026-09-16',
        end_date: '2026-09-16',
      },
    })
  })
  test('account lists preserve lifetime metadata and same-email pool identities while discarding secrets', async () => {
    request.mockResolvedValue(
      response(
        page([
          { ...account, password: 'hidden', card_number: 'hidden' },
          { ...account, id: 9, account_type: 'refund', card_last4: '' },
        ])
      )
    )
    const result = await mailboxApi.workAccounts(7, {})
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/operators/7/work-accounts',
      params: {
        period: 'all',
        account_type: 'all',
        scope: 'submitted',
        page: 1,
        page_size: 20,
      },
    })
    expect(result.items).toEqual([
      account,
      { ...account, id: 9, account_type: 'refund', card_last4: '' },
    ])
    await mailboxApi.workAccounts(7, {
      period: 'last_7_days',
      scope: 'current_issue_pending',
      account_type: 'opening',
      search: 'fixture',
      page: 4,
      page_size: 10,
    })
    expect(request.mock.calls[1][0].params).toEqual({
      period: 'last_7_days',
      scope: 'current_issue_pending',
      account_type: 'opening',
      search: 'fixture',
      page: 4,
      page_size: 10,
    })
  })
  test('history is independently server-paginated and never includes the statistics range', async () => {
    const assignment: WorkAssignment = {
      id: 12,
      account_id: 8,
      operator_id: 7,
      status: 'rejected',
      version: 3,
      assigned_by: 1,
      created_at: 1,
      updated_at: 2,
      revoked_at: 3,
      assignment_active: false,
    }
    const data = {
      account: { ...account, otp: 'hidden' },
      assignments: page([{ ...assignment, password: 'hidden' }]),
      submissions: page([]),
      issues: page([]),
    }
    request.mockResolvedValue(response(data))
    const query = {
      account_type: 'opening' as const,
      assignment_page: 2,
      submission_page: 3,
      issue_page: 4,
      page_size: 10,
      period: 'today' as const,
      start_date: '2026-09-16',
    }
    const result = await mailboxApi.workHistory(7, 8, query)
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/operators/7/work-accounts/8/history',
      params: {
        account_type: 'opening',
        assignment_page: 2,
        submission_page: 3,
        issue_page: 4,
        page_size: 10,
      },
    })
    expect(result).toEqual({
      account,
      assignments: page([assignment]),
      submissions: page([]),
      issues: page([]),
    })
    await mailboxApi.workHistory(7, 8, { account_type: 'opening' })
    expect(request.mock.calls[1][0].params).toEqual({
      account_type: 'opening',
      assignment_page: 1,
      submission_page: 1,
      issue_page: 1,
      page_size: 20,
    })
  })
  test('history rejects foreign operator, account, and pool associations', async () => {
    const data: WorkHistory = {
      account,
      assignments: page([]),
      submissions: page([]),
      issues: page([]),
    }
    for (const foreign of [
      { ...data, account: { ...account, id: 99 } },
      { ...data, account: { ...account, account_type: 'refund' } },
      { ...data, assignments: page([{ operator_id: 99, account_id: 8 }]) },
      { ...data, submissions: page([{ operator_id: 7, account_id: 99 }]) },
      { ...data, issues: page([{ operator_id: 99, account_id: 8 }]) },
    ]) {
      request.mockResolvedValueOnce(response(foreign))
      await expect(
        mailboxApi.workHistory(7, 8, { account_type: 'opening' })
      ).rejects.toMatchObject({ code: 'mailbox_not_found' })
    }
  })
  test('both pools retain note-only submission remarks in work history and accept legacy records', async () => {
    for (const accountType of ['refund', 'opening'] as const) {
      const submissions = [
        {
          id: 1,
          account_id: 8,
          operator_id: 7,
          account_type: accountType,
          remark: 'First line\nSecond line',
          attachments: [],
          password: 'must-not-cache',
        },
        {
          id: 2,
          account_id: 8,
          operator_id: 7,
          account_type: accountType,
          attachments: [],
        },
      ]
      request.mockResolvedValue(
        response({
          account: { ...account, account_type: accountType },
          assignments: page([]),
          submissions: page(submissions),
          issues: page([]),
        })
      )
      const result = await mailboxApi.workHistory(7, 8, {
        account_type: accountType,
      })
      expect(result.submissions.items.map((item) => item.remark)).toEqual([
        'First line\nSecond line',
        '',
      ])
      expect(
        result.submissions.items.every((item) => item.attachments.length === 0)
      ).toBe(true)
      expect(result.submissions.items[0]).not.toHaveProperty('password')
    }
  })
  test('missing permissions prevent all stats requests, including list opt-in', async () => {
    useAuthStore.getState().auth.setUser({ ...admin, role: ROLE.ADMIN })
    for (const run of [
      () =>
        mailboxApi.operators({ page: 1, page_size: 20, include_stats: true }),
      () => mailboxApi.workSummary(7, {}),
      () => mailboxApi.workAccounts(7, {}),
      () => mailboxApi.workHistory(7, 8, { account_type: 'opening' }),
    ]) {
      await expect(run()).rejects.toMatchObject({
        code: 'mailbox_permission_denied',
        status: 403,
      })
    }
    expect(request).not.toHaveBeenCalled()
  })
  test('in-flight data is discarded after grant, principal or authorization version changes', async () => {
    for (const changed of [
      null,
      { ...admin, role: ROLE.ADMIN },
      { ...admin, id: 99 },
      { ...admin, authorization_version: 2 },
    ]) {
      useAuthStore.getState().auth.setUser(admin)
      let finish!: (value: unknown) => void
      request.mockReturnValueOnce(
        new Promise((resolve) => {
          finish = resolve
        })
      )
      const pending = mailboxApi.workAccounts(7, {})
      useAuthStore.getState().auth.setUser(changed)
      finish(response(page([account])))
      await expect(pending).rejects.toMatchObject({
        code: 'mailbox_permission_denied',
      })
    }
  })
})
