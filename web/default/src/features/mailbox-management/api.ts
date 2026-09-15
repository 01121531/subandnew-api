import { isAxiosError } from 'axios'

import { api } from '@/lib/api'

import { MailboxError, safeCode } from './lib/errors'
import {
  accountMetadata,
  auditMetadata,
  importMetadata,
  issueMetadata,
  pageMetadata,
  submissionMetadata,
} from './lib/pools'
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
} from './types'

const base = '/api/mailbox-management'
export function importBody(
  source: ImportSource
): FormData | { format: string; text: string; account_type: AccountType } {
  if (source.file) {
    const body = new FormData()
    body.append('account_type', source.account_type ?? 'refund')
    body.append('file', source.file)
    return body
  }
  return {
    format: source.format,
    text: source.text,
    account_type: source.account_type ?? 'refund',
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
  params?: Partial<ListQuery>,
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
    request<{ temporary_cvv_enabled: boolean }>(
      '/import-options',
      'GET',
      undefined,
      undefined,
      signal
    ),
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
  operators: (query: ListQuery, signal?: AbortSignal) =>
    request<Page<Operator>>('/operators', 'GET', undefined, query, signal),
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
