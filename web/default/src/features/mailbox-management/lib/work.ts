import { z } from 'zod'

import type {
  Operator,
  Submission,
  WorkAccount,
  WorkAssignment,
  WorkRangeQuery,
  WorkScope,
  WorkSummary,
} from '../types'

export const workPeriods = [
  'all',
  'today',
  'last_7_days',
  'last_30_days',
  'custom',
] as const
export const workScopes: WorkScope[] = [
  'submitted',
  'assigned',
  'issues',
  'current_pending',
  'current_submitted',
  'current_approved',
  'current_rejected',
  'current_issue_pending',
]

export const workRangeSchema = z
  .object({
    period: z.enum(workPeriods),
    start_date: z.string().optional(),
    end_date: z.string().optional(),
  })
  .superRefine((value, context) => {
    if (value.period !== 'custom') return
    if (
      !value.start_date ||
      !value.end_date ||
      !z.iso.date().safeParse(value.start_date).success ||
      !z.iso.date().safeParse(value.end_date).success ||
      value.start_date > value.end_date
    ) {
      context.addIssue({
        code: 'custom',
        message: 'mailbox.errors.mailbox_invalid_work_query',
      })
    }
  })

export function workRangeParams(query: WorkRangeQuery): WorkRangeQuery {
  if (query.period === 'custom') {
    return {
      period: query.period,
      start_date: query.start_date,
      end_date: query.end_date,
    }
  }
  return { period: query.period ?? 'all' }
}

export interface WorkSelection {
  scope: WorkScope
  account_type: 'all' | 'refund' | 'opening'
}
export const workMetrics = [
  'submitted_accounts',
  'refund_submitted',
  'opening_submitted',
  'current_issue_pending',
  'issue_accounts',
] as const
export type WorkMetric = (typeof workMetrics)[number]

export function workMetricSelection(metric: WorkMetric): WorkSelection {
  if (metric === 'current_issue_pending') {
    return { scope: metric, account_type: 'all' }
  }
  if (metric === 'issue_accounts') {
    return { scope: 'issues', account_type: 'all' }
  }
  if (metric === 'refund_submitted') {
    return { scope: 'submitted', account_type: 'refund' }
  }
  if (metric === 'opening_submitted') {
    return { scope: 'submitted', account_type: 'opening' }
  }
  return { scope: 'submitted', account_type: 'all' }
}
export function workMetricValue(
  summary: WorkSummary,
  metric: WorkMetric
): number {
  return metric === 'current_issue_pending'
    ? summary.current.issue_pending
    : summary[metric]
}

export function canReviewWorkSubmission(
  account: WorkAccount,
  submission: Submission
): boolean {
  return (
    account.assignment_active &&
    !account.revoked_at &&
    account.assignment_id === submission.assignment_id &&
    (submission.status === 'pending' || submission.status === 'submitted')
  )
}

export function workSummaryMetadata(value: WorkSummary): WorkSummary {
  return {
    submitted_accounts: value.submitted_accounts,
    refund_submitted: value.refund_submitted,
    opening_submitted: value.opening_submitted,
    submission_count: value.submission_count,
    issue_accounts: value.issue_accounts,
    current: {
      pending: value.current.pending,
      submitted: value.current.submitted,
      approved: value.current.approved,
      rejected: value.current.rejected,
      issue_pending: value.current.issue_pending,
    },
  }
}

export function workOperatorMetadata(value: Operator): Operator {
  return {
    id: value.id,
    username: value.username,
    display_name: value.display_name,
    enabled: value.enabled,
    version: value.version,
    created_at: value.created_at,
    updated_at: value.updated_at,
    ...(value.work_summary
      ? { work_summary: workSummaryMetadata(value.work_summary) }
      : {}),
  }
}

export function workAccountMetadata(value: WorkAccount): WorkAccount {
  return {
    id: value.id,
    email: value.email,
    account_type: value.account_type,
    card_last4: /^\d{4}$/.test(value.card_last4) ? value.card_last4 : '',
    archived_at: value.archived_at,
    assignment_id: value.assignment_id,
    assignment_version: value.assignment_version,
    status: value.status,
    assignment_active: value.assignment_active,
    assigned_at: value.assigned_at,
    revoked_at: value.revoked_at,
    submission_count: value.submission_count,
    last_submitted_at: value.last_submitted_at,
    ...(value.latest_submission_id !== undefined
      ? { latest_submission_id: value.latest_submission_id }
      : {}),
    ...(value.latest_submission_status
      ? { latest_submission_status: value.latest_submission_status }
      : {}),
    last_issue_at: value.last_issue_at,
  }
}

export function workAssignmentMetadata(value: WorkAssignment): WorkAssignment {
  return {
    id: value.id,
    account_id: value.account_id,
    operator_id: value.operator_id,
    status: value.status,
    version: value.version,
    assigned_by: value.assigned_by,
    created_at: value.created_at,
    updated_at: value.updated_at,
    revoked_at: value.revoked_at,
    assignment_active: value.assignment_active,
  }
}
