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
import { useQuery } from '@tanstack/react-query'
import {
  Binary,
  ChevronDown,
  CircleDollarSign,
  Download,
  RefreshCw,
  RotateCcw,
  Search,
  SlidersHorizontal,
} from 'lucide-react'
import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { MultiSelect } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getManagedInstances } from '@/features/managed-instances/api'
import type { ManagedInstance } from '@/features/managed-instances/types'
import { cn } from '@/lib/utils'

import {
  createUsageRecordsExport,
  downloadUsageRecordsExport,
  getUsageRecords,
  getUsageRecordsExport,
  getUsageRecordFilterOptions,
  getUsageRecordSummary,
} from './api'
import { CompactDateTimeRangePicker } from './compact-date-time-range-picker'
import type {
  UsageRecord,
  UsageRecordFilters,
  UsageRecordSummary,
  UsageSystem,
} from './types'

type FilterOption = { value: string; label: string }
type FilterDefinition = {
  key: string
  label: string
  placeholder?: string
  options?: FilterOption[]
}
type SortDirection = 'asc' | 'desc'
type UsageSortKey = 'created_at' | 'model' | 'id'

const PAGE_SIZE = 20
const USAGE_RECORDS_REFRESH_MS = 120_000
const EMPTY_INSTANCES: ManagedInstance[] = []
const EMPTY_RECORDS: UsageRecord[] = []

const wait = (milliseconds: number) =>
  new Promise((resolve) => window.setTimeout(resolve, milliseconds))
const TIME_FILTER_KEYS = new Set([
  'start_time',
  'end_time',
  'start_date',
  'end_date',
])
const LOG_TYPES: FilterOption[] = [
  { value: '1', label: '充值' },
  { value: '2', label: '消费' },
  { value: '3', label: '管理' },
  { value: '4', label: '系统' },
  { value: '5', label: '错误' },
  { value: '6', label: '退款' },
  { value: '7', label: '登录' },
]

const NEW_API_FILTERS: FilterDefinition[] = [
  { key: 'start_time', label: '开始时间' },
  { key: 'end_time', label: '结束时间' },
  { key: 'type', label: '日志类型', options: LOG_TYPES },
  { key: 'username', label: '用户名', placeholder: '精确用户名' },
  { key: 'token_name', label: '令牌名称', placeholder: '精确令牌名称' },
  { key: 'model_name', label: '模型', placeholder: '模型名称' },
  { key: 'channel', label: '渠道 ID' },
  { key: 'group', label: '分组', placeholder: '分组名称' },
  { key: 'request_id', label: '请求 ID', placeholder: 'request_id' },
  {
    key: 'upstream_request_id',
    label: '上游请求 ID',
    placeholder: 'upstream_request_id',
  },
  { key: 'proxy_id', label: '代理 ID' },
]

const SUB2_FILTERS: FilterDefinition[] = [
  { key: 'start_date', label: '开始日期' },
  { key: 'end_date', label: '结束日期' },
  { key: 'user_id', label: '用户 ID' },
  { key: 'api_key_id', label: 'API Key ID' },
  { key: 'account_id', label: '账号 ID' },
  { key: 'group_id', label: '分组 ID' },
  { key: 'model', label: '模型', placeholder: '请求模型' },
  { key: 'request_id', label: '请求 ID', placeholder: 'request_id' },
  {
    key: 'request_type',
    label: '请求类型',
    options: [
      { value: 'sync', label: '同步' },
      { value: 'stream', label: '流式' },
      { value: 'ws_v2', label: 'WebSocket' },
      { value: 'live', label: 'Live' },
      { value: 'cyber', label: 'Cyber' },
    ],
  },
  {
    key: 'billing_type',
    label: '计费类型',
    options: [
      { value: '0', label: '余额' },
      { value: '1', label: '订阅' },
    ],
  },
  {
    key: 'billing_mode',
    label: '计费模式',
    options: [
      { value: 'token', label: 'Token' },
      { value: 'per_request', label: '按请求' },
      { value: 'image', label: '图片' },
      { value: 'video', label: '视频' },
    ],
  },
  {
    key: 'upstream_model_mismatch',
    label: '上游模型校验',
    options: [
      { value: 'true', label: '仅不一致' },
      { value: 'false', label: '仅一致' },
    ],
  },
]

function localDateTime(date: Date) {
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

function localDate(date: Date) {
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 10)
}

function scalarFilter(value: UsageRecordFilters[string] | undefined) {
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '')
}

function selectedFilters(value: UsageRecordFilters[string] | undefined) {
  if (Array.isArray(value)) return value
  return value ? [value] : []
}

function filterDate(
  value: UsageRecordFilters[string] | undefined,
  dateOnly: boolean
) {
  const normalized = scalarFilter(value)
  if (!normalized) return undefined
  const date = new Date(dateOnly ? `${normalized}T00:00` : normalized)
  return Number.isNaN(date.getTime()) ? undefined : date
}

function serializeRangeDate(system: UsageSystem, date?: Date) {
  if (!date) return ''
  return system === 'sub2api' ? localDate(date) : localDateTime(date)
}

function defaultFilters(system: UsageSystem): UsageRecordFilters {
  const end = new Date()
  const start = new Date(end.getTime() - 86_400_000)
  if (system === 'sub2api') {
    return {
      start_date: localDate(start),
      end_date: localDate(end),
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
    }
  }
  return { start_time: localDateTime(start), end_time: localDateTime(end) }
}

function requestFilters(
  system: UsageSystem,
  filters: UsageRecordFilters,
  page: number
) {
  const result: UsageRecordFilters = {
    page_size: String(PAGE_SIZE),
    [system === 'sub2api' ? 'page' : 'p']: String(page),
  }
  Object.entries(filters).forEach(([key, value]) => {
    if (!value || (Array.isArray(value) && value.length === 0)) return
    if (system === 'new_api' && (key === 'start_time' || key === 'end_time')) {
      const timestamp = Math.floor(
        new Date(scalarFilter(value)).getTime() / 1000
      )
      if (Number.isFinite(timestamp)) {
        result[key === 'start_time' ? 'start_timestamp' : 'end_timestamp'] =
          String(timestamp)
      }
      return
    }
    result[key] = value
  })
  return result
}

function summaryRequestFilters(
  system: UsageSystem,
  filters: UsageRecordFilters
) {
  const request = requestFilters(system, filters, 1)
  const keys =
    system === 'sub2api'
      ? ['start_date', 'end_date', 'timezone']
      : ['start_timestamp', 'end_timestamp']
  return Object.fromEntries(
    keys.flatMap((key) => (request[key] ? [[key, request[key]]] : []))
  )
}

function usageFilterError(
  system: UsageSystem,
  filters: UsageRecordFilters
): string | null {
  const start = filters[system === 'sub2api' ? 'start_date' : 'start_time']
  const end = filters[system === 'sub2api' ? 'end_date' : 'end_time']
  if (
    start &&
    end &&
    new Date(scalarFilter(start)).getTime() >
      new Date(scalarFilter(end)).getTime()
  ) {
    return '开始时间不能晚于结束时间'
  }
  return null
}

function belongsToSystem(instance: ManagedInstance, system: UsageSystem) {
  return system === 'sub2api'
    ? instance.kind === 'sub2api'
    : instance.kind === 'new_api' || instance.kind === 'huichuan'
}

function withUsageSort(
  system: UsageSystem,
  filters: UsageRecordFilters,
  sortKey: UsageSortKey,
  sortDirection: SortDirection
) {
  if (system !== 'sub2api') return filters
  return { ...filters, sort_by: sortKey, sort_order: sortDirection }
}

function usageSortValue(
  record: UsageRecord,
  system: UsageSystem,
  sortKey: UsageSortKey
) {
  if (sortKey === 'id') return number(record, 'id')
  if (sortKey === 'model') {
    return system === 'sub2api'
      ? text(record, 'model')
      : text(record, 'model_name')
  }

  const raw = value(record, 'created_at')
  if (typeof raw === 'number') return raw
  const timestamp = Date.parse(String(raw ?? ''))
  return Number.isNaN(timestamp) ? null : timestamp
}

function sortUsageRecords(
  records: UsageRecord[],
  system: UsageSystem,
  sortKey: UsageSortKey,
  sortDirection: SortDirection
) {
  return [...records].sort((left, right) => {
    const leftValue = usageSortValue(left, system, sortKey)
    const rightValue = usageSortValue(right, system, sortKey)
    const leftMissing = leftValue == null || leftValue === ''
    const rightMissing = rightValue == null || rightValue === ''
    if (leftMissing && rightMissing) return 0
    if (leftMissing) return 1
    if (rightMissing) return -1
    const comparison =
      typeof leftValue === 'number' && typeof rightValue === 'number'
        ? leftValue - rightValue
        : String(leftValue).localeCompare(String(rightValue), undefined, {
            numeric: true,
            sensitivity: 'base',
          })
    return sortDirection === 'asc' ? comparison : -comparison
  })
}

export function UsageRecords() {
  const { t } = useTranslation()
  const [system, setSystem] = useState<UsageSystem>('new_api')
  const [instanceId, setInstanceId] = useState('')
  const [draft, setDraft] = useState<UsageRecordFilters>(() =>
    defaultFilters('new_api')
  )
  const [applied, setApplied] = useState<UsageRecordFilters>(() =>
    defaultFilters('new_api')
  )
  const [page, setPage] = useState(1)
  const [exporting, setExporting] = useState(false)
  const [exportProgress, setExportProgress] = useState({
    progress: 0,
    processed: 0,
    total: 0,
  })
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [sortKey, setSortKey] = useState<UsageSortKey>('created_at')
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc')

  const instancesQuery = useQuery({
    queryKey: ['usage-record-instances'],
    queryFn: () => getManagedInstances({ search: '', kind: '', status: '' }),
    refetchInterval: USAGE_RECORDS_REFRESH_MS,
  })
  const allInstances = instancesQuery.data?.data.items ?? EMPTY_INSTANCES
  const instances = useMemo(
    () => allInstances.filter((instance) => belongsToSystem(instance, system)),
    [allInstances, system]
  )

  useEffect(() => {
    if (!instances.some((instance) => String(instance.id) === instanceId)) {
      setInstanceId(instances[0] ? String(instances[0].id) : '')
    }
  }, [instanceId, instances])

  const selectedId = Number(instanceId)
  const apiFilters = useMemo(
    () =>
      requestFilters(
        system,
        withUsageSort(system, applied, sortKey, sortDirection),
        page
      ),
    [applied, page, sortDirection, sortKey, system]
  )
  const summaryApiFilters = useMemo(
    () => summaryRequestFilters(system, applied),
    [applied, system]
  )
  const recordsQuery = useQuery({
    queryKey: ['usage-records', selectedId, apiFilters],
    queryFn: () => getUsageRecords(selectedId, apiFilters),
    enabled: selectedId > 0,
    retry: false,
    staleTime: USAGE_RECORDS_REFRESH_MS / 2,
    refetchInterval: USAGE_RECORDS_REFRESH_MS,
  })
  const summaryQuery = useQuery({
    queryKey: ['usage-record-summary', selectedId, summaryApiFilters],
    queryFn: () => getUsageRecordSummary(selectedId, summaryApiFilters),
    enabled: selectedId > 0,
    retry: false,
    staleTime: USAGE_RECORDS_REFRESH_MS / 2,
    refetchInterval: USAGE_RECORDS_REFRESH_MS,
  })
  const filterOptionsQuery = useQuery({
    queryKey: ['usage-record-filter-options', selectedId, summaryApiFilters],
    queryFn: () => getUsageRecordFilterOptions(selectedId, summaryApiFilters),
    enabled: selectedId > 0,
    retry: false,
    staleTime: USAGE_RECORDS_REFRESH_MS,
  })
  const result = recordsQuery.data?.data
  const records = result?.items ?? EMPTY_RECORDS
  const sortedRecords = useMemo(
    () => sortUsageRecords(records, system, sortKey, sortDirection),
    [records, sortDirection, sortKey, system]
  )
  const total = result?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const sub2PageSize = Math.max(1, result?.page_size ?? PAGE_SIZE)
  const hasNextPage =
    system === 'sub2api'
      ? result?.page === page &&
        (records.length >= sub2PageSize || page < totalPages)
      : page < totalPages
  const filters = system === 'sub2api' ? SUB2_FILTERS : NEW_API_FILTERS

  useEffect(() => {
    if (!result) return
    if (system === 'sub2api') {
      if (result.page === page && page > 1 && records.length === 0) {
        setPage((value) => value - 1)
      }
      return
    }
    if (page > totalPages) setPage(totalPages)
  }, [page, records.length, result, system, totalPages])

  const changeSystem = (value: UsageSystem) => {
    const defaults = defaultFilters(value)
    setSystem(value)
    setInstanceId('')
    setDraft(defaults)
    setApplied(defaults)
    setSortKey('created_at')
    setSortDirection('desc')
    setPage(1)
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    const validationError = usageFilterError(system, draft)
    if (validationError) {
      toast.error(validationError)
      return
    }
    setPage(1)
    setApplied({ ...draft })
    setFiltersOpen(false)
  }

  const reset = () => {
    const defaults = defaultFilters(system)
    setDraft(defaults)
    setApplied(defaults)
    setSortKey('created_at')
    setSortDirection('desc')
    setPage(1)
  }

  const download = async () => {
    if (!selectedId) return
    setExporting(true)
    setExportProgress({ progress: 0, processed: 0, total: 0 })
    try {
      const filters = requestFilters(
        system,
        withUsageSort(system, applied, sortKey, sortDirection),
        1
      )
      let task = (await createUsageRecordsExport(selectedId, filters)).data
      while (task.status === 'pending' || task.status === 'running') {
        setExportProgress({
          progress: Math.max(0, Math.min(100, task.state?.progress ?? 0)),
          processed: task.state?.processed ?? 0,
          total: task.state?.total ?? 0,
        })
        await wait(1000)
        task = (await getUsageRecordsExport(selectedId, task.task_id)).data
      }
      if (task.status !== 'succeeded') {
        throw new Error(task.error || 'usage_export_failed')
      }
      setExportProgress({
        progress: 100,
        processed: task.result?.record_count ?? task.state?.processed ?? 0,
        total: task.state?.total ?? task.result?.record_count ?? 0,
      })
      await downloadUsageRecordsExport(selectedId, task.task_id)
      toast.success('CSV 导出完成')
    } catch {
      toast.error('CSV 导出失败')
    } finally {
      setExporting(false)
    }
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Usage records', { defaultValue: '使用记录' })}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {exporting ? (
          <div className='hidden w-44 shrink-0 space-y-1 sm:block'>
            <div className='text-muted-foreground flex justify-between text-xs tabular-nums'>
              <span>正在导出</span>
              <span>
                {exportProgress.total > 0
                  ? `${exportProgress.processed.toLocaleString()} / ${exportProgress.total.toLocaleString()}`
                  : '准备数据'}
              </span>
            </div>
            <Progress value={exportProgress.progress} className='h-1.5' />
          </div>
        ) : null}
        <Button
          variant='outline'
          size='icon-sm'
          aria-label='刷新'
          onClick={() => {
            void recordsQuery.refetch()
            void summaryQuery.refetch()
          }}
          disabled={!selectedId}
        >
          <RefreshCw
            className={
              recordsQuery.isFetching || summaryQuery.isFetching
                ? 'animate-spin'
                : ''
            }
          />
        </Button>
        <Button
          size='sm'
          onClick={() => void download()}
          disabled={!selectedId || exporting}
        >
          <Download />
          {exporting ? `导出中 ${exportProgress.progress}%` : '导出 CSV'}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='grid gap-4'>
          <div className='border-border bg-card flex flex-wrap items-center gap-2 rounded-lg border p-2 shadow-xs'>
            <div className='bg-muted flex h-8 items-center rounded-md p-0.5'>
              {(['new_api', 'sub2api'] as const).map((value) => (
                <button
                  key={value}
                  type='button'
                  aria-pressed={system === value}
                  className={cn(
                    'h-7 rounded px-3 text-sm font-medium transition-colors',
                    system === value
                      ? 'bg-background text-foreground shadow-xs'
                      : 'text-muted-foreground hover:text-foreground'
                  )}
                  onClick={() => changeSystem(value)}
                >
                  {value === 'sub2api' ? 'Sub2API' : 'New API'}
                </button>
              ))}
            </div>
            <NativeSelect
              id='usage-record-instance'
              name='usage-record-instance'
              className='min-w-0 flex-1 sm:max-w-72'
              value={instanceId}
              onChange={(event) => {
                setInstanceId(event.target.value)
                setPage(1)
              }}
              aria-label='选择站点'
            >
              {instances.length === 0 && (
                <NativeSelectOption value=''>暂无可用站点</NativeSelectOption>
              )}
              {instances.map((instance) => (
                <NativeSelectOption key={instance.id} value={instance.id}>
                  {instance.name} · {instance.status}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <span className='text-muted-foreground hidden text-xs sm:inline'>
              {instances.length} 个站点
            </span>
          </div>

          <form
            onSubmit={submit}
            className='border-border bg-card rounded-lg border shadow-xs'
          >
            <div className='border-border flex items-center justify-between border-b px-3 py-2 sm:hidden'>
              <span className='text-sm font-medium'>筛选条件</span>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                aria-expanded={filtersOpen}
                aria-controls='usage-record-filters'
                onClick={() => setFiltersOpen((open) => !open)}
              >
                <SlidersHorizontal />
                筛选
                <ChevronDown
                  className={cn(
                    'transition-transform',
                    filtersOpen && 'rotate-180'
                  )}
                />
              </Button>
            </div>
            <div
              id='usage-record-filters'
              className={cn(
                'grid grid-cols-1 gap-3 p-3 sm:grid sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-6',
                !filtersOpen && 'hidden'
              )}
            >
              <div className='min-w-0 sm:col-span-2'>
                <CompactDateTimeRangePicker
                  start={filterDate(
                    draft[system === 'sub2api' ? 'start_date' : 'start_time'],
                    system === 'sub2api'
                  )}
                  end={filterDate(
                    draft[system === 'sub2api' ? 'end_date' : 'end_time'],
                    system === 'sub2api'
                  )}
                  onChange={({ start, end }) => {
                    const startKey =
                      system === 'sub2api' ? 'start_date' : 'start_time'
                    const endKey =
                      system === 'sub2api' ? 'end_date' : 'end_time'
                    setDraft((current) => ({
                      ...current,
                      [startKey]: serializeRangeDate(system, start),
                      [endKey]: serializeRangeDate(system, end),
                    }))
                  }}
                />
              </div>
              {filters
                .filter((filter) => !TIME_FILTER_KEYS.has(filter.key))
                .map((filter) => (
                  <FilterControl
                    key={filter.key}
                    definition={filter}
                    value={selectedFilters(draft[filter.key])}
                    dynamicOptions={
                      filterOptionsQuery.data?.data.fields[filter.key] ?? []
                    }
                    onChange={(value) =>
                      setDraft((current) => ({
                        ...current,
                        [filter.key]: value,
                      }))
                    }
                  />
                ))}
            </div>
            <div
              className={cn(
                'border-border items-center justify-end gap-2 border-t px-3 py-2 sm:flex',
                filtersOpen ? 'flex' : 'hidden'
              )}
            >
              <Button type='button' variant='ghost' size='sm' onClick={reset}>
                <RotateCcw />
                重置
              </Button>
              <Button type='submit' size='sm' disabled={!selectedId}>
                <Search />
                查询
              </Button>
            </div>
          </form>

          <UsageSummaryPanel
            summary={summaryQuery.data?.data}
            loading={summaryQuery.isLoading}
            error={summaryQuery.isError}
            hasInstance={selectedId > 0}
          />

          <div className='border-border bg-card overflow-hidden rounded-lg border shadow-xs'>
            <div className='border-border flex flex-wrap items-center justify-between gap-2 border-b px-3 py-2.5'>
              <div>
                <div className='text-sm font-semibold'>记录明细</div>
                <div className='text-muted-foreground text-xs tabular-nums'>
                  共 {total.toLocaleString('zh-CN')} 条
                </div>
              </div>
              <div className='flex max-w-full flex-wrap items-center justify-end gap-1.5'>
                <NativeSelect
                  value={sortKey}
                  onChange={(event) => {
                    setSortKey(event.target.value as UsageSortKey)
                    setPage(1)
                  }}
                  className='h-7 w-28 text-xs'
                  aria-label={t('Sort')}
                >
                  <NativeSelectOption value='created_at'>
                    时间
                  </NativeSelectOption>
                  <NativeSelectOption value='model'>模型</NativeSelectOption>
                  <NativeSelectOption value='id'>记录 ID</NativeSelectOption>
                </NativeSelect>
                <NativeSelect
                  value={sortDirection}
                  onChange={(event) => {
                    setSortDirection(event.target.value as SortDirection)
                    setPage(1)
                  }}
                  className='h-7 w-20 text-xs'
                  aria-label={t('Sort')}
                >
                  <NativeSelectOption value='desc'>
                    {t('Desc')}
                  </NativeSelectOption>
                  <NativeSelectOption value='asc'>
                    {t('Asc')}
                  </NativeSelectOption>
                </NativeSelect>
                <Badge variant='outline'>第 {page} 页</Badge>
              </div>
            </div>
            <UsageRecordsContent
              system={system}
              records={sortedRecords}
              loading={recordsQuery.isLoading || instancesQuery.isLoading}
              error={recordsQuery.isError || instancesQuery.isError}
              hasInstance={selectedId > 0}
              onRetry={() => void recordsQuery.refetch()}
            />
            {(total > 0 || records.length > 0) && (
              <div className='border-border flex items-center justify-between border-t px-3 py-2'>
                <span className='text-muted-foreground text-xs tabular-nums'>
                  {system === 'sub2api'
                    ? `第 ${page} 页 · 本页 ${records.length} 条`
                    : `${(page - 1) * PAGE_SIZE + 1}–${Math.min(page * PAGE_SIZE, total)} / ${total}`}
                </span>
                <div className='flex items-center gap-1'>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={page <= 1 || recordsQuery.isFetching}
                    onClick={() => setPage((value) => value - 1)}
                  >
                    上一页
                  </Button>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={!hasNextPage || recordsQuery.isFetching}
                    onClick={() => setPage((value) => value + 1)}
                  >
                    下一页
                  </Button>
                </div>
              </div>
            )}
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function UsageSummaryPanel(props: {
  summary?: UsageRecordSummary
  loading: boolean
  error: boolean
  hasInstance: boolean
}) {
  const amountIsQuota = props.summary?.currency === 'quota'
  let status = '当前查询时间范围'
  if (!props.hasInstance) status = '未选择站点'
  else if (props.error) status = '汇总读取失败'
  return (
    <section
      className='border-border bg-card overflow-hidden rounded-lg border shadow-xs'
      aria-label='时间范围汇总'
    >
      <div className='border-border flex items-center justify-between gap-3 border-b px-3 py-2.5'>
        <div className='text-sm font-semibold'>时间范围汇总</div>
        <div className='text-muted-foreground text-xs'>{status}</div>
      </div>
      <div className='grid sm:grid-cols-2'>
        <SummaryMetric
          icon={Binary}
          label='消耗 Tokens'
          value={formatTokenTotal(props.summary?.total_tokens)}
          loading={props.loading}
        />
        <SummaryMetric
          icon={CircleDollarSign}
          label={amountIsQuota ? '额度消耗' : '消费金额'}
          value={formatSummaryAmount(props.summary)}
          loading={props.loading}
          divided
        />
      </div>
    </section>
  )
}

function SummaryMetric(props: {
  icon: typeof Binary
  label: string
  value: string
  loading: boolean
  divided?: boolean
}) {
  const Icon = props.icon
  return (
    <div
      className={cn(
        'min-w-0 p-4',
        props.divided && 'border-border border-t sm:border-t-0 sm:border-l'
      )}
    >
      <div className='text-muted-foreground flex items-center gap-2 text-xs font-medium'>
        <Icon className='size-4' />
        {props.label}
      </div>
      {props.loading ? (
        <Skeleton className='mt-3 h-8 w-36' />
      ) : (
        <div className='mt-2 truncate text-2xl font-semibold tabular-nums'>
          {props.value}
        </div>
      )}
    </div>
  )
}

function FilterControl(props: {
  definition: FilterDefinition
  value: string[]
  dynamicOptions: FilterOption[]
  onChange: (value: string[]) => void
}) {
  const { definition } = props
  const controlId = `usage-filter-${definition.key}`
  const options = useMemo(() => {
    const values = new Map<string, FilterOption>()
    ;[...(definition.options ?? []), ...props.dynamicOptions].forEach(
      (option) => {
        if (option.value) values.set(option.value, option)
      }
    )
    return [...values.values()]
  }, [definition.options, props.dynamicOptions])
  return (
    <div className='grid min-w-0 gap-1.5'>
      <Label htmlFor={controlId} className='text-xs'>
        {definition.label}
      </Label>
      <MultiSelect
        id={controlId}
        options={options}
        selected={props.value}
        onChange={props.onChange}
        placeholder={definition.placeholder ?? '请选择或输入'}
        allowCreate
      />
    </div>
  )
}

function UsageRecordsContent(props: {
  system: UsageSystem
  records: UsageRecord[]
  loading: boolean
  error: boolean
  hasInstance: boolean
  onRetry: () => void
}) {
  if (props.loading) {
    return (
      <div className='grid gap-2 p-3'>
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className='h-10 w-full' />
        ))}
      </div>
    )
  }
  if (!props.hasInstance) {
    return <EmptyMessage text='当前系统还没有可查询的托管站点' />
  }
  if (props.error) {
    return (
      <div className='flex min-h-44 flex-col items-center justify-center gap-3 p-6 text-center'>
        <div>
          <div className='text-sm font-medium'>读取使用记录失败</div>
          <div className='text-muted-foreground mt-1 text-xs'>
            请检查站点状态、凭据和接口版本
          </div>
        </div>
        <Button variant='outline' size='sm' onClick={props.onRetry}>
          <RefreshCw />
          重试
        </Button>
      </div>
    )
  }
  if (props.records.length === 0) {
    return <EmptyMessage text='当前筛选条件下没有记录' />
  }
  return (
    <>
      <div className='hidden overflow-x-auto md:block'>
        {props.system === 'sub2api' ? (
          <Sub2Table records={props.records} />
        ) : (
          <NewAPITable records={props.records} />
        )}
      </div>
      <div className='divide-border divide-y md:hidden'>
        {props.records.map((record, index) => (
          <UsageMobileRow
            key={String(record.id ?? index)}
            system={props.system}
            record={record}
          />
        ))}
      </div>
    </>
  )
}

function NewAPITable({ records }: { records: UsageRecord[] }) {
  return (
    <Table className='min-w-[1180px]'>
      <TableHeader>
        <TableRow>
          {[
            '时间',
            '用户',
            '类型',
            '令牌',
            '模型',
            '渠道',
            '输入 / 输出',
            '额度',
            '耗时',
            '请求 ID',
          ].map((label) => (
            <TableHead key={label}>{label}</TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {records.map((record, index) => (
          <TableRow key={String(record.id ?? index)}>
            <TableCell className='whitespace-nowrap'>
              {formatTime(value(record, 'created_at'))}
            </TableCell>
            <TableCell className='font-medium'>
              {text(record, 'username')}
            </TableCell>
            <TableCell>
              <LogTypeBadge type={number(record, 'type')} />
            </TableCell>
            <EllipsisCell value={text(record, 'token_name')} />
            <EllipsisCell value={text(record, 'model_name')} />
            <EllipsisCell
              value={text(record, 'channel_name') || text(record, 'channel')}
            />
            <TableCell className='font-mono tabular-nums'>
              {formatNumber(number(record, 'prompt_tokens'))} /{' '}
              {formatNumber(number(record, 'completion_tokens'))}
            </TableCell>
            <TableCell className='font-mono tabular-nums'>
              {formatNumber(number(record, 'quota'))}
            </TableCell>
            <TableCell className='font-mono tabular-nums'>
              {formatDuration(number(record, 'use_time'), 's')}
            </TableCell>
            <EllipsisCell value={text(record, 'request_id')} mono />
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function Sub2Table({ records }: { records: UsageRecord[] }) {
  return (
    <Table className='min-w-[1320px]'>
      <TableHeader>
        <TableRow>
          {[
            '时间',
            '用户',
            'API Key',
            '账号',
            '请求模型',
            '上游模型',
            '分组',
            '类型',
            '输入 / 输出',
            '用户计费',
            '账号计费',
            '耗时',
            '请求 ID',
          ].map((label) => (
            <TableHead key={label}>{label}</TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {records.map((record, index) => (
          <TableRow key={String(record.id ?? index)}>
            <TableCell className='whitespace-nowrap'>
              {formatTime(value(record, 'created_at'))}
            </TableCell>
            <EllipsisCell
              value={text(record, 'user.email') || text(record, 'user_id')}
            />
            <EllipsisCell
              value={text(record, 'api_key.name') || text(record, 'api_key_id')}
            />
            <EllipsisCell
              value={text(record, 'account.name') || text(record, 'account_id')}
            />
            <EllipsisCell value={text(record, 'model')} />
            <EllipsisCell
              value={text(record, 'upstream_model') || text(record, 'model')}
            />
            <EllipsisCell
              value={text(record, 'group.name') || text(record, 'group_id')}
            />
            <TableCell>
              <Badge variant='outline'>
                {text(record, 'request_type') || '--'}
              </Badge>
            </TableCell>
            <TableCell className='font-mono tabular-nums'>
              {formatNumber(number(record, 'input_tokens'))} /{' '}
              {formatNumber(number(record, 'output_tokens'))}
            </TableCell>
            <TableCell className='font-mono tabular-nums'>
              {formatCurrency(number(record, 'actual_cost'))}
            </TableCell>
            <TableCell className='font-mono tabular-nums'>
              {formatCurrency(sub2AccountBilled(record))}
            </TableCell>
            <TableCell className='font-mono tabular-nums'>
              {formatDuration(number(record, 'duration_ms'), 'ms')}
            </TableCell>
            <EllipsisCell value={text(record, 'request_id')} mono />
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function UsageMobileRow(props: { system: UsageSystem; record: UsageRecord }) {
  const record = props.record
  const sub2 = props.system === 'sub2api'
  const user = sub2
    ? text(record, 'user.email') || text(record, 'user_id')
    : text(record, 'username')
  const model = text(record, sub2 ? 'model' : 'model_name')
  const input = number(record, sub2 ? 'input_tokens' : 'prompt_tokens')
  const output = number(record, sub2 ? 'output_tokens' : 'completion_tokens')
  return (
    <div className='grid gap-2 p-3'>
      <div className='flex min-w-0 items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='truncate text-sm font-medium'>{model || '--'}</div>
          <div className='text-muted-foreground truncate text-xs'>
            {user || '--'}
          </div>
        </div>
        <span className='text-muted-foreground shrink-0 text-xs'>
          {formatTime(value(record, 'created_at'))}
        </span>
      </div>
      <div className='grid grid-cols-3 gap-2 text-xs'>
        <Metric label='输入' value={formatNumber(input)} />
        <Metric label='输出' value={formatNumber(output)} />
        <Metric
          label={sub2 ? '计费' : '额度'}
          value={
            sub2
              ? formatCurrency(number(record, 'actual_cost'))
              : formatNumber(number(record, 'quota'))
          }
        />
      </div>
      <div
        className='text-muted-foreground truncate font-mono text-xs'
        title={text(record, 'request_id')}
      >
        {text(record, 'request_id') || '无请求 ID'}
      </div>
    </div>
  )
}

function Metric({
  label,
  value: metricValue,
}: {
  label: string
  value: string
}) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground'>{label}</div>
      <div className='truncate font-mono tabular-nums'>{metricValue}</div>
    </div>
  )
}

function EllipsisCell({
  value: cellValue,
  mono = false,
}: {
  value: string
  mono?: boolean
}) {
  return (
    <TableCell
      className={cn('max-w-44 truncate', mono && 'font-mono text-xs')}
      title={cellValue}
    >
      {cellValue || '--'}
    </TableCell>
  )
}

function LogTypeBadge({ type }: { type: number | null }) {
  const labels: Record<number, string> = {
    1: '充值',
    2: '消费',
    3: '管理',
    4: '系统',
    5: '错误',
    6: '退款',
    7: '登录',
  }
  return (
    <Badge variant={type === 5 ? 'destructive' : 'outline'}>
      {type == null ? '--' : (labels[type] ?? `#${type}`)}
    </Badge>
  )
}

function EmptyMessage({ text: message }: { text: string }) {
  return (
    <div className='text-muted-foreground flex min-h-44 items-center justify-center p-6 text-center text-sm'>
      {message}
    </div>
  )
}

function value(record: UsageRecord, path: string): unknown {
  let current: unknown = record
  for (const key of path.split('.')) {
    if (typeof current !== 'object' || current == null || !(key in current)) {
      return undefined
    }
    current = (current as Record<string, unknown>)[key]
  }
  return current
}

function text(record: UsageRecord, path: string) {
  const result = value(record, path)
  return result == null ? '' : String(result)
}

function number(record: UsageRecord, path: string): number | null {
  const raw = value(record, path)
  if (raw == null || raw === '') return null
  const result = Number(raw)
  return Number.isFinite(result) ? result : null
}

function sub2AccountBilled(record: UsageRecord): number | null {
  const base =
    number(record, 'account_stats_cost') ?? number(record, 'total_cost')
  if (base == null) return null
  return base * (number(record, 'account_rate_multiplier') ?? 1)
}

function formatTokenTotal(input: number | undefined) {
  return input == null
    ? '--'
    : input.toLocaleString(undefined, { maximumFractionDigits: 0 })
}

function formatSummaryAmount(summary: UsageRecordSummary | undefined) {
  if (!summary) return '--'
  return summary.currency === 'USD'
    ? formatCurrency(summary.amount)
    : formatNumber(summary.amount)
}

function formatNumber(input: number | null) {
  return input == null
    ? '--'
    : input.toLocaleString(undefined, { maximumFractionDigits: 4 })
}

function formatCurrency(input: number | null) {
  return input == null ? '--' : `$${input.toFixed(6)}`
}

function formatDuration(input: number | null, unit: 's' | 'ms') {
  return input == null ? '--' : `${input.toLocaleString()} ${unit}`
}

function formatTime(input: unknown) {
  if (input == null || input === '') return '--'
  const numeric = Number(input)
  const date = Number.isFinite(numeric)
    ? new Date(numeric < 10_000_000_000 ? numeric * 1000 : numeric)
    : new Date(String(input))
  return Number.isNaN(date.getTime())
    ? String(input)
    : date.toLocaleString(undefined, { hour12: false })
}
