/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useQuery } from '@tanstack/react-query'
import { Eye, RefreshCw, Search } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
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

import { type InstanceAlert, listInstanceAlerts } from './api'

const PAGE_SIZE = 20
const dateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'short',
})

type AlertInstanceOption = { id: number; name: string }

export function InstanceAlerts({
  instances,
}: {
  instances: AlertInstanceOption[]
}) {
  const [page, setPage] = useState(1)
  const [instanceId, setInstanceId] = useState('')
  const [status, setStatus] = useState('')
  const [alertType, setAlertType] = useState('')
  const [deliveryStatus, setDeliveryStatus] = useState('')
  const [search, setSearch] = useState('')
  const [startTime, setStartTime] = useState('')
  const [endTime, setEndTime] = useState('')
  const [selected, setSelected] = useState<InstanceAlert | null>(null)
  const params = useMemo(() => {
    const value = new URLSearchParams({
      page: String(page),
      page_size: String(PAGE_SIZE),
    })
    if (instanceId) value.set('instance_id', instanceId)
    if (status) value.set('status', status)
    if (alertType) value.set('alert_type', alertType)
    if (deliveryStatus) value.set('delivery_status', deliveryStatus)
    if (search.trim()) value.set('search', search.trim())
    if (startTime) value.set('start_time', String(localDateTimeUnix(startTime)))
    if (endTime) value.set('end_time', String(localDateTimeUnix(endTime)))
    return value
  }, [
    alertType,
    deliveryStatus,
    endTime,
    instanceId,
    page,
    search,
    startTime,
    status,
  ])
  const query = useQuery({
    queryKey: ['billing-instance-alerts', params.toString()],
    queryFn: () => listInstanceAlerts(params),
    refetchInterval: 60_000,
  })
  const data = query.data?.data
  const pages = Math.max(1, Math.ceil((data?.total ?? 0) / PAGE_SIZE))
  const resetPage = () => setPage(1)

  return (
    <div className='min-w-0'>
      <div className='mb-3 flex justify-end'>
        <Button
          variant='outline'
          size='icon-sm'
          className='size-11 sm:size-8'
          aria-label='刷新实例预警'
          onClick={() => void query.refetch()}
        >
          <RefreshCw className={query.isFetching ? 'animate-spin' : ''} />
        </Button>
      </div>
      <div className='mb-4 grid gap-3 rounded-lg border p-4 sm:grid-cols-2 xl:grid-cols-4'>
        <NativeSelect
          value={instanceId}
          aria-label='筛选实例'
          onChange={(event) => {
            setInstanceId(event.target.value)
            resetPage()
          }}
        >
          <NativeSelectOption value=''>全部实例</NativeSelectOption>
          {instances.map((instance) => (
            <NativeSelectOption key={instance.id} value={String(instance.id)}>
              {instance.name}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <NativeSelect
          value={status}
          aria-label='筛选预警状态'
          onChange={(event) => {
            setStatus(event.target.value)
            resetPage()
          }}
        >
          <NativeSelectOption value=''>全部状态</NativeSelectOption>
          <NativeSelectOption value='open'>预警中</NativeSelectOption>
          <NativeSelectOption value='resolved'>已恢复</NativeSelectOption>
        </NativeSelect>
        <NativeSelect
          value={alertType}
          aria-label='筛选预警类型'
          onChange={(event) => {
            setAlertType(event.target.value)
            resetPage()
          }}
        >
          <NativeSelectOption value=''>全部类型</NativeSelectOption>
          <NativeSelectOption value='availability'>
            实例不可用
          </NativeSelectOption>
          <NativeSelectOption value='credential'>凭据异常</NativeSelectOption>
        </NativeSelect>
        <NativeSelect
          value={deliveryStatus}
          aria-label='筛选邮件状态'
          onChange={(event) => {
            setDeliveryStatus(event.target.value)
            resetPage()
          }}
        >
          <NativeSelectOption value=''>全部邮件状态</NativeSelectOption>
          <NativeSelectOption value='pending'>等待发送</NativeSelectOption>
          <NativeSelectOption value='retrying'>正在重试</NativeSelectOption>
          <NativeSelectOption value='sent'>已发送</NativeSelectOption>
          <NativeSelectOption value='cancelled'>已取消</NativeSelectOption>
        </NativeSelect>
        <div className='relative sm:col-span-2'>
          <Search className='text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2' />
          <Input
            className='pl-9'
            value={search}
            onChange={(event) => {
              setSearch(event.target.value)
              resetPage()
            }}
            placeholder='搜索实例、平台、错误代码或收件人'
          />
        </div>
        <Input
          type='datetime-local'
          value={startTime}
          aria-label='开始时间'
          onChange={(event) => {
            setStartTime(event.target.value)
            resetPage()
          }}
        />
        <Input
          type='datetime-local'
          value={endTime}
          aria-label='结束时间'
          onChange={(event) => {
            setEndTime(event.target.value)
            resetPage()
          }}
        />
        <div className='text-muted-foreground text-sm tabular-nums sm:col-span-2 xl:col-span-4'>
          共 {data?.total ?? 0} 条故障生命周期
        </div>
      </div>

      <Accordion className='divide-border divide-y overflow-hidden rounded-lg border md:hidden'>
        {(data?.items ?? []).map((item) => (
          <AccordionItem
            key={item.id}
            value={String(item.id)}
            className='border-0'
          >
            <div className='flex min-w-0 items-stretch'>
              <AccordionTrigger className='min-h-24 min-w-0 flex-1 gap-3 rounded-none px-3 py-3 hover:no-underline'>
                <div className='min-w-0 flex-1 text-left'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='font-medium break-words'>
                      {item.instance_name || `#${item.instance_id}`}
                    </span>
                    <AlertStatusBadge status={item.status} />
                  </div>
                  <div className='text-muted-foreground mt-2 text-xs break-words'>
                    {alertTypeLabel(item.alert_type)} · {item.error_code}
                  </div>
                  <div className='text-muted-foreground mt-1 text-xs tabular-nums'>
                    最后发生 {formatTime(item.last_seen_at)}
                  </div>
                </div>
              </AccordionTrigger>
              <div className='flex shrink-0 items-center pe-2'>
                <Button
                  variant='ghost'
                  size='icon'
                  className='size-11'
                  aria-label='查看实例预警详情'
                  onClick={() => setSelected(item)}
                >
                  <Eye />
                </Button>
              </div>
            </div>
            <AccordionContent className='px-3 pb-4'>
              <div className='bg-muted/35 grid gap-3 rounded-md p-3 min-[420px]:grid-cols-2'>
                <Detail label='平台'>{item.instance_kind || '—'}</Detail>
                <Detail label='出现次数'>
                  {item.occurrences.toLocaleString()}
                </Detail>
                <Detail label='故障通知'>
                  <DeliverySummary item={item} phase='failure' />
                </Detail>
                <Detail label='恢复通知'>
                  <DeliverySummary item={item} phase='recovery' />
                </Detail>
              </div>
            </AccordionContent>
          </AccordionItem>
        ))}
        {!data?.items.length && (
          <div className='text-muted-foreground flex min-h-40 items-center justify-center px-4 text-sm'>
            {query.isLoading ? '数据加载中' : '暂无实例预警'}
          </div>
        )}
      </Accordion>

      <div className='hidden overflow-x-auto rounded-lg border md:block'>
        <Table>
          <TableHeader className='bg-muted/40'>
            <TableRow>
              <TableHead>状态</TableHead>
              <TableHead>实例</TableHead>
              <TableHead>类型 / 错误</TableHead>
              <TableHead>次数</TableHead>
              <TableHead>生命周期</TableHead>
              <TableHead>故障通知</TableHead>
              <TableHead>恢复通知</TableHead>
              <TableHead className='text-right'>操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(data?.items ?? []).map((item) => (
              <TableRow key={item.id}>
                <TableCell>
                  <AlertStatusBadge status={item.status} />
                </TableCell>
                <TableCell>
                  <div className='font-medium'>
                    {item.instance_name || `#${item.instance_id}`}
                  </div>
                  <div className='text-muted-foreground text-xs'>
                    {item.instance_kind}
                  </div>
                </TableCell>
                <TableCell>
                  <div>{alertTypeLabel(item.alert_type)}</div>
                  <code className='text-destructive text-xs break-all'>
                    {item.error_code}
                  </code>
                </TableCell>
                <TableCell className='tabular-nums'>
                  {item.occurrences.toLocaleString()}
                </TableCell>
                <TableCell className='text-xs whitespace-nowrap tabular-nums'>
                  <div>{formatTime(item.first_seen_at)}</div>
                  <div className='text-muted-foreground'>
                    至 {formatTime(item.resolved_at || item.last_seen_at)}
                  </div>
                </TableCell>
                <TableCell>
                  <DeliverySummary item={item} phase='failure' />
                </TableCell>
                <TableCell>
                  <DeliverySummary item={item} phase='recovery' />
                </TableCell>
                <TableCell className='text-right'>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    aria-label='查看实例预警详情'
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
                  {query.isLoading ? '数据加载中' : '暂无实例预警'}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <div className='flex flex-col gap-2 border-t px-4 py-3 text-sm min-[420px]:flex-row min-[420px]:items-center min-[420px]:justify-between'>
        <span className='text-muted-foreground'>
          第 {page} / {pages} 页
        </span>
        <div className='flex gap-2'>
          <Button
            size='sm'
            variant='outline'
            className='min-h-11 min-[420px]:min-h-0'
            disabled={page <= 1}
            onClick={() => setPage(page - 1)}
          >
            上一页
          </Button>
          <Button
            size='sm'
            variant='outline'
            className='min-h-11 min-[420px]:min-h-0'
            disabled={page >= pages}
            onClick={() => setPage(page + 1)}
          >
            下一页
          </Button>
        </div>
      </div>

      <InstanceAlertDialog item={selected} onClose={() => setSelected(null)} />
    </div>
  )
}

function InstanceAlertDialog({
  item,
  onClose,
}: {
  item: InstanceAlert | null
  onClose: () => void
}) {
  return (
    <Dialog open={Boolean(item)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>实例预警详情</DialogTitle>
          <DialogDescription>
            {item &&
              `${item.instance_name} · ${alertTypeLabel(item.alert_type)}`}
          </DialogDescription>
        </DialogHeader>
        {item && (
          <div className='grid gap-4 sm:grid-cols-2'>
            <Detail label='状态'>
              <AlertStatusBadge status={item.status} />
            </Detail>
            <Detail label='平台'>{item.instance_kind || '—'}</Detail>
            <Detail label='错误代码' className='sm:col-span-2'>
              <code className='text-destructive break-all'>
                {item.error_code}
              </code>
            </Detail>
            <Detail label='出现次数'>
              {item.occurrences.toLocaleString()}
            </Detail>
            <Detail label='首次发生'>{formatTime(item.first_seen_at)}</Detail>
            <Detail label='最后发生'>{formatTime(item.last_seen_at)}</Detail>
            <Detail label='恢复时间'>{formatTime(item.resolved_at)}</Detail>
            <DeliveryDetail item={item} phase='failure' />
            <DeliveryDetail item={item} phase='recovery' />
          </div>
        )}
        <DialogFooter>
          <Button variant='outline' onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function DeliveryDetail({
  item,
  phase,
}: {
  item: InstanceAlert
  phase: 'failure' | 'recovery'
}) {
  const recovery = phase === 'recovery'
  const status = recovery ? item.recovery_email_status : item.email_status
  const recipients = recovery
    ? item.recovery_email_recipients
    : item.email_recipients
  const attempts = recovery ? item.recovery_email_attempts : item.email_attempts
  const error = recovery ? item.recovery_email_error : item.email_error
  const sentAt = recovery ? item.recovery_email_sent_at : item.email_sent_at
  const nextRetryAt = recovery
    ? item.recovery_email_next_retry_at
    : item.email_next_retry_at
  return (
    <Detail
      label={recovery ? '恢复邮件' : '故障邮件'}
      className='sm:col-span-2'
    >
      <div className='space-y-1'>
        <DeliveryStatusBadge status={status} />
        <div>
          收件人：<span className='break-all'>{recipients || '—'}</span>
        </div>
        <div>尝试次数：{attempts}</div>
        {sentAt > 0 && <div>发送时间：{formatTime(sentAt)}</div>}
        {nextRetryAt > 0 && <div>下次重试：{formatTime(nextRetryAt)}</div>}
        {error && (
          <div className='text-destructive break-all'>失败原因：{error}</div>
        )}
      </div>
    </Detail>
  )
}

function DeliverySummary({
  item,
  phase,
}: {
  item: InstanceAlert
  phase: 'failure' | 'recovery'
}) {
  const recovery = phase === 'recovery'
  const status = recovery ? item.recovery_email_status : item.email_status
  const attempts = recovery ? item.recovery_email_attempts : item.email_attempts
  return (
    <div className='flex flex-wrap items-center gap-1.5'>
      <DeliveryStatusBadge status={status} />
      {attempts > 0 && (
        <span className='text-muted-foreground text-xs tabular-nums'>
          {attempts} 次
        </span>
      )}
    </div>
  )
}

function AlertStatusBadge({ status }: { status: InstanceAlert['status'] }) {
  return (
    <Badge variant={status === 'open' ? 'destructive' : 'outline'}>
      {status === 'open' ? '预警中' : '已恢复'}
    </Badge>
  )
}

function DeliveryStatusBadge({ status }: { status: string }) {
  const labels: Record<string, string> = {
    pending: '等待发送',
    retrying: '正在重试',
    sent: '已发送',
    cancelled: '已取消',
  }
  let variant: 'default' | 'destructive' | 'secondary' = 'secondary'
  if (status === 'sent') variant = 'default'
  if (status === 'retrying') variant = 'destructive'
  return <Badge variant={variant}>{labels[status] ?? '未发送'}</Badge>
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
      <div className='text-muted-foreground text-xs'>{label}</div>
      <div className='mt-1 text-sm break-words tabular-nums'>{children}</div>
    </div>
  )
}

function alertTypeLabel(value: InstanceAlert['alert_type']) {
  return value === 'credential' ? '凭据异常' : '实例不可用'
}

function formatTime(timestamp: number) {
  return timestamp > 0 ? dateTime.format(new Date(timestamp * 1000)) : '—'
}

function localDateTimeUnix(value: string) {
  const timestamp = new Date(value).getTime()
  return Number.isFinite(timestamp) ? Math.floor(timestamp / 1000) : 0
}
