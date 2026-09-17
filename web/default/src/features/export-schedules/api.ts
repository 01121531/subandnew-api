/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { AccountFilterRuleInput } from '@/features/managed-accounts/account-filtering'
import { api } from '@/lib/api'

export type ScheduleConfig = {
  schedule: {
    kind: 'daily' | 'weekly' | 'interval'
    hour: number
    minute: number
    weekday: number
    interval_hours: number
    first_at: number
  }
  period: {
    kind: 'today' | 'yesterday' | 'last7' | 'last30' | 'fixed'
    start_date?: string
    end_date?: string
  }
  scope: 'dynamic' | 'fixed'
  query: {
    instance_ids: number[]
    dataset: 'inventory' | 'account_output'
    preset_days: number
    include_terms?: string[]
    exclude_terms?: string[]
    search?: string
    match_mode: 'all' | 'any'
    rules: AccountFilterRuleInput[]
    sort_by: string
    sort_order: string
  }
  accounts: { instance_id: number; account_id: string }[]
  recipients: string[]
  locale: string
}
export type ExportSchedule = {
  id: number
  owner_id: number
  name: string
  version: number
  enabled: boolean
  next_at: number
  active_run_id: number
  error_code: string
  config: ScheduleConfig
}
type Delivery = {
  id: number
  recipient: string
  status: string
  version: number
  attempts: number
  error_code: string
  next_attempt_at: number
  sent_at: number
}
export type ScheduleRun = {
  id: number
  scheduled_at: number
  task_id: string
  status: string
  missing_count: number
  error_code: string
  export: {
    status: string
    file_name: string
    file_size: number
    error_code: string
  } | null
  deliveries: Delivery[]
}
const base = '/api/managed-account-export-schedules'
const options = { skipErrorHandler: true, disableDuplicate: true }
export async function listSchedules(page = 1) {
  const data = (
    await api.get<{
      data: { items: ExportSchedule[]; total: number; smtp_ready: boolean }
    }>(base, { params: { page, page_size: 20 }, skipErrorHandler: true })
  ).data.data
  return {
    ...data,
    items: data.items.map((plan) => ({
      ...plan,
      config: {
        ...plan.config,
        accounts: plan.config.accounts ?? [],
        query: {
          ...plan.config.query,
          rules: plan.config.query.rules ?? [],
        },
      },
    })),
  }
}
export async function saveSchedule(
  input: Pick<ExportSchedule, 'name' | 'enabled' | 'version' | 'config'>,
  id?: number
) {
  return (
    await (id
      ? api.put(`${base}/${id}`, input, options)
      : api.post(base, input, options))
  ).data
}
export async function scheduleAction(
  plan: ExportSchedule,
  action: 'pause' | 'resume' | 'delete' | 'execute'
) {
  return action === 'delete'
    ? api.delete(`${base}/${plan.id}`, {
        ...options,
        data: { version: plan.version },
      })
    : api.post(
        `${base}/${plan.id}/${action}`,
        { version: plan.version },
        options
      )
}
export async function scheduleRuns(id: number, page = 1) {
  return (
    await api.get<{ data: { items: ScheduleRun[]; total: number } }>(
      `${base}/${id}/runs`,
      { params: { page }, skipErrorHandler: true }
    )
  ).data.data
}
export async function retryDelivery(
  planID: number,
  delivery: Delivery,
  confirmed: boolean
) {
  return api.post(
    `${base}/${planID}/deliveries/${delivery.id}/retry`,
    { version: delivery.version, confirmed },
    options
  )
}

export function newSchedule(
  query: ScheduleConfig['query'],
  accounts: ScheduleConfig['accounts'] = [],
  locale = 'zh-CN'
): Pick<ExportSchedule, 'name' | 'enabled' | 'version' | 'config'> {
  return {
    name: '',
    enabled: false,
    version: 0,
    config: {
      query,
      accounts,
      scope: 'dynamic',
      recipients: [],
      locale,
      schedule: {
        kind: 'daily',
        hour: 9,
        minute: 0,
        weekday: 1,
        interval_hours: 1,
        first_at: Math.floor(Date.now() / 1000) + 3600,
      },
      period: { kind: 'last30' },
    },
  }
}
export function shanghaiInput(timestamp: number) {
  return new Intl.DateTimeFormat('sv-SE', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  })
    .format(new Date(timestamp * 1000))
    .replace(' ', 'T')
}
export function parseShanghaiInput(value: string) {
  const millis = Date.parse(`${value}:00+08:00`)
  return Number.isFinite(millis) ? Math.floor(millis / 1000) : 0
}
export function splitRecipients(value: string) {
  return [
    ...new Set(
      value
        .split(/[,;\n\r]+/)
        .map((v) => v.trim())
        .filter(Boolean)
    ),
  ]
}
