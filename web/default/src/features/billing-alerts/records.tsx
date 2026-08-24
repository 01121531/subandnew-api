/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useQuery } from '@tanstack/react-query'
import { Download, Eye, FileClock, RefreshCw, Search } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  type AlertRecord,
  createAlertRecordExport,
  downloadAlertRecordExport,
  listAlertRecordExports,
  listAlertRecords,
} from './api'
import { formatDiscountPercent } from './discount'

const PAGE_SIZE = 20
const dateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'short',
})
const eventMeta: Record<string, { label: string; className: string }> = {
  metric_triggered: {
    label: '指标触发',
    className:
      'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/50 dark:text-red-300',
  },
  metric_recovered: {
    label: '指标恢复',
    className:
      'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-300',
  },
  threshold: {
    label: '金额预警',
    className:
      'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900 dark:bg-amber-950/50 dark:text-amber-300',
  },
  monitor_failure: {
    label: '监控异常',
    className:
      'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/50 dark:text-red-300',
  },
  monitor_recovery: {
    label: '监控恢复',
    className:
      'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-300',
  },
  metric_monitor_failure: {
    label: '指标监控异常',
    className:
      'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/50 dark:text-red-300',
  },
  metric_monitor_recovery: {
    label: '指标监控恢复',
    className:
      'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-300',
  },
}

function EventBadge({ type }: { type: string }) {
  const meta = eventMeta[type] ?? { label: type, className: '' }
  return (
    <Badge variant='outline' className={meta.className}>
      {meta.label}
    </Badge>
  )
}

function eventRowClass(type: string) {
  if (
    type === 'monitor_failure' ||
    type === 'metric_triggered' ||
    type === 'metric_monitor_failure'
  ) {
    return 'border-l-2 border-l-red-500'
  }
  if (
    type === 'monitor_recovery' ||
    type === 'metric_recovered' ||
    type === 'metric_monitor_recovery'
  ) {
    return 'border-l-2 border-l-emerald-500'
  }
  return 'border-l-2 border-l-amber-500'
}

const metricLabels: Record<string, string> = {
  rpm: 'RPM',
  rpm_capacity: 'RPM 最大容量',
  rpm_utilization: 'RPM 容量使用率',
  accounts_available: '可用账号',
  accounts_total: '全部账号',
  accounts_availability: '账号可用率',
  concurrency_used: '当前并发',
  concurrency_max: '最大并发',
  concurrency_utilization: '并发使用率',
  success_rate: '成功率',
  active_sessions: '活跃会话',
  today_cost: '今日费用',
  instance_connected: '实例连接状态',
  unhealthy_instances: '异常实例数',
}

function parseObject(value: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(value)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? parsed
      : {}
  } catch {
    return {}
  }
}

function metricValueSummary(item: AlertRecord) {
  const values = parseObject(item.observed_values)
  return Object.entries(values)
    .map(([key, value]) => `${metricLabels[key] ?? key} ${String(value)}`)
    .join(' · ')
}

function metricConditionSummary(item: AlertRecord) {
  try {
    const parsed = JSON.parse(item.conditions) as Array<{
      metric?: string
      operator?: string
      threshold?: string
    }>
    if (!Array.isArray(parsed)) return item.metric_key || '指标条件'
    return parsed
      .map(
        (condition) =>
          `${metricLabels[condition.metric ?? ''] ?? condition.metric} ${condition.operator ?? ''} ${condition.threshold ?? ''}`
      )
      .join(item.scope_mode === 'aggregate' ? ' · ' : ' / ')
  } catch {
    return item.metric_key || '指标条件'
  }
}

function deliveryVariant(
  status: string
): 'default' | 'destructive' | 'secondary' {
  if (status === 'sent') return 'default'
  if (status === 'failed') return 'destructive'
  return 'secondary'
}

function exportVariant(
  status: string
): 'default' | 'destructive' | 'secondary' {
  if (status === 'succeeded') return 'default'
  if (status === 'failed') return 'destructive'
  return 'secondary'
}

export function BillingAlertRecords({
  embedded = false,
}: {
  embedded?: boolean
}) {
  const [page, setPage] = useState(1)
  const [eventType, setEventType] = useState('')
  const [sourceType, setSourceType] = useState('')
  const [recipient, setRecipient] = useState('')
  const [selected, setSelected] = useState<AlertRecord | null>(null)
  const params = useMemo(() => {
    const value = new URLSearchParams({
      page: String(page),
      page_size: String(PAGE_SIZE),
    })
    if (eventType) value.set('event_type', eventType)
    if (sourceType) value.set('source_type', sourceType)
    if (recipient.trim()) value.set('recipient', recipient.trim())
    return value
  }, [eventType, page, recipient, sourceType])
  const query = useQuery({
    queryKey: ['billing-alert-records', params.toString()],
    queryFn: () => listAlertRecords(params),
    refetchInterval: 60_000,
  })
  const exportsQuery = useQuery({
    queryKey: ['billing-alert-record-exports'],
    queryFn: listAlertRecordExports,
    refetchInterval: (result) =>
      result.state.data?.data.some((item) =>
        ['pending', 'running'].includes(item.status)
      )
        ? 3_000
        : 30_000,
  })
  const data = query.data?.data
  const pages = Math.max(1, Math.ceil((data?.total ?? 0) / PAGE_SIZE))

  const content = (
    <div className='min-w-0'>
      <div className='mb-3 flex justify-end gap-2'>
        <Button
          variant='outline'
          size='sm'
          onClick={async () => {
            try {
              await createAlertRecordExport(params)
              toast.success('已加入后台导出队列')
              await exportsQuery.refetch()
            } catch {
              toast.error('创建导出任务失败')
            }
          }}
        >
          <Download />
          导出 CSV
        </Button>
        <Button
          variant='outline'
          size='icon-sm'
          aria-label='刷新预警记录'
          onClick={() => void query.refetch()}
        >
          <RefreshCw />
        </Button>
      </div>
      <div className='mb-4 grid gap-3 rounded-lg border p-4 sm:grid-cols-[180px_200px_minmax(240px,1fr)_auto]'>
        <NativeSelect
          value={sourceType}
          onChange={(e) => {
            setSourceType(e.target.value)
            setPage(1)
          }}
        >
          <NativeSelectOption value=''>全部来源</NativeSelectOption>
          <NativeSelectOption value='billing'>账单预警</NativeSelectOption>
          <NativeSelectOption value='metric'>指标预警</NativeSelectOption>
        </NativeSelect>
        <NativeSelect
          value={eventType}
          onChange={(e) => {
            setEventType(e.target.value)
            setPage(1)
          }}
        >
          <NativeSelectOption value=''>全部事件</NativeSelectOption>
          <NativeSelectOption value='threshold'>金额预警</NativeSelectOption>
          <NativeSelectOption value='metric_triggered'>
            指标触发
          </NativeSelectOption>
          <NativeSelectOption value='metric_recovered'>
            指标恢复
          </NativeSelectOption>
          <NativeSelectOption value='monitor_failure'>
            监控异常
          </NativeSelectOption>
          <NativeSelectOption value='monitor_recovery'>
            账单监控恢复
          </NativeSelectOption>
          <NativeSelectOption value='metric_monitor_failure'>
            指标监控异常
          </NativeSelectOption>
          <NativeSelectOption value='metric_monitor_recovery'>
            指标监控恢复
          </NativeSelectOption>
        </NativeSelect>
        <div className='relative'>
          <Search className='text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2' />
          <Input
            className='pl-9'
            value={recipient}
            onChange={(e) => {
              setRecipient(e.target.value)
              setPage(1)
            }}
            placeholder='筛选收件邮箱'
          />
        </div>
        <Badge variant='secondary' className='self-center tabular-nums'>
          共 {data?.total ?? 0} 条
        </Badge>
      </div>
      <div className='overflow-x-auto rounded-lg border'>
        <Table>
          <TableHeader className='bg-muted/40'>
            <TableRow>
              <TableHead>事件</TableHead>
              <TableHead>实例 / 规则</TableHead>
              <TableHead>触发条件</TableHead>
              <TableHead>观测结果</TableHead>
              <TableHead>范围 / 换算</TableHead>
              <TableHead>邮件</TableHead>
              <TableHead>时间</TableHead>
              <TableHead className='text-right'>操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(data?.items ?? []).map((item) => (
              <TableRow
                key={item.id}
                className={eventRowClass(item.event_type)}
              >
                <TableCell>
                  <EventBadge type={item.event_type} />
                </TableCell>
                <TableCell>
                  <div className='font-medium'>
                    {item.instance_name || `#${item.instance_id}`}
                  </div>
                  <div className='text-muted-foreground text-xs'>
                    {item.rule_name || `规则 #${item.rule_id}`} ·{' '}
                    {item.instance_kind}
                  </div>
                </TableCell>
                <TableCell>
                  {item.source_type === 'metric' ? (
                    <div className='max-w-72 text-sm break-words'>
                      {metricConditionSummary(item)}
                    </div>
                  ) : (
                    <>
                      <div>{item.threshold_name || '—'}</div>
                      <div className='text-muted-foreground text-xs tabular-nums'>
                        {item.currency && `${item.currency} ${item.threshold}`}
                      </div>
                    </>
                  )}
                </TableCell>
                <TableCell className='tabular-nums'>
                  {item.source_type === 'metric' ? (
                    <div className='max-w-64 text-sm break-words'>
                      {metricValueSummary(item) || item.error_code || '—'}
                    </div>
                  ) : (
                    <>
                      <div>${item.usd_total || '—'}</div>
                      <div className='text-muted-foreground text-xs'>
                        ¥{item.cny_total || '—'}
                      </div>
                    </>
                  )}
                </TableCell>
                <TableCell className='text-xs tabular-nums'>
                  {item.source_type === 'metric' ? (
                    <>
                      <div>
                        {item.scope_mode === 'aggregate'
                          ? '汇总监控'
                          : '实例独立监控'}
                      </div>
                      <div className='text-muted-foreground'>
                        {item.metric_key || '多指标'}
                      </div>
                    </>
                  ) : (
                    <>
                      <div>{item.exchange_rate || '—'}</div>
                      <div className='text-muted-foreground'>
                        折扣 {formatDiscountPercent(item.discount_rate)}
                      </div>
                    </>
                  )}
                </TableCell>
                <TableCell>
                  <div className='flex flex-wrap gap-1'>
                    {item.deliveries.map((delivery) => (
                      <Badge
                        key={delivery.id}
                        variant={deliveryVariant(delivery.status)}
                      >
                        {delivery.status}
                      </Badge>
                    ))}
                  </div>
                </TableCell>
                <TableCell className='whitespace-nowrap'>
                  {dateTime.format(new Date(item.created_at * 1000))}
                </TableCell>
                <TableCell className='text-right'>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    aria-label='查看详情'
                    onClick={() => setSelected(item)}
                  >
                    <Eye />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
            {!data?.items.length && (
              <TableRow>
                <TableCell
                  colSpan={8}
                  className='text-muted-foreground h-40 text-center'
                >
                  {query.isLoading ? '数据加载中' : '暂无预警记录'}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        <div className='flex items-center justify-between border-t px-4 py-3 text-sm'>
          <span className='text-muted-foreground'>
            第 {page} / {pages} 页
          </span>
          <div className='flex gap-2'>
            <Button
              size='sm'
              variant='outline'
              disabled={page <= 1}
              onClick={() => setPage(page - 1)}
            >
              上一页
            </Button>
            <Button
              size='sm'
              variant='outline'
              disabled={page >= pages}
              onClick={() => setPage(page + 1)}
            >
              下一页
            </Button>
          </div>
        </div>
      </div>
      {(exportsQuery.data?.data.length ?? 0) > 0 && (
        <section className='mt-5 overflow-x-auto rounded-lg border'>
          <div className='bg-muted/30 flex items-center gap-2 border-b px-4 py-3 text-sm font-medium'>
            <FileClock className='size-4' />
            最近导出
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>状态</TableHead>
                <TableHead>记录数</TableHead>
                <TableHead>文件</TableHead>
                <TableHead>创建时间</TableHead>
                <TableHead className='text-right'>操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(exportsQuery.data?.data ?? []).slice(0, 5).map((item) => (
                <TableRow key={item.id}>
                  <TableCell>
                    <Badge variant={exportVariant(item.status)}>
                      {exportStatusLabel(item.status)}
                    </Badge>
                  </TableCell>
                  <TableCell className='tabular-nums'>
                    {item.record_count.toLocaleString()}
                  </TableCell>
                  <TableCell>
                    {item.file_name || item.error_code || '准备中'}
                  </TableCell>
                  <TableCell>
                    {dateTime.format(new Date(item.created_at * 1000))}
                  </TableCell>
                  <TableCell className='text-right'>
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      aria-label='下载导出文件'
                      disabled={item.status !== 'succeeded'}
                      onClick={() =>
                        void downloadAlertRecordExport(item.task_id)
                      }
                    >
                      <Download />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </section>
      )}
      <Dialog
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
      >
        <DialogContent className='sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>预警记录详情</DialogTitle>
            <DialogDescription>
              {selected && `${selected.instance_name} · ${selected.rule_name}`}
            </DialogDescription>
          </DialogHeader>
          {selected && (
            <div className='grid gap-3 sm:grid-cols-2'>
              <Detail label='事件'>
                <EventBadge type={selected.event_type} />
              </Detail>
              {selected.source_type === 'metric' ? (
                <>
                  <Detail label='监控范围'>
                    {selected.scope_mode === 'aggregate'
                      ? '所选实例汇总'
                      : '每个实例独立'}
                  </Detail>
                  <Detail label='触发条件' className='sm:col-span-2'>
                    {metricConditionSummary(selected)}
                  </Detail>
                  <Detail label='观测值' className='sm:col-span-2'>
                    {metricValueSummary(selected) || '—'}
                  </Detail>
                </>
              ) : (
                <>
                  <Detail label='触发档位'>
                    {selected.threshold_name || '—'} {selected.currency}{' '}
                    {selected.threshold}
                  </Detail>
                  <Detail label='美元消耗'>${selected.usd_total || '—'}</Detail>
                  <Detail label='人民币账单'>
                    ¥{selected.cny_total || '—'}
                  </Detail>
                  <Detail label='折扣比例'>
                    {formatDiscountPercent(selected.discount_rate)}
                  </Detail>
                  <Detail label='USD/CNY 汇率'>
                    {selected.exchange_rate || '—'}
                  </Detail>
                </>
              )}
              <Detail label='收件人' className='sm:col-span-2'>
                {selected.deliveries.map((item) => (
                  <div
                    key={item.id}
                    className='flex justify-between border-b py-2 last:border-0'
                  >
                    <span>{item.recipient}</span>
                    <span className='text-muted-foreground'>
                      {item.status} · {item.attempts} 次
                      {item.last_error && ` · ${item.last_error}`}
                    </span>
                  </div>
                ))}
              </Detail>
              {selected.error_code && (
                <Detail label='错误代码' className='sm:col-span-2'>
                  <code className='text-destructive'>
                    {selected.error_code}
                  </code>
                </Detail>
              )}
            </div>
          )}
          <DialogFooter>
            <Button variant='outline' onClick={() => setSelected(null)}>
              关闭
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
  if (embedded) return content
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>预警记录</SectionPageLayout.Title>
      <SectionPageLayout.Content>{content}</SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function exportStatusLabel(status: string) {
  const labels: Record<string, string> = {
    pending: '等待中',
    running: '导出中',
    succeeded: '已完成',
    failed: '失败',
    expired: '已过期',
  }
  return labels[status] ?? status
}

function Detail({
  label,
  className,
  children,
}: {
  label: string
  className?: string
  children: ReactNode
}) {
  return (
    <div className={className}>
      <div className='text-muted-foreground mb-1 text-xs'>{label}</div>
      <div className='text-sm break-words'>{children}</div>
    </div>
  )
}
