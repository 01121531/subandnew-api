import { isAxiosError } from 'axios'

import { api } from '@/lib/api'

import { MailboxError, safeCode } from './lib/errors'
import type {
  Account,
  AccountOperator,
  AssignInput,
  Audit,
  Credential,
  Envelope,
  ImportPreview,
  ImportSource,
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
): FormData | { format: string; text: string } {
  if (source.file) {
    const body = new FormData()
    body.append('format', source.format)
    body.append('file', source.file)
    return body
  }
  return { format: source.format, text: source.text }
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
  params?: ListQuery,
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
  accounts: (query: ListQuery, signal?: AbortSignal) =>
    request<Page<Account>>('/accounts', 'GET', undefined, query, signal),
  accountOperators: (signal?: AbortSignal) =>
    request<AccountOperator[]>(
      '/account-operators',
      'GET',
      undefined,
      undefined,
      signal
    ),
  account: (id: number, signal?: AbortSignal) =>
    request<Account>(`/accounts/${id}`, 'GET', undefined, undefined, signal),
  preview: (source: ImportSource) =>
    request<ImportPreview>('/imports/preview', 'POST', importBody(source)),
  import: (source: ImportSource) =>
    request<{ imported: number }>('/imports', 'POST', importBody(source)),
  assign: (input: AssignInput) =>
    request<unknown>('/assignments', 'POST', input),
  credentials: (id: number, kind: 'password' | 'otp', signal?: AbortSignal) =>
    request<Credential>(
      `/accounts/${id}/credentials`,
      'POST',
      { kind },
      undefined,
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
  submissions: (query: ListQuery, signal?: AbortSignal) =>
    request<Page<Submission>>('/submissions', 'GET', undefined, query, signal),
  review: (id: number, input: ReviewInput) =>
    request<unknown>(`/submissions/${id}/review`, 'POST', input),
  audits: (query: ListQuery, signal?: AbortSignal) =>
    request<Page<Audit>>('/audits', 'GET', undefined, query, signal),
  attachment: async (id: string, signal?: AbortSignal): Promise<Blob> => {
    try {
      const response = await api.request<Blob>({
        url: `${base}/attachments/${encodeURIComponent(id)}`,
        responseType: 'blob',
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
