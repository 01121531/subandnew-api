import { afterEach, beforeEach, describe, expect, spyOn, test } from 'bun:test'

import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'

import { mailboxEn, mailboxZh } from '@/i18n/mailbox'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'

import { mailboxApi } from './api'
import { RepairFacts } from './components/assignment-repairs'
import { WorkAccountFacts } from './components/work-accounts'
import {
  repairBody,
  repairItem,
  repairMetadata,
} from './lib/assignment-repairs'
import { assignmentConflicts } from './lib/errors'
import { canMailboxRepair, mailboxTabs } from './lib/permissions'
import { workAccountMetadata } from './lib/work'
import type { RepairCandidate, WorkAccount } from './types'

const candidate: RepairCandidate = {
  later_assignments: [
    {
      id: 12,
      operator_id: 6,
      operator_name: 'Later operator',
      status: 'pending',
      assigned_at: 8,
      revoked_at: 9,
    },
  ],
  account_id: 8,
  account_version: 3,
  email: 'fixture@example.test',
  account_type: 'opening',
  original_assignment_id: 10,
  original_assignment_version: 4,
  original_operator_id: 5,
  original_operator_name: 'Original operator',
  original_revoked_at: 7,
  submission_id: 11,
  submission_version: 6,
  submission_status: 'approved',
  submitted_at: 5,
  review_reason: 'Verified',
  reviewed_at: 6,
  current_assignment_id: 12,
  current_assignment_version: 2,
  current_operator_name: 'Current operator',
  current_status: 'pending',
  current_assigned_at: 8,
  can_repair: true,
  conflict_code: '',
}
const admin: AuthUser = { id: 1, username: 'admin', role: ROLE.SUPER_ADMIN }
let previous: AuthUser | null
let request: ReturnType<typeof spyOn<typeof api, 'request'>>
beforeEach(() => {
  previous = useAuthStore.getState().auth.user
  useAuthStore.getState().auth.setUser(admin)
  request = spyOn(api, 'request')
})
afterEach(() => {
  request.mockRestore()
  useAuthStore.getState().auth.setUser(previous)
})

describe('assignment repair contracts', () => {
  test('requires all three grants, including at the API boundary', async () => {
    for (const missing of ['view', 'assign', 'review', 'none']) {
      const user = {
        ...admin,
        role: ROLE.ADMIN,
        permissions: {
          admin_permissions: {
            mailbox_management: {
              view: missing !== 'view',
              assign: missing !== 'assign',
              review: missing !== 'review',
            },
          },
        },
      }
      expect(canMailboxRepair(user)).toBe(missing === 'none')
      expect(mailboxTabs(user).includes('repairs')).toBe(missing === 'none')
      if (missing !== 'none') {
        useAuthStore.getState().auth.setUser(user)
        await expect(
          mailboxApi.assignmentRepairs({
            account_type: 'refund',
            page: 1,
            page_size: 20,
          })
        ).rejects.toThrow('mailbox_permission_denied')
        expect(() =>
          mailboxApi.repairAssignments({
            account_type: 'opening',
            reason: 'test',
            items: [candidate],
          })
        ).toThrow('mailbox_permission_denied')
      }
    }
    expect(request).not.toHaveBeenCalled()
  })
  test('GET forwards filters, page and abort signal and strips unexpected fields', async () => {
    request.mockResolvedValue({
      data: {
        success: true,
        data: {
          items: [{ ...candidate, password: 'secret' }],
          total: 21,
          page: 2,
          page_size: 20,
          has_more: false,
          secret: 'hidden',
        },
      },
    })
    const params = {
      account_type: 'opening' as const,
      search: 'fixture',
      operator_id: 5,
      page: 2,
      page_size: 20,
    }
    const signal = new AbortController().signal
    const result = await mailboxApi.assignmentRepairs(params, signal)
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/assignment-repairs',
      method: 'GET',
      params,
      signal,
    })
    expect(result).toEqual({
      items: [candidate],
      total: 21,
      page: 2,
      page_size: 20,
      has_more: false,
    })
    expect(() => repairMetadata(candidate, 'refund')).toThrow(
      'mailbox_not_found'
    )
    expect(
      repairMetadata({ ...candidate, later_assignments: undefined }, 'opening')
        .later_assignments
    ).toEqual([])
    expect(
      repairMetadata({ ...candidate, later_assignments: [] }, 'opening')
        .later_assignments
    ).toEqual([])
  })
  test('in-flight previews cannot survive a permission change', async () => {
    let finish!: (value: unknown) => void
    request.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const pending = mailboxApi.assignmentRepairs({
      account_type: 'opening',
      page: 1,
      page_size: 20,
    })
    useAuthStore.getState().auth.setUser({ ...admin, role: ROLE.ADMIN })
    finish({ data: { success: true, data: { items: [candidate] } } })
    await expect(pending).rejects.toThrow('mailbox_permission_denied')
  })
  test('POST sends one atomic versioned body, never preview metadata', async () => {
    request.mockResolvedValue({ data: { success: true } })
    await mailboxApi.repairAssignments({
      account_type: 'opening',
      reason: '  Mistaken reassignment  ',
      items: [candidate],
    })
    expect(request).toHaveBeenCalledTimes(1)
    expect(request.mock.calls[0][0]).toMatchObject({
      method: 'POST',
      url: '/api/mailbox-management/assignment-repairs',
      data: {
        account_type: 'opening',
        reason: 'Mistaken reassignment',
        items: [repairItem(candidate)],
      },
    })
  })
  test('rejects blank reasons, invalid versions, duplicates and over-limit batches', () => {
    const input = {
      account_type: 'opening' as const,
      reason: 'Correct mistake',
      items: [candidate],
    }
    for (const value of [
      { ...input, reason: '  ' },
      { ...input, reason: 'a'.repeat(2001) },
      { ...input, items: [] },
      { ...input, items: [candidate, candidate] },
      { ...input, items: [{ ...candidate, submission_version: 0 }] },
      {
        ...input,
        items: [{ ...candidate, account_id: Number.MAX_SAFE_INTEGER + 1 }],
      },
      { ...input, items: [{ ...candidate, current_assignment_id: 0 }] },
      {
        ...input,
        items: Array.from({ length: 1001 }, (_, index) => ({
          ...candidate,
          account_id: index + 1,
        })),
      },
    ]) {
      expect(() => repairBody(value)).toThrow('mailbox_repair_invalid')
    }
    expect(
      repairBody({
        ...input,
        items: Array.from({ length: 1000 }, (_, index) => ({
          ...candidate,
          account_id: index + 1,
        })),
      }).items
    ).toHaveLength(1000)
    expect(
      repairBody({
        ...input,
        items: [
          {
            ...candidate,
            current_assignment_id: 0,
            current_assignment_version: 0,
          },
        ],
      }).items[0].current_assignment_id
    ).toBe(0)
  })
  test('assignment conflicts preserve only validated IDs and emails for envelope and HTTP failures', async () => {
    const conflicts = [
      { id: 8, email: 'fixture@example.test', password: 'secret' },
      { id: '9', email: 'bad' },
    ]
    expect(assignmentConflicts('mailbox_other', { conflicts })).toEqual([])
    for (const http of [false, true]) {
      const body = {
        success: false,
        message: 'mailbox_already_submitted',
        conflicts,
      }
      if (http) {
        request.mockRejectedValue({
          isAxiosError: true,
          response: { status: 409, data: body },
        })
      } else request.mockResolvedValue({ data: body })
      await expect(
        mailboxApi.assign({ items: [{ id: 8, version: 3 }], operator_id: 5 })
      ).rejects.toMatchObject({
        code: 'mailbox_already_submitted',
        conflicts: [{ id: 8, email: 'fixture@example.test' }],
      })
    }
  })
})

describe('repair and work-history presentation', () => {
  for (const language of ['en', 'zh'] as const) {
    test(`separates original/current records and submission status in ${language}`, async () => {
      const translations = language === 'en' ? mailboxEn : mailboxZh
      const i18n = createInstance()
      await i18n.init({
        lng: language,
        resources: { [language]: { translation: { mailbox: translations } } },
      })
      const facts = renderToStaticMarkup(
        <I18nextProvider i18n={i18n}>
          <RepairFacts
            item={{
              ...candidate,
              current_assignment_id: 0,
              current_assignment_version: 0,
              can_repair: false,
              conflict_code: 'mailbox_version_conflict',
            }}
          />
        </I18nextProvider>
      )
      expect(facts).toContain(translations.repairs.original)
      expect(facts).toContain(translations.repairs.current)
      expect(facts).toContain(translations.errors.mailbox_version_conflict)
      expect(facts).toContain('<details>')
      expect(facts).toContain('Later operator')
      expect(facts).toContain('Verified')
      expect(facts).not.toContain('mailbox.')
      const account: WorkAccount = {
        id: 8,
        email: candidate.email,
        account_type: 'opening',
        card_last4: '',
        archived_at: 0,
        assignment_id: 10,
        assignment_version: 4,
        status: 'pending',
        assignment_active: false,
        assigned_at: 2,
        revoked_at: 7,
        submission_count: 1,
        last_submitted_at: 5,
        last_issue_at: 0,
        latest_submission_id: 11,
        latest_submission_status: 'approved',
      }
      expect(workAccountMetadata(account)).toEqual(account)
      const markup = renderToStaticMarkup(
        <I18nextProvider i18n={i18n}>
          <WorkAccountFacts account={account} />
        </I18nextProvider>
      )
      expect(markup).toContain(translations.work.latestSubmission)
      expect(markup).toContain(translations.admin.statuses.approved)
      expect(markup).toContain(translations.work.inactive)
      // Historical pending belongs below its assignment label, never in the primary status row.
      expect(
        markup.indexOf(translations.admin.statuses.pending)
      ).toBeGreaterThan(markup.indexOf(translations.work.latestAssignment))
      const {
        latest_submission_id: _id,
        latest_submission_status: _status,
        ...legacy
      } = account
      expect(workAccountMetadata(legacy)).toEqual(legacy)
    })
  }
})
