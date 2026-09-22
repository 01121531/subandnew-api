import { api } from '@/lib/api'

export type DailyReportMetric = {
  requests: number
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  total_tokens: number
  cost: number
  currency: string
}
export type DailyReportRow = {
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
}

export async function getDailyReports(params: {
  date: string
  instance_ids?: number[]
  source?: string
}) {
  const query = new URLSearchParams({ date: params.date })
  if (params.instance_ids?.length) {
    query.set('instance_ids', params.instance_ids.join(','))
  }
  if (params.source) query.set('source', params.source)
  const response = await api.get<{
    success: boolean
    data: DailyReportOverview
  }>(`/api/daily-reports/overview?${query.toString()}`, {
    disableDuplicate: true,
  })
  return response.data
}

export async function listDailyReportRules() {
  const response = await api.get<{
    success: boolean
    data: { items: DailyReportRule[] }
  }>('/api/daily-report-rules')
  return response.data
}

export async function exportDailyReport(params: {
  date: string
  instance_ids?: number[]
  source?: string
}) {
  const query = new URLSearchParams({ date: params.date })
  if (params.instance_ids?.length) {
    query.set('instance_ids', params.instance_ids.join(','))
  }
  if (params.source) query.set('source', params.source)
  const response = await api.post(
    `/api/daily-reports/export?${query.toString()}`,
    null,
    { responseType: 'blob', disableDuplicate: true }
  )
  const disposition = String(response.headers['content-disposition'] ?? '')
  const filename =
    disposition.match(/filename="?([^";]+)"?/i)?.[1] ?? 'daily-report.xlsx'
  const objectUrl = URL.createObjectURL(response.data)
  const anchor = document.createElement('a')
  anchor.href = objectUrl
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(objectUrl)
}
