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
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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

import {
  ExportStatusBadge,
  FilterDetailsDialog,
  FilterSummaryButton,
} from './filter-details-dialog'
import { EXPORT_STATUS_META } from './status-meta'

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
      <div className='w-44 space-y-1'>
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

export function ExportRecords() {
  const user = useAuthStore((state) => state.auth.user)
  const isRoot = user?.role === ROLE.SUPER_ADMIN
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState('')
  const [instanceId, setInstanceId] = useState('')
  const [actorId, setActorId] = useState('')
  const [busyTask, setBusyTask] = useState('')
  const [deleteTarget, setDeleteTarget] =
    useState<UsageRecordExportTask | null>(null)
  const [detailTarget, setDetailTarget] =
    useState<UsageRecordExportTask | null>(null)

  const queryFilters = useMemo(
    () => ({
      page,
      page_size: PAGE_SIZE,
      status: status || undefined,
      instance_id: Number(instanceId) || undefined,
      actor_id: isRoot ? Number(actorId) || undefined : undefined,
    }),
    [actorId, instanceId, isRoot, page, status]
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
          <div className='border-border bg-card flex flex-wrap items-end gap-3 rounded-lg border p-3 shadow-xs'>
            <label className='grid min-w-36 gap-1 text-xs font-medium'>
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
            <label className='grid min-w-48 gap-1 text-xs font-medium'>
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
              <label className='grid min-w-40 gap-1 text-xs font-medium'>
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
            <div className='overflow-x-auto'>
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
                            <Badge
                              variant='outline'
                              className={cn(
                                'h-4 rounded px-1.5 text-[10px] font-medium',
                                item.instance_kind === 'sub2api'
                                  ? 'border-cyan-200 bg-cyan-50 text-cyan-700 dark:border-cyan-900/70 dark:bg-cyan-950/40 dark:text-cyan-300'
                                  : 'border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-900/70 dark:bg-violet-950/40 dark:text-violet-300'
                              )}
                            >
                              {item.instance_kind === 'sub2api'
                                ? 'Sub2API'
                                : 'New API'}
                            </Badge>
                            <span className='text-muted-foreground text-xs tabular-nums'>
                              #{item.instance_id}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell className='w-72 max-w-72'>
                          <FilterSummaryButton
                            item={item}
                            onClick={() => setDetailTarget(item)}
                          />
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
                                label='下载 CSV'
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
            <div className='border-border flex items-center justify-between border-t px-3 py-2'>
              <span className='text-muted-foreground text-xs tabular-nums'>
                共 {(list?.total ?? 0).toLocaleString()} 个任务
              </span>
              <div className='flex items-center gap-2'>
                <Button
                  variant='outline'
                  size='sm'
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
      <FilterDetailsDialog
        item={detailTarget}
        onOpenChange={(open) => {
          if (!open) setDetailTarget(null)
        }}
      />
      <ConfirmDialog
        open={deleteTarget != null}
        onOpenChange={(open) => {
          if (!open && busyTask !== deleteTarget?.task_id) {
            setDeleteTarget(null)
          }
        }}
        title='删除导出记录'
        desc='删除后将同时清理已生成的 CSV 文件，且无法恢复。'
        confirmText='删除记录'
        destructive
        isLoading={deleteTarget != null && busyTask === deleteTarget.task_id}
        handleConfirm={() => void remove()}
      />
    </SectionPageLayout>
  )
}
