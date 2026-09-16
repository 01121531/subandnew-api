import { isAxiosError } from 'axios'

import { adminDataAuthorizationKey } from '@/lib/admin-data-policy'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { MailboxError, safeCode } from './lib/errors'
import { canMailboxWork } from './lib/permissions'
import {
  accountMetadata,
  auditMetadata,
  importMetadata,
  issueMetadata,
  pageMetadata,
  submissionMetadata,
} from './lib/pools'
import {
  workAccountMetadata,
  workAssignmentMetadata,
  workOperatorMetadata,
  workRangeParams,
  workSummaryMetadata,
} from './lib/work'
import type {
  Account,
  AccountType,
  AccountOperator,
  AssignInput,
  Audit,
  Credential,
  CredentialKind,
  Envelope,
  ImportPreview,
  ImportSource,
  Issue,
  ResolveIssueInput,
  ListQuery,
  Operator,
  OperatorInput,
  OperatorOption,
  Page,
  ReviewInput,
  Submission,
  VersionedID,
  OperatorsQuery,
  OperatorWorkSummary,
  WorkAccount,
  WorkAccountsQuery,
  WorkHistory,
  WorkHistoryQuery,
  WorkRangeQuery,
} from './types'

const base = '/api/mailbox-management'
export function importBody(source: ImportSource):
  | FormData
  | {
      format: string
      text: string
      account_type: AccountType
      ignore_extra_fields?: boolean
    } {
  if (source.file) {
    const body = new FormData()
    body.append('account_type', source.account_type ?? 'refund')
    body.append('file', source.file)
    if (source.ignore_extra_fields) body.append('ignore_extra_fields', 'true')
    return body
  }
  return {
    format: source.format,
    text: source.text,
    account_type: source.account_type ?? 'refund',
    ...(source.ignore_extra_fields ? { ignore_extra_fields: true } : {}),
  }
}
export function unwrap<T>(body: Envelope<T>, status = 0): T {
  if (body?.success !== true) {
    throw new MailboxError(safeCode(body?.message), status)
  }
  return body.data
}
async function request<T>(
  path: string,
  method = 'GET',
  data?: unknown,
  params?:
    | Partial<ListQuery>
    | OperatorsQuery
    | WorkRangeQuery
    | WorkAccountsQuery
    | WorkHistoryQuery,
  signal?: AbortSignal
): Promise<T> {
  try {
    const response = await api.request<Envelope<T>>({
      url: `${base}${path}`,
      method,
      data,
      params,
      signal,
      headers: { 'X-Mailbox-Request': '1' },
      skipBusinessError: true,
      skipErrorHandler: true,
    })
    return unwrap(response.data)
  } catch (error) {
    if (isAxiosError<Envelope<T>>(error)) {
      throw new MailboxError(
        safeCode(error.response?.data?.message),
        error.response?.status ?? 0
      )
    }
    throw error
  }
}
async function workRequest<T>(
  path: string,
  params:
    | OperatorsQuery
    | WorkRangeQuery
    | WorkAccountsQuery
    | WorkHistoryQuery,
  signal?: AbortSignal
): Promise<T> {
  const user = useAuthStore.getState().auth.user
  if (!canMailboxWork(user)) {
    throw new MailboxError('mailbox_permission_denied', 403)
  }
  const authorization = adminDataAuthorizationKey(user)
  const result = await request<T>(path, 'GET', undefined, params, signal)
  const current = useAuthStore.getState().auth.user
  // An in-flight response must not survive a principal or grant change.
  if (
    !canMailboxWork(current) ||
    authorization !== adminDataAuthorizationKey(current)
  ) {
    throw new MailboxError('mailbox_permission_denied', 403)
  }
  return result
}
export const mailboxApi = {
  issueOperators: (signal?: AbortSignal) =>
    request<AccountOperator[]>(
      '/issue-operators',
      'GET',
      undefined,
      undefined,
      signal
    ),
  importOptions: (signal?: AbortSignal) =>
    request<{
      temporary_cvv_enabled: boolean
      temporary_cvv_unavailable_reason?: string
    }>('/import-options', 'GET', undefined, undefined, signal),
  issues: async (query: ListQuery, signal?: AbortSignal) =>
    pageMetadata(
      await request<Page<Issue>>('/issues', 'GET', undefined, query, signal),
      (item) => issueMetadata(item, query.account_type ?? 'refund')
    ),
  issue: async (id: number, accountType: AccountType, signal?: AbortSignal) =>
    issueMetadata(
      await request<Issue>(
        `/issues/${id}`,
        'GET',
        undefined,
        { account_type: accountType },
        signal
      ),
      accountType
    ),
  resolveIssue: (
    id: number,
    accountType: AccountType,
    input: ResolveIssueInput
  ) =>
    request<unknown>(`/issues/${id}/resolve`, 'POST', input, {
      account_type: accountType,
    }),
  provideTemporaryCvv: async (id: number, version: number, cvv: string) => {
    const result = await request<{ expires_at: number }>(
      `/accounts/${id}/temporary-cvv`,
      'POST',
      { version, cvv },
      { account_type: 'opening' }
    )
    return { expires_at: result.expires_at }
  },
  accounts: async (query: ListQuery, signal?: AbortSignal) => {
    const accountType = query.account_type ?? 'refund'
    const page = await request<Page<Account>>(
      '/accounts',
      'GET',
      undefined,
      { ...query, account_type: accountType },
      signal
    )
    return pageMetadata(page, (item) => accountMetadata(item, accountType))
  },
  accountOperators: (
    signal?: AbortSignal,
    accountType: AccountType = 'refund'
  ) =>
    request<AccountOperator[]>(
      '/account-operators',
      'GET',
      undefined,
      { account_type: accountType },
      signal
    ),
  account: async (
    id: number,
    signal?: AbortSignal,
    accountType: AccountType = 'refund'
  ) =>
    accountMetadata(
      await request<Account>(
        `/accounts/${id}`,
        'GET',
        undefined,
        { account_type: accountType },
        signal
      ),
      accountType
    ),
  preview: async (source: ImportSource, signal?: AbortSignal) =>
    importMetadata(
      await request<ImportPreview>(
        '/imports/preview',
        'POST',
        importBody(source),
        undefined,
        signal
      )
    ),
  import: (source: ImportSource) =>
    request<{ imported: number }>('/imports', 'POST', importBody(source)),
  assign: (input: AssignInput) =>
    request<unknown>('/assignments', 'POST', {
      ...input,
      account_type: input.account_type ?? 'refund',
    }),
  archive: (accountType: AccountType, items: VersionedID[], restore: boolean) =>
    request<unknown>(`/accounts/${restore ? 'restore' : 'archive'}`, 'POST', {
      account_type: accountType,
      items,
    }),
  credentials: (
    id: number,
    kind: CredentialKind,
    signal?: AbortSignal,
    accountType: AccountType = 'refund'
  ) =>
    request<Credential>(
      `/accounts/${id}/credentials`,
      'POST',
      { kind },
      { account_type: accountType },
      signal
    ),
  operators: async (query: OperatorsQuery, signal?: AbortSignal) => {
    if (!query.include_stats) {
      return request<Page<Operator>>(
        '/operators',
        'GET',
        undefined,
        query,
        signal
      )
    }
    return pageMetadata(
      await workRequest<Page<Operator>>('/operators', query, signal),
      workOperatorMetadata
    )
  },
  workSummary: async (
    id: number,
    query: WorkRangeQuery,
    signal?: AbortSignal
  ): Promise<OperatorWorkSummary> => {
    const result = await workRequest<OperatorWorkSummary>(
      `/operators/${id}/work-summary`,
      workRangeParams(query),
      signal
    )
    if (result.operator.id !== id) {
      throw new MailboxError('mailbox_not_found', 404)
    }
    return {
      operator: workOperatorMetadata(result.operator),
      summary: workSummaryMetadata(result.summary),
      range: {
        period: result.range.period,
        start_at: result.range.start_at,
        end_at: result.range.end_at,
        timezone: result.range.timezone,
      },
    }
  },
  workAccounts: async (
    id: number,
    query: WorkAccountsQuery,
    signal?: AbortSignal
  ) =>
    pageMetadata(
      await workRequest<Page<WorkAccount>>(
        `/operators/${id}/work-accounts`,
        {
          ...workRangeParams(query),
          account_type: query.account_type ?? 'all',
          scope: query.scope ?? 'submitted',
          search: query.search,
          page: query.page ?? 1,
          page_size: query.page_size ?? 20,
        },
        signal
      ),
      workAccountMetadata
    ),
  workHistory: async (
    id: number,
    accountID: number,
    query: WorkHistoryQuery,
    signal?: AbortSignal
  ): Promise<WorkHistory> => {
    const result = await workRequest<WorkHistory>(
      `/operators/${id}/work-accounts/${accountID}/history`,
      {
        account_type: query.account_type,
        assignment_page: query.assignment_page ?? 1,
        submission_page: query.submission_page ?? 1,
        issue_page: query.issue_page ?? 1,
        page_size: query.page_size ?? 20,
      },
      signal
    )
    if (
      result.account.id !== accountID ||
      result.account.account_type !== query.account_type ||
      [
        ...result.assignments.items,
        ...result.submissions.items,
        ...result.issues.items,
      ].some((item) => item.operator_id !== id || item.account_id !== accountID)
    ) {
      throw new MailboxError('mailbox_not_found', 404)
    }
    return {
      account: workAccountMetadata(result.account),
      assignments: pageMetadata(result.assignments, workAssignmentMetadata),
      submissions: pageMetadata(result.submissions, (item) =>
        submissionMetadata(item, query.account_type)
      ),
      issues: pageMetadata(result.issues, (item) =>
        issueMetadata(item, query.account_type)
      ),
    }
  },
  operatorOptions: (signal?: AbortSignal) =>
    request<OperatorOption[]>(
      '/operator-options',
      'GET',
      undefined,
      undefined,
      signal
    ),
  saveOperator: (input: OperatorInput, id?: number) =>
    request<Operator>(
      id ? `/operators/${id}` : '/operators',
      id ? 'PUT' : 'POST',
      input
    ),
  password: (id: number, password: string) =>
    request<{ password: string }>(`/operators/${id}/password`, 'POST', {
      password,
    }),
  revoke: (id: number) =>
    request<unknown>(`/operators/${id}/revoke-sessions`, 'POST'),
  submissions: async (query: ListQuery, signal?: AbortSignal) => {
    const accountType = query.account_type ?? 'refund'
    const page = await request<Page<Submission>>(
      '/submissions',
      'GET',
      undefined,
      { ...query, account_type: accountType },
      signal
    )
    return pageMetadata(page, (item) => submissionMetadata(item, accountType))
  },
  review: (id: number, input: ReviewInput) =>
    request<unknown>(
      `/submissions/${id}/review`,
      'POST',
      { version: input.version, status: input.status, reason: input.reason },
      { account_type: input.account_type ?? 'refund' }
    ),
  audits: async (query: ListQuery, signal?: AbortSignal) =>
    pageMetadata(
      await request<Page<Audit>>(
        '/audits',
        'GET',
        undefined,
        { ...query, account_type: query.account_type ?? 'refund' },
        signal
      ),
      auditMetadata
    ),
  attachment: async (
    id: string,
    signal?: AbortSignal,
    accountType: AccountType = 'refund'
  ): Promise<Blob> => {
    try {
      const response = await api.request<Blob>({
        url: `${base}/attachments/${encodeURIComponent(id)}`,
        responseType: 'blob',
        params: { account_type: accountType },
        signal,
        skipBusinessError: true,
        skipErrorHandler: true,
      })
      if (
        !['image/png', 'image/jpeg', 'image/webp'].includes(response.data.type)
      ) {
        throw new MailboxError('mailbox_attachment_invalid')
      }
      return response.data
    } catch (error) {
      if (isAxiosError(error)) {
        throw new MailboxError(
          error.response?.status === 410
            ? 'mailbox_attachment_expired'
            : 'mailbox_request_failed',
          error.response?.status ?? 0
        )
      }
      throw error
    }
  },
}
