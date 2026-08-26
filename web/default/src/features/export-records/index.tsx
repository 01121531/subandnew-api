/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useQuery } from '@tanstack/react-query'
import {
  Ban,
  CircleAlert,
  Download,
  RefreshCw,
  RotateCcw,
  Search,
  Trash2,
  type LucideIcon,
} from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import { SectionPageLayout } from '@/components/layout'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DropdownMenuItem } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
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
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getManagedInstances } from '@/features/managed-instances/api'
import {
  cancelUsageRecordsExport,
  deleteUsageRecordsExport,
  downloadUsageRecordsExport,
  listUsageRecordsExports,
  retryUsageRecordsExport,
  type UsageRecordExportTask,
} from '@/features/usage-records/api'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { ExportStatusBadge, FilterDetailsDialog } from './filter-details-dialog'
import {
  EXPORT_STATUS_META,
  exportInstanceKindClassName,
  exportInstanceKindLabel,
} from './status-meta'

const PAGE_SIZE = 20
const SKELETON_ROWS = [
  'loading-1',
  'loading-2',
  'loading-3',
  'loading-4',
  'loading-5',
]
const STATUS_OPTIONS = [
  ['', '全部状态'],
  ['pending', '等待中'],
  ['running', '导出中'],
  ['succeeded', '已完成'],
  ['failed', '失败'],
  ['cancelled', '已取消'],
  ['expired', '已过期'],
] as const
const EXPORT_KIND_OPTIONS = [
  ['', '全部类型'],
  ['usage_records', '使用记录'],
  ['accounts', '账号导出'],
] as const

const dateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'short',
})

function formatTime(value: number) {
  return value > 0 ? dateTime.format(new Date(value * 1000)) : '—'
}

function formatBytes(value: number) {
  if (value <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit += 1
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

function ExportProgress({ item }: { item: UsageRecordExportTask }) {
  if (item.status === 'running') {
    return (
      <div className='w-full max-w-44 space-y-1'>
        <div className='flex justify-between text-xs tabular-nums'>
          <span>{item.progress}%</span>
          <span className='text-muted-foreground'>
            {item.processed.toLocaleString()} / {item.total.toLocaleString()}
          </span>
        </div>
        <Progress value={item.progress} className='h-1.5' />
      </div>
    )
  }
  if (item.status === 'succeeded' || item.status === 'expired') {
    return (
      <div className='text-xs tabular-nums'>
        {item.record_count.toLocaleString()} 条 · {formatBytes(item.file_size)}
        {item.warning_count > 0 && (
          <span className='text-warning'> · {item.warning_count} 条警告</span>
        )}
      </div>
    )
  }
  if (item.error_code) {
    return <span className='text-destructive text-xs'>{item.error_code}</span>
  }
  return <span className='text-muted-foreground text-xs'>等待处理</span>
}

function ExportActionButton({
  label,
  icon: Icon,
  disabled,
  destructive = false,
  onClick,
}: {
  label: string
  icon: LucideIcon
  disabled: boolean
  destructive?: boolean
  onClick: () => void
}) {
  const button = (
    <Button
      variant='ghost'
      size='icon-sm'
      aria-label={label}
      disabled={disabled}
      className={cn(
        destructive &&
          'text-muted-foreground hover:bg-destructive/10 hover:text-destructive'
      )}
      onClick={onClick}
    >
      <Icon />
    </Button>
  )
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger render={button} />
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

function ExportKindBadge({ item }: { item: UsageRecordExportTask }) {
  const accountExport = item.export_kind === 'accounts'
  return (
    <Badge
      variant='outline'
      className={cn(
        'h-5 rounded px-1.5 text-[10px] font-medium',
        accountExport
          ? 'border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-900/70 dark:bg-sky-950/40 dark:text-sky-300'
          : 'border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-900/70 dark:bg-violet-950/40 dark:text-violet-300'
      )}
    >
      {accountExport ? '账号导出 · XLSX' : '使用记录 · CSV'}
    </Badge>
  )
}

function ExportWarningBadge({ item }: { item: UsageRecordExportTask }) {
  if (item.status !== 'succeeded' || item.warning_count <= 0) return null
  return (
    <Badge
      className='border-warning/30 bg-warning/10 text-warning'
      variant='outline'
    >
      完成但有警告
    </Badge>
  )
}

export function ExportRecords() {
  const user = useAuthStore((state) => state.auth.user)
  const isRoot = user?.role === ROLE.SUPER_ADMIN
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState('')
  const [exportKind, setExportKind] = useState('')
  const [instanceId, setInstanceId] = useState('')
  const [actorId, setActorId] = useState('')
  const [busyTask, setBusyTask] = useState('')
  const [deleteTarget, setDeleteTarget] =
    useState<UsageRecordExportTask | null>(null)

  const queryFilters = useMemo(
    () => ({
      page,
      page_size: PAGE_SIZE,
      status: status || undefined,
      export_kind: exportKind || undefined,
      instance_id: Number(instanceId) || undefined,
      actor_id: isRoot ? Number(actorId) || undefined : undefined,
    }),
    [actorId, exportKind, instanceId, isRoot, page, status]
  )
  const exportsQuery = useQuery({
    queryKey: ['managed-usage-exports', queryFilters],
    queryFn: () => listUsageRecordsExports(queryFilters),
    refetchInterval: (query) =>
      query.state.data?.data.has_active ? 3_000 : 30_000,
  })
  const instancesQuery = useQuery({
    queryKey: ['usage-export-instances'],
    queryFn: () => getManagedInstances({ search: '', kind: '', status: '' }),
    staleTime: 60_000,
  })
  const list = exportsQuery.data?.data
  const items = list?.items ?? []
  const totalPages = Math.max(1, Math.ceil((list?.total ?? 0) / PAGE_SIZE))

  const runAction = async (taskId: string, action: 'cancel' | 'retry') => {
    setBusyTask(taskId)
    try {
      if (action === 'cancel') {
        await cancelUsageRecordsExport(taskId)
        toast.success('已取消排队任务')
      } else {
        await retryUsageRecordsExport(taskId)
        toast.success('已重新加入导出队列')
      }
      await exportsQuery.refetch()
    } catch {
      toast.error(action === 'cancel' ? '取消失败' : '重新导出失败')
    } finally {
      setBusyTask('')
    }
  }

  const download = async (taskId: string) => {
    setBusyTask(taskId)
    try {
      await downloadUsageRecordsExport(taskId)
    } catch {
      toast.error('下载失败，文件可能已经过期')
    } finally {
      setBusyTask('')
    }
  }

  const remove = async () => {
    if (!deleteTarget) return
    setBusyTask(deleteTarget.task_id)
    try {
      await deleteUsageRecordsExport(deleteTarget.task_id)
      toast.success('导出记录已删除')
      setDeleteTarget(null)
      if (items.length === 1 && page > 1) {
        setPage(page - 1)
      } else {
        await exportsQuery.refetch()
      }
    } catch {
      toast.error('删除导出记录失败')
    } finally {
      setBusyTask('')
    }
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>导出记录</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='icon-sm'
          aria-label='刷新导出记录'
          onClick={() => void exportsQuery.refetch()}
        >
          <RefreshCw
            className={exportsQuery.isFetching ? 'animate-spin' : ''}
          />
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='grid gap-3'>
          <div className='border-border bg-card grid min-w-0 gap-3 rounded-lg border p-3 shadow-xs sm:flex sm:flex-wrap sm:items-end'>
            <label className='grid min-w-0 gap-1 text-xs font-medium sm:min-w-36'>
              状态
              <NativeSelect
                value={status}
                onChange={(event) => {
                  setStatus(event.target.value)
                  setPage(1)
                }}
              >
                {STATUS_OPTIONS.map(([value, label]) => (
                  <NativeSelectOption key={value} value={value}>
                    {label}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </label>
            <label className='grid min-w-0 gap-1 text-xs font-medium sm:min-w-36'>
              类型
              <NativeSelect
                value={exportKind}
                onChange={(event) => {
                  setExportKind(event.target.value)
                  setPage(1)
                }}
              >
                {EXPORT_KIND_OPTIONS.map(([value, label]) => (
                  <NativeSelectOption key={value} value={value}>
                    {label}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </label>
            <label className='grid min-w-0 gap-1 text-xs font-medium sm:min-w-48'>
              实例
              <NativeSelect
                value={instanceId}
                onChange={(event) => {
                  setInstanceId(event.target.value)
                  setPage(1)
                }}
              >
                <NativeSelectOption value=''>全部实例</NativeSelectOption>
                {(instancesQuery.data?.data.items ?? []).map((instance) => (
                  <NativeSelectOption
                    key={instance.id}
                    value={String(instance.id)}
                  >
                    {instance.name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </label>
            {isRoot && (
              <label className='grid min-w-0 gap-1 text-xs font-medium sm:min-w-40'>
                创建人 ID
                <div className='relative'>
                  <Search className='text-muted-foreground absolute top-1/2 left-2 size-4 -translate-y-1/2' />
                  <Input
                    className='pl-8'
                    inputMode='numeric'
                    value={actorId}
                    onChange={(event) => {
                      setActorId(event.target.value.replaceAll(/\D/g, ''))
                      setPage(1)
                    }}
                    placeholder='全部创建人'
                  />
                </div>
              </label>
            )}
          </div>

          {exportsQuery.isError && (
            <Alert variant='destructive'>
              <CircleAlert />
              <AlertTitle>导出记录加载失败</AlertTitle>
              <AlertDescription>
                请检查服务状态后重试。已有导出任务不会因此丢失。
              </AlertDescription>
            </Alert>
          )}

          <div className='border-border bg-card overflow-hidden rounded-lg border shadow-xs'>
            <div className='md:hidden'>
              {exportsQuery.isLoading && (
                <div className='grid gap-2 p-3'>
                  {SKELETON_ROWS.map((key) => (
                    <Skeleton key={key} className='h-20 w-full rounded-md' />
                  ))}
                </div>
              )}
              {!exportsQuery.isLoading &&
                !exportsQuery.isError &&
                items.length === 0 && (
                  <div className='text-muted-foreground flex min-h-40 items-center justify-center px-4 text-sm'>
                    暂无导出记录
                  </div>
                )}
              <Accordion className='divide-border divide-y'>
                {items.map((item) => {
                  const meta = EXPORT_STATUS_META[item.status]
                  const canDelete =
                    item.status !== 'pending' && item.status !== 'running'
                  return (
                    <AccordionItem
                      key={item.task_id}
                      value={item.task_id}
                      className={cn('border-l-2', meta.accentClassName)}
                    >
                      <div className='flex min-w-0 items-stretch'>
                        <AccordionTrigger className='min-h-24 min-w-0 flex-1 gap-3 rounded-none px-3 py-3 hover:no-underline'>
                          <div className='min-w-0 flex-1'>
                            <div className='flex min-w-0 flex-wrap items-center gap-2'>
                              <span className='min-w-0 font-medium break-words'>
                                {item.instance_name}
                              </span>
                              <ExportStatusBadge status={item.status} />
                              <ExportWarningBadge item={item} />
                              <ExportKindBadge item={item} />
                            </div>
                            <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs'>
                              <span>{formatTime(item.created_at)}</span>
                              {item.queue_position > 0 && (
                                <span className='tabular-nums'>
                                  队列第 {item.queue_position} 位
                                </span>
                              )}
                            </div>
                            <div className='mt-2'>
                              <ExportProgress item={item} />
                            </div>
                          </div>
                        </AccordionTrigger>
                        <div className='flex shrink-0 items-center gap-1 pe-2'>
                          {item.status === 'pending' && (
                            <Button
                              variant='ghost'
                              size='icon'
                              className='size-11'
                              aria-label='取消导出'
                              disabled={busyTask === item.task_id}
                              onClick={() =>
                                void runAction(item.task_id, 'cancel')
                              }
                            >
                              <Ban />
                            </Button>
                          )}
                          {item.status === 'succeeded' && (
                            <Button
                              variant='ghost'
                              size='icon'
                              className='size-11'
                              aria-label={`下载 ${item.file_format.toUpperCase()}`}
                              disabled={busyTask === item.task_id}
                              onClick={() => void download(item.task_id)}
                            >
                              <Download />
                            </Button>
                          )}
                          {(item.status === 'failed' ||
                            item.status === 'expired') && (
                            <Button
                              variant='ghost'
                              size='icon'
                              className='size-11'
                              aria-label='重新导出'
                              disabled={busyTask === item.task_id}
                              onClick={() =>
                                void runAction(item.task_id, 'retry')
                              }
                            >
                              <RotateCcw />
                            </Button>
                          )}
                          {canDelete && (
                            <DataTableRowActionMenu
                              ariaLabel='更多导出操作'
                              triggerClassName='size-11'
                            >
                              <DropdownMenuItem
                                variant='destructive'
                                disabled={busyTask === item.task_id}
                                onClick={() => setDeleteTarget(item)}
                              >
                                <Trash2 />
                                删除导出记录
                              </DropdownMenuItem>
                            </DataTableRowActionMenu>
                          )}
                        </div>
                      </div>
                      <AccordionContent className='px-3 pb-4'>
                        <div className='bg-muted/35 grid grid-cols-2 gap-x-4 gap-y-3 rounded-md p-3'>
                          <MobileExportDetail label='系统类型'>
                            <Badge
                              variant='outline'
                              className={cn(
                                'h-5 rounded px-1.5 text-[10px] font-medium',
                                exportInstanceKindClassName(item.instance_kind)
                              )}
                            >
                              {exportInstanceKindLabel(item.instance_kind)}
                            </Badge>
                          </MobileExportDetail>
                          <MobileExportDetail label='创建人'>
                            {item.actor_name} · #{item.actor_id}
                          </MobileExportDetail>
                          <MobileExportDetail label='筛选条件'>
                            <FilterDetailsDialog item={item} />
                          </MobileExportDetail>
                          <MobileExportDetail label='完成时间'>
                            {formatTime(item.finished_at)}
                          </MobileExportDetail>
                          {item.expires_at > 0 && (
                            <MobileExportDetail label='过期时间'>
                              {formatTime(item.expires_at)}
                            </MobileExportDetail>
                          )}
                          {item.error_code && (
                            <MobileExportDetail label='错误'>
                              <span className='text-destructive break-words'>
                                {item.error_code}
                              </span>
                            </MobileExportDetail>
                          )}
                        </div>
                      </AccordionContent>
                    </AccordionItem>
                  )
                })}
              </Accordion>
            </div>
            <div className='hidden overflow-x-auto md:block'>
              <Table className='min-w-[1180px]'>
                <TableHeader className='bg-muted/35'>
                  <TableRow>
                    <TableHead>状态</TableHead>
                    <TableHead>实例</TableHead>
                    <TableHead>筛选条件</TableHead>
                    <TableHead>创建人</TableHead>
                    <TableHead>进度 / 结果</TableHead>
                    <TableHead>创建时间</TableHead>
                    <TableHead>完成 / 过期</TableHead>
                    <TableHead className='text-right'>操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {exportsQuery.isLoading &&
                    SKELETON_ROWS.map((key) => (
                      <TableRow key={key}>
                        <TableCell colSpan={8}>
                          <Skeleton className='h-8 w-full' />
                        </TableCell>
                      </TableRow>
                    ))}
                  {!exportsQuery.isLoading &&
                    !exportsQuery.isError &&
                    items.length === 0 && (
                      <TableRow>
                        <TableCell colSpan={8} className='h-40 text-center'>
                          <span className='text-muted-foreground text-sm'>
                            暂无导出记录
                          </span>
                        </TableCell>
                      </TableRow>
                    )}
                  {items.map((item) => {
                    const meta = EXPORT_STATUS_META[item.status]
                    return (
                      <TableRow
                        key={item.task_id}
                        className='hover:bg-muted/30'
                      >
                        <TableCell
                          className={cn(
                            'border-l-2 pl-3',
                            meta.accentClassName
                          )}
                        >
                          <div className='grid gap-1'>
                            <ExportStatusBadge status={item.status} />
                            <ExportWarningBadge item={item} />
                            {item.queue_position > 0 && (
                              <span className='text-muted-foreground text-xs tabular-nums'>
                                队列第 {item.queue_position} 位
                              </span>
                            )}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className='font-medium'>
                            {item.instance_name}
                          </div>
                          <div className='mt-1 flex items-center gap-1.5'>
                            <ExportKindBadge item={item} />
                            <Badge
                              variant='outline'
                              className={cn(
                                'h-4 rounded px-1.5 text-[10px] font-medium',
                                exportInstanceKindClassName(item.instance_kind)
                              )}
                            >
                              {exportInstanceKindLabel(item.instance_kind)}
                            </Badge>
                            <span className='text-muted-foreground text-xs tabular-nums'>
                              {item.instance_id > 0
                                ? `#${item.instance_id}`
                                : '跨实例'}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell className='w-72 max-w-72'>
                          <FilterDetailsDialog item={item} />
                        </TableCell>
                        <TableCell>
                          <div>{item.actor_name}</div>
                          <div className='text-muted-foreground text-xs'>
                            #{item.actor_id}
                          </div>
                        </TableCell>
                        <TableCell>
                          <ExportProgress item={item} />
                        </TableCell>
                        <TableCell className='text-xs tabular-nums'>
                          {formatTime(item.created_at)}
                        </TableCell>
                        <TableCell className='text-xs tabular-nums'>
                          <div>{formatTime(item.finished_at)}</div>
                          {item.expires_at > 0 && (
                            <div className='text-muted-foreground'>
                              过期：{formatTime(item.expires_at)}
                            </div>
                          )}
                        </TableCell>
                        <TableCell className='text-right'>
                          <div className='flex justify-end gap-1'>
                            {item.status === 'pending' && (
                              <ExportActionButton
                                label='取消导出'
                                icon={Ban}
                                disabled={busyTask === item.task_id}
                                onClick={() =>
                                  void runAction(item.task_id, 'cancel')
                                }
                              />
                            )}
                            {item.status === 'succeeded' && (
                              <ExportActionButton
                                label={`下载 ${item.file_format.toUpperCase()}`}
                                icon={Download}
                                disabled={busyTask === item.task_id}
                                onClick={() => void download(item.task_id)}
                              />
                            )}
                            {(item.status === 'failed' ||
                              item.status === 'expired') && (
                              <ExportActionButton
                                label='重新导出'
                                icon={RotateCcw}
                                disabled={busyTask === item.task_id}
                                onClick={() =>
                                  void runAction(item.task_id, 'retry')
                                }
                              />
                            )}
                            {item.status !== 'pending' &&
                              item.status !== 'running' && (
                                <ExportActionButton
                                  label='删除导出记录'
                                  icon={Trash2}
                                  disabled={busyTask === item.task_id}
                                  destructive
                                  onClick={() => setDeleteTarget(item)}
                                />
                              )}
                          </div>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
            <div className='border-border flex flex-col gap-2 border-t px-3 py-2 min-[420px]:flex-row min-[420px]:items-center min-[420px]:justify-between'>
              <span className='text-muted-foreground text-xs tabular-nums'>
                共 {(list?.total ?? 0).toLocaleString()} 个任务
              </span>
              <div className='grid grid-cols-[1fr_auto_1fr] items-center gap-2'>
                <Button
                  variant='outline'
                  size='sm'
                  className='min-h-11 min-[420px]:min-h-0'
                  disabled={page <= 1}
                  onClick={() => setPage(page - 1)}
                >
                  上一页
                </Button>
                <Badge variant='outline'>
                  第 {page} / {totalPages} 页
                </Badge>
                <Button
                  variant='outline'
                  size='sm'
                  className='min-h-11 min-[420px]:min-h-0'
                  disabled={page >= totalPages}
                  onClick={() => setPage(page + 1)}
                >
                  下一页
                </Button>
              </div>
            </div>
          </div>
        </div>
      </SectionPageLayout.Content>
      <ConfirmDialog
        open={deleteTarget != null}
        onOpenChange={(open) => {
          if (!open && busyTask !== deleteTarget?.task_id) {
            setDeleteTarget(null)
          }
        }}
        title='删除导出记录'
        desc='删除后将同时清理已生成的导出文件，且无法恢复。'
        confirmText='删除记录'
        destructive
        isLoading={deleteTarget != null && busyTask === deleteTarget.task_id}
        handleConfirm={() => void remove()}
      />
    </SectionPageLayout>
  )
}

function MobileExportDetail(props: { label: string; children: ReactNode }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 text-sm break-words tabular-nums'>
        {props.children}
      </div>
    </div>
  )
}
