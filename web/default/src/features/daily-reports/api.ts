import { api } from '@/lib/api'

type ApiResponse<T> = { success: boolean; message?: string; data: T }

type DailyReportMetric = {
  requests: number
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  total_tokens: number
  cost: number
  currency: string
}
type DailyReportRow = {
  snapshot: {
    instance_id: number
    snapshot_date: string
    source: string
    account_count: number
    active_count: number
    channel_count: number
    upload_count: number
    status: string
    stale: boolean
    observed_at: number
    error_code?: string
    supplier_code: string
    supplier_name: string
  }
  full: DailyReportMetric
  filtered: DailyReportMetric
}
export type DailyReportOverview = {
  date: string
  timezone: string
  items: DailyReportRow[]
  collected_at: number
}
export type DailyReportRule = {
  id: number
  instance_id: number
  supplier_code: string
  supplier_name: string
  filter?: unknown
  enabled: boolean
  version: number
  source_template_id?: number
  source_template_name?: string
}

/** @public Kept for legacy Router bill clients. */
export type SupplierBill = {
  instance_id: number
  supplier_code: string
  supplier_name: string
  bill_date: string
  timezone: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  original_amount: number
  payable_amount: number
  currency: string
  token_details?: Record<string, number>
  channel_count: number
  observed_at: number
  status: string
  error_code?: string
}

/** @public Kept for legacy Router bill clients. */
export type SupplierBillsOverview = {
  start_date: string
  end_date: string
  timezone: string
  bill_count: number
  total_requests: number
  total_payable: number
  items: SupplierBill[]
  collected_at: number
}

export type UploadRecord = {
  instance_id: number
  id: number
  vendor_id: string
  vendor: string
  batch: string
  account_identifier: string
  state: string
  issue_type?: string
  submitted: boolean
  created_at: string
  updated_at: string
  beijing_date: string
}

export type UploadDay = {
  date: string
  upload_count: number
  submitted: number
  needs_fix: number
  by_vendor: Record<string, number>
  by_state: Record<string, number>
}

export type UploadsOverview = {
  start_date: string
  end_date: string
  timezone: string
  days: UploadDay[]
  items: UploadRecord[]
  collected_at: number
}

export type SupplierOption = { code: string; name: string }

export async function getDailyReports(params: {
  date: string
  instance_ids?: number[]
  instance_kind?: string
  source?: string
  supplier_code?: string
  rule_id?: number
}) {
  const query = new URLSearchParams({ date: params.date })
  if (params.instance_ids?.length) {
    query.set('instance_ids', params.instance_ids.join(','))
  }
  if (params.instance_kind) query.set('instance_kind', params.instance_kind)
  if (params.source) query.set('source', params.source)
  if (params.supplier_code) query.set('supplier_code', params.supplier_code)
  if (params.rule_id) query.set('rule_id', String(params.rule_id))
  const response = await api.get<{
    success: boolean
    data: DailyReportOverview
  }>(`/api/daily-reports/overview?${query.toString()}`, {
    disableDuplicate: true,
  })
  return response.data
}

/** @public Kept for legacy Router bill clients. */
export async function getDailyReportSupplierBills(params: {
  start_date: string
  end_date: string
  instance_ids?: number[]
}) {
  const query = new URLSearchParams({
    start_date: params.start_date,
    end_date: params.end_date,
  })
  if (params.instance_ids?.length) {
    query.set('instance_ids', params.instance_ids.join(','))
  }
  const response = await api.get<{
    success: boolean
    data: SupplierBillsOverview
  }>(`/api/daily-reports/suppliers/overview?${query.toString()}`, {
    disableDuplicate: true,
  })
  return response.data
}

export async function getDailyReportUploads(params: {
  start_date: string
  end_date: string
  instance_ids?: number[]
}) {
  const query = new URLSearchParams({
    start_date: params.start_date,
    end_date: params.end_date,
  })
  if (params.instance_ids?.length) {
    query.set('instance_ids', params.instance_ids.join(','))
  }
  const response = await api.get<{
    success: boolean
    data: UploadsOverview
  }>(`/api/daily-reports/uploads/overview?${query.toString()}`, {
    disableDuplicate: true,
  })
  return response.data
}

export async function listDailyReportSupplierOptions(instanceId: number) {
  const response = await api.get<{
    success: boolean
    data: { items: SupplierOption[] }
  }>(`/api/daily-report-rules/supplier-options?instance_id=${instanceId}`, {
    disableDuplicate: true,
  })
  return response.data
}

export async function pushDailyReportFilterTemplate(input: {
  instance_id: number
  supplier_code: string
  template_id: number
  enabled: boolean
  replace_existing?: boolean
  version?: number
}) {
  const response = await api.post<ApiResponse<DailyReportRule>>(
    '/api/daily-report-rules/from-filter-template',
    input,
    { disableDuplicate: true, skipErrorHandler: true }
  )
  return response.data
}

export async function listDailyReportRules() {
  const response = await api.get<{
    success: boolean
    data: { items: DailyReportRule[] }
  }>('/api/daily-report-rules')
  return response.data
}

async function downloadDailyReportExport(
  path: string,
  params: Record<string, string>,
  fallbackFilename: string
) {
  const query = new URLSearchParams(params)
  const response = await api.post(`${path}?${query.toString()}`, null, {
    responseType: 'blob',
    disableDuplicate: true,
  })
  const disposition = String(response.headers['content-disposition'] ?? '')
  const filename =
    disposition.match(/filename="?([^";]+)"?/i)?.[1] ?? fallbackFilename
  const objectUrl = URL.createObjectURL(response.data)
  const anchor = document.createElement('a')
  anchor.href = objectUrl
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(objectUrl)
}

export function exportDailyReportAccounts(params: {
  date: string
  instance_ids?: number[]
  instance_kind?: string
  supplier_code?: string
  rule_id?: number
  mode?: 'full' | 'filtered'
}) {
  return downloadDailyReportExport(
    '/api/daily-reports/accounts/export',
    {
      date: params.date,
      ...(params.instance_kind ? { instance_kind: params.instance_kind } : {}),
      ...(params.supplier_code ? { supplier_code: params.supplier_code } : {}),
      ...(params.rule_id ? { rule_id: String(params.rule_id) } : {}),
      ...(params.mode ? { mode: params.mode } : {}),
      ...(params.instance_ids?.length
        ? { instance_ids: params.instance_ids.join(',') }
        : {}),
    },
    'daily-accounts.xlsx'
  )
}

/** @public Kept for legacy Router bill clients. */
export function exportDailyReportSuppliers(params: {
  start_date: string
  end_date: string
  instance_ids?: number[]
}) {
  return downloadDailyReportExport(
    '/api/daily-reports/suppliers/export',
    {
      start_date: params.start_date,
      end_date: params.end_date,
      ...(params.instance_ids?.length
        ? { instance_ids: params.instance_ids.join(',') }
        : {}),
    },
    'supplier-bills.xlsx'
  )
}

export function exportDailyReportUploads(params: {
  start_date: string
  end_date: string
  instance_ids?: number[]
}) {
  return downloadDailyReportExport(
    '/api/daily-reports/uploads/export',
    {
      start_date: params.start_date,
      end_date: params.end_date,
      ...(params.instance_ids?.length
        ? { instance_ids: params.instance_ids.join(',') }
        : {}),
    },
    'nevermore-uploads.xlsx'
  )
}
