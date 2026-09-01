/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { ChevronRight, Filter } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import type { UsageRecordExportTask } from '@/features/usage-records/api'
import { cn } from '@/lib/utils'

import { EXPORT_STATUS_META, exportInstanceKindLabel } from './status-meta'

const FILTER_LABELS: Record<string, string> = {
  start_timestamp: '开始时间',
  end_timestamp: '结束时间',
  start_date: '开始日期',
  end_date: '结束日期',
  type: '日志类型',
  username: '用户名',
  token_name: '令牌名称',
  model_name: '模型',
  channel: '渠道 ID',
  group: '分组',
  request_id: '请求 ID',
  upstream_request_id: '上游请求 ID',
  proxy_id: '代理 ID',
  user_id: '用户 ID',
  api_key_id: 'API Key ID',
  account_id: '账号 ID',
  group_id: '分组 ID',
  model: '模型',
  request_type: '请求类型',
  billing_type: '计费类型',
  billing_mode: '计费模式',
  upstream_model_mismatch: '上游模型校验',
  timezone: '时区',
  sort_by: '排序字段',
  sort_order: '排序方向',
  exact_total: '精确统计总数',
  report_type: '报表类型',
}

const TIME_KEYS = new Set([
  'start_timestamp',
  'end_timestamp',
  'start_date',
  'end_date',
])
const EXPORT_PARAMETER_KEYS = new Set([
  'timezone',
  'sort_by',
  'sort_order',
  'exact_total',
  'report_type',
])

const VALUE_LABELS: Record<string, Record<string, string>> = {
  type: {
    '1': '充值',
    '2': '消费',
    '3': '管理',
    '4': '系统',
    '5': '错误',
    '6': '退款',
    '7': '登录',
  },
  request_type: {
    sync: '同步',
    stream: '流式',
    ws_v2: 'WebSocket',
    live: 'Live',
    cyber: 'Cyber',
  },
  billing_type: { '0': '余额', '1': '订阅' },
  billing_mode: {
    token: 'Token',
    per_request: '按请求',
    image: '图片',
    video: '视频',
  },
  upstream_model_mismatch: { true: '仅不一致', false: '仅一致' },
  sort_by: { created_at: '时间', model: '模型', id: 'ID' },
  sort_order: { asc: '升序', desc: '降序' },
  exact_total: { true: '是', false: '否' },
  report_type: { account_costs: '账号历史消费' },
}

type FilterEntry = {
  key: string
  label: string
  values: string[]
}

const fullDateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'medium',
})

function nonEmptyFilterEntries(filters: Record<string, string[]>) {
  return Object.entries(filters)
    .filter(([, values]) => values.length > 0)
    .map(([key, values]) => ({
      key,
      label: FILTER_LABELS[key] ?? key,
      values,
    }))
}

function formatFilterValue(key: string, value: string) {
  if (key === 'start_timestamp' || key === 'end_timestamp') {
    const timestamp = Number(value)
    if (Number.isFinite(timestamp) && timestamp > 0) {
      return fullDateTime.format(new Date(timestamp * 1000))
    }
  }
  const label = VALUE_LABELS[key]?.[value]
  return label && label !== value ? `${label} (${value})` : value
}

function getFilterSummary(filters: Record<string, string[]>) {
  const entries = nonEmptyFilterEntries(filters)
  const visible = entries.filter(({ key }) => !EXPORT_PARAMETER_KEYS.has(key))
  const hasTimeRange = visible.some(({ key }) => TIME_KEYS.has(key))
  const conditionCount = visible.filter(({ key }) => !TIME_KEYS.has(key)).length
  let label = '全部记录'
  if (hasTimeRange && conditionCount > 0) {
    label = `时间范围 · ${conditionCount} 个条件`
  } else if (hasTimeRange) {
    label = '已设置时间范围'
  } else if (conditionCount > 0) {
    label = `${conditionCount} 个筛选条件`
  }
  return { label, count: visible.length }
}

function accountExportEntries(item: UsageRecordExportTask): FilterEntry[] {
  const snapshot = item.snapshot ?? {}
  const window =
    snapshot.window && typeof snapshot.window === 'object'
      ? (snapshot.window as Record<string, unknown>)
      : {}
  const entries: FilterEntry[] = []
  const add = (key: string, label: string, value: unknown) => {
    if (value == null || value === '') return
    entries.push({ key, label, values: [String(value)] })
  }
  add('start_timestamp', '开始时间', window.start)
  add('end_timestamp', '结束时间', window.end)
  add('timezone', '时区', window.timezone)
  add('search', '包含搜索', snapshot.search)
  add('exclude_search', '排除搜索', snapshot.exclude_search)
  add('sort_by', '排序字段', snapshot.sort_by)
  add('sort_order', '排序方向', snapshot.sort_order)
  add('report_type', '报表类型', snapshot.report_type)
  add('source', '数据来源', snapshot.source)
  add('selection_count', '账号数量', snapshot.selection_count)
  add('instance_count', '实例数量', snapshot.instance_count)
  return entries
}

export function ExportStatusBadge({
  status,
  className,
}: {
  status: UsageRecordExportTask['status']
  className?: string
}) {
  const meta = EXPORT_STATUS_META[status]
  const Icon = meta.icon
  return (
    <Badge
      variant='outline'
      className={cn('gap-1 font-medium', meta.badgeClassName, className)}
    >
      <Icon className={cn(status === 'running' && 'animate-spin')} />
      {meta.label}
    </Badge>
  )
}

function FilterSection({
  title,
  entries,
}: {
  title: string
  entries: FilterEntry[]
}) {
  if (entries.length === 0) return null
  return (
    <section className='space-y-2'>
      <h3 className='text-muted-foreground text-xs font-medium'>{title}</h3>
      <div className='border-border divide-border divide-y rounded-lg border'>
        {entries.map((entry) => (
          <div
            key={entry.key}
            className='grid gap-1.5 px-3 py-2.5 sm:grid-cols-[9rem_minmax(0,1fr)] sm:gap-3'
          >
            <div className='text-muted-foreground text-xs'>{entry.label}</div>
            <div className='flex min-w-0 flex-wrap gap-1.5'>
              {entry.values.map((value) => (
                <span
                  key={`${entry.key}-${value}`}
                  className='bg-muted text-foreground max-w-full rounded-md px-2 py-1 font-mono text-xs break-all whitespace-normal'
                >
                  {formatFilterValue(entry.key, value)}
                </span>
              ))}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

export function FilterDetailsDialog({ item }: { item: UsageRecordExportTask }) {
  const accountSelectionExport =
    item.export_kind === 'accounts' || item.export_kind === 'account_costs'
  const entries = accountSelectionExport
    ? accountExportEntries(item)
    : nonEmptyFilterEntries(item.filters)
  const timeEntries = entries.filter(({ key }) => TIME_KEYS.has(key))
  const parameterEntries = entries.filter(({ key }) =>
    EXPORT_PARAMETER_KEYS.has(key)
  )
  const conditionEntries = entries.filter(
    ({ key }) => !TIME_KEYS.has(key) && !EXPORT_PARAMETER_KEYS.has(key)
  )
  const summary = accountSelectionExport
    ? {
        label: `${String(item.snapshot?.selection_count ?? item.record_count ?? 0)} 个账号`,
        count: entries.length,
      }
    : getFilterSummary(item.filters)
  let dialogTitle = '筛选条件详情'
  if (item.export_kind === 'account_costs') {
    dialogTitle = '账号历史消费导出详情'
  } else if (item.export_kind === 'accounts') {
    dialogTitle = '账号导出详情'
  }

  return (
    <Dialog>
      <DialogTrigger
        render={
          <button
            type='button'
            className='border-border hover:bg-muted/60 focus-visible:border-ring focus-visible:ring-ring/50 group flex w-full min-w-0 items-center gap-2 rounded-md border bg-transparent px-2.5 py-2 text-left transition-colors outline-none focus-visible:ring-[3px]'
            aria-label={`查看筛选条件详情：${summary.label}`}
          />
        }
      >
        <Filter className='text-primary size-4 shrink-0' />
        <span className='min-w-0 flex-1 truncate text-xs font-medium'>
          {summary.label}
        </span>
        {summary.count > 0 && (
          <span className='bg-muted text-muted-foreground shrink-0 rounded px-1.5 py-0.5 text-[11px] tabular-nums'>
            {summary.count}
          </span>
        )}
        <ChevronRight className='text-muted-foreground size-3.5 shrink-0 transition-transform group-hover:translate-x-0.5' />
      </DialogTrigger>
      <DialogContent className='max-h-[min(82vh,760px)] gap-0 overflow-hidden p-0 sm:max-w-2xl'>
        <DialogHeader className='border-border border-b px-5 py-4 pr-12'>
          <div className='flex flex-wrap items-center gap-2'>
            <div className='bg-primary/10 text-primary flex size-8 items-center justify-center rounded-md'>
              <Filter className='size-4' />
            </div>
            <DialogTitle>{dialogTitle}</DialogTitle>
            <ExportStatusBadge status={item.status} />
          </div>
          <DialogDescription>
            {`${item.instance_name} · ${exportInstanceKindLabel(item.instance_kind)} · 只读快照`}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-5 overflow-y-auto px-5 py-4'>
          {entries.length === 0 ? (
            <div className='border-border bg-muted/30 flex min-h-32 flex-col items-center justify-center rounded-lg border border-dashed text-center'>
              <Filter className='text-muted-foreground mb-2 size-5' />
              <div className='text-sm font-medium'>全部记录</div>
              <div className='text-muted-foreground mt-1 text-xs'>
                此任务未设置额外筛选条件
              </div>
            </div>
          ) : (
            <>
              <FilterSection title='时间范围' entries={timeEntries} />
              <FilterSection title='筛选条件' entries={conditionEntries} />
              <FilterSection title='导出参数' entries={parameterEntries} />
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
