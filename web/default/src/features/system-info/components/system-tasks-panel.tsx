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
import { ListChecks, RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import { listSystemTasks } from '@/features/system-settings/api'
import type {
  SystemTask,
  SystemTaskStatus,
} from '@/features/system-settings/types'
import { toIntlLocale } from '@/i18n/languages'
import { formatTimestampRelative, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

const TASK_LIMIT = 20
const ACTIVE_POLL_INTERVAL_MS = 8000

const STATUS_VARIANT: Record<SystemTaskStatus, 'secondary' | 'destructive'> = {
  pending: 'secondary',
  running: 'secondary',
  succeeded: 'secondary',
  failed: 'destructive',
}

const STATUS_CLASS_NAME: Record<SystemTaskStatus, string> = {
  pending:
    'bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
  running:
    'bg-sky-50 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300 [&_span]:bg-sky-500',
  succeeded:
    'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
  failed: '',
}

const STATUS_DOT_CLASS_NAME: Record<SystemTaskStatus, string> = {
  pending: 'bg-amber-500',
  running: 'bg-sky-500',
  succeeded: 'bg-emerald-500',
  failed: 'bg-destructive',
}

const PROGRESS_BAR_CLASS_NAME: Record<SystemTaskStatus, string> = {
  pending: '[&_[data-slot=progress-indicator]]:bg-amber-500',
  running: '[&_[data-slot=progress-indicator]]:bg-sky-500',
  succeeded: '[&_[data-slot=progress-indicator]]:bg-emerald-500',
  failed: '[&_[data-slot=progress-indicator]]:bg-destructive',
}

// Maps backend system task type constants to i18n source keys. Unknown/future
// types fall back to their raw identifier so the panel never shows blank.
const TYPE_LABEL: Record<string, string> = {
  managed_instance_probe: 'Instance health check',
  managed_instance_operation: 'Instance operation',
}

function isActiveStatus(status: SystemTaskStatus) {
  return status === 'pending' || status === 'running'
}

function getProgress(task: SystemTask): number | null {
  const progress = (task.state as { progress?: unknown } | undefined)?.progress
  if (typeof progress !== 'number' || Number.isNaN(progress)) return null
  return Math.min(100, Math.max(0, progress))
}

type SystemTasksTableProps = {
  tasks: SystemTask[]
}

function SystemTasksTable(props: SystemTasksTableProps) {
  const { t, i18n } = useTranslation()

  return (
    <>
      <Accordion className='divide-border divide-y overflow-hidden rounded-md border md:hidden'>
        {props.tasks.map((task) => {
          const progress = getProgress(task)
          return (
            <AccordionItem
              key={task.task_id}
              value={task.task_id}
              className='border-0'
            >
              <AccordionTrigger className='min-h-20 gap-3 rounded-none px-4 py-3 hover:no-underline'>
                <div className='min-w-0 flex-1'>
                  <div className='flex min-w-0 flex-wrap items-center gap-2'>
                    <span className='min-w-0 font-medium break-words'>
                      {t(TYPE_LABEL[task.type] ?? task.type)}
                    </span>
                    <Badge
                      variant={STATUS_VARIANT[task.status]}
                      className={cn('gap-1.5', STATUS_CLASS_NAME[task.status])}
                    >
                      <span
                        className={cn(
                          'size-1.5 rounded-full',
                          STATUS_DOT_CLASS_NAME[task.status]
                        )}
                        aria-hidden='true'
                      />
                      {t(task.status)}
                    </Badge>
                  </div>
                  <div className='mt-3 flex items-center gap-2'>
                    <Progress
                      value={progress ?? 0}
                      className={cn(
                        'h-1.5 flex-1',
                        PROGRESS_BAR_CLASS_NAME[task.status]
                      )}
                    />
                    <span className='text-muted-foreground w-10 text-right text-xs tabular-nums'>
                      {progress === null ? '-' : `${progress}%`}
                    </span>
                  </div>
                </div>
              </AccordionTrigger>
              <AccordionContent className='px-4 pb-4'>
                <div className='bg-muted/35 grid gap-3 rounded-md p-3 min-[420px]:grid-cols-2'>
                  <MobileTaskDetail label={t('Type')}>
                    <span className='font-mono text-xs break-all'>
                      {task.type}
                    </span>
                  </MobileTaskDetail>
                  <MobileTaskDetail label={t('Executor')}>
                    <span className='font-mono text-xs break-all'>
                      {task.locked_by || '-'}
                    </span>
                  </MobileTaskDetail>
                  <MobileTaskDetail label={t('Updated')}>
                    {formatTimestampToDate(task.updated_at)}
                  </MobileTaskDetail>
                  <MobileTaskDetail label={t('Detail')}>
                    <span
                      className={task.error ? 'text-destructive' : undefined}
                    >
                      {task.error || '-'}
                    </span>
                  </MobileTaskDetail>
                </div>
              </AccordionContent>
            </AccordionItem>
          )
        })}
      </Accordion>
      <div className='hidden overflow-x-auto rounded-md border md:block'>
        <Table className='min-w-[900px]'>
          <TableHeader>
            <TableRow className='bg-muted/40 hover:bg-muted/40'>
              <TableHead className='h-9 w-[260px] px-4 text-xs'>
                {t('Type')}
              </TableHead>
              <TableHead className='h-9 w-[130px] text-xs'>
                {t('Status')}
              </TableHead>
              <TableHead className='h-9 w-[180px] text-xs'>
                {t('Progress')}
              </TableHead>
              <TableHead className='h-9 min-w-[260px] text-xs'>
                {t('Executor')}
              </TableHead>
              <TableHead className='h-9 w-[190px] text-xs'>
                {t('Updated')}
              </TableHead>
              <TableHead className='h-9 w-[220px] pr-4 text-xs'>
                {t('Detail')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.tasks.map((task) => {
              const progress = getProgress(task)
              return (
                <TableRow key={task.task_id} className='hover:bg-muted/30'>
                  <TableCell className='px-4 py-3 align-middle'>
                    <div className='space-y-0.5'>
                      <div className='font-medium'>
                        {t(TYPE_LABEL[task.type] ?? task.type)}
                      </div>
                      <div className='text-muted-foreground font-mono text-[11px]'>
                        {task.type}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className='py-3 align-middle'>
                    <Badge
                      variant={STATUS_VARIANT[task.status]}
                      className={cn('gap-1.5', STATUS_CLASS_NAME[task.status])}
                    >
                      <span
                        className={cn(
                          'size-1.5 rounded-full',
                          STATUS_DOT_CLASS_NAME[task.status]
                        )}
                        aria-hidden='true'
                      />
                      {t(task.status)}
                    </Badge>
                  </TableCell>
                  <TableCell className='py-3 align-middle'>
                    <div className='flex items-center gap-2'>
                      <Progress
                        value={progress ?? 0}
                        aria-label={`${t('Progress')}: ${t(TYPE_LABEL[task.type] ?? task.type)}`}
                        className={cn(
                          'w-24',
                          PROGRESS_BAR_CLASS_NAME[task.status]
                        )}
                      />
                      <span className='text-muted-foreground w-10 text-right text-xs tabular-nums'>
                        {progress === null ? '-' : `${progress}%`}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell className='text-muted-foreground max-w-[280px] truncate py-3 align-middle font-mono text-xs'>
                    {task.locked_by || '-'}
                  </TableCell>
                  <TableCell
                    className='text-muted-foreground py-3 align-middle text-xs whitespace-nowrap'
                    title={formatTimestampToDate(task.updated_at)}
                  >
                    {formatTimestampRelative(
                      task.updated_at,
                      'seconds',
                      toIntlLocale(i18n.language)
                    )}
                  </TableCell>
                  <TableCell
                    className='text-destructive max-w-[220px] truncate py-3 pr-4 align-middle text-xs'
                    title={task.error || undefined}
                  >
                    {task.error || '-'}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>
    </>
  )
}

function MobileTaskDetail(props: { label: string; children: ReactNode }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 text-sm break-words tabular-nums'>
        {props.children}
      </div>
    </div>
  )
}

export function SystemTasksPanel() {
  const { t } = useTranslation()
  const tasksQuery = useQuery({
    queryKey: ['system-info', 'system-tasks'],
    queryFn: async () => {
      const res = await listSystemTasks(TASK_LIMIT)
      if (!res.success || !Array.isArray(res.data)) {
        throw new Error(res.message || t('We could not load system tasks.'))
      }
      return res.data
    },
    staleTime: 30 * 1000,
    retry: false,
    refetchInterval: (query) =>
      query.state.data?.some((task) => isActiveStatus(task.status))
        ? ACTIVE_POLL_INTERVAL_MS
        : false,
  })

  const tasks = tasksQuery.data ?? []
  const loading = tasksQuery.isLoading
  const refreshing = tasksQuery.isFetching && !tasksQuery.isLoading
  const hasActiveTasks = tasks.some((task) => isActiveStatus(task.status))
  const activeTasks = tasks.filter((task) => isActiveStatus(task.status))
  const historyTasks = tasks.filter((task) => !isActiveStatus(task.status))

  return (
    <section className='bg-card overflow-hidden rounded-lg border shadow-xs'>
      <div className='flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5'>
        <div className='min-w-0'>
          <div className='flex items-center gap-2'>
            <span className='bg-muted text-muted-foreground inline-flex size-7 items-center justify-center rounded-md'>
              <ListChecks className='size-4' aria-hidden='true' />
            </span>
            <div className='min-w-0'>
              <h3 className='text-sm font-semibold'>{t('System Tasks')}</h3>
              <p className='text-muted-foreground mt-0.5 text-xs'>
                {t(
                  'Recent maintenance tasks running across instances and their execution status.'
                )}
              </p>
            </div>
          </div>
        </div>
        <div className='flex shrink-0 items-center gap-3'>
          <span
            className='text-muted-foreground inline-flex items-center gap-1.5 text-xs'
            aria-live='polite'
          >
            <span
              className={cn(
                'size-1.5 rounded-full',
                hasActiveTasks ? 'bg-emerald-500' : 'bg-muted-foreground/40'
              )}
              aria-hidden='true'
            />
            {hasActiveTasks
              ? t('Auto-refreshing every {{seconds}}s', {
                  seconds: ACTIVE_POLL_INTERVAL_MS / 1000,
                })
              : t('Live refresh pauses when no task is running')}
          </span>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => void tasksQuery.refetch()}
            disabled={tasksQuery.isFetching}
            aria-label={t('Refresh')}
          >
            <RefreshCw
              data-icon='inline-start'
              className={cn('size-3.5', refreshing && 'animate-spin')}
              aria-hidden='true'
            />
            {refreshing ? t('Refreshing...') : t('Refresh')}
          </Button>
        </div>
      </div>

      <div aria-busy={tasksQuery.isFetching}>
        {(() => {
          if (loading) {
            return (
              <div className='space-y-2 p-4 sm:p-5'>
                {['task-a', 'task-b', 'task-c', 'task-d'].map((key) => (
                  <Skeleton key={key} className='h-9 w-full rounded-md' />
                ))}
              </div>
            )
          }
          if (tasksQuery.isError) {
            return (
              <ErrorState
                title={t('We could not load system tasks.')}
                description={
                  tasksQuery.error instanceof Error
                    ? tasksQuery.error.message
                    : undefined
                }
                onRetry={() => {
                  void tasksQuery.refetch()
                }}
                className='min-h-[260px]'
              />
            )
          }
          if (tasks.length === 0) {
            return (
              <div className='px-4 py-10 text-center sm:px-5'>
                <div className='bg-muted mx-auto mb-3 flex size-10 items-center justify-center rounded-lg'>
                  <ListChecks
                    className='text-muted-foreground size-5'
                    aria-hidden='true'
                  />
                </div>
                <p className='text-muted-foreground text-sm'>
                  {t('No system tasks yet.')}
                </p>
              </div>
            )
          }
          return (
            <div className='space-y-4 p-4 sm:p-5'>
              <div>
                <div className='mb-2 flex items-center justify-between gap-3'>
                  <div>
                    <h4 className='text-sm font-medium'>{t('Active Tasks')}</h4>
                    <p className='text-muted-foreground mt-0.5 text-xs'>
                      {t('Tasks currently pending or running.')}
                    </p>
                  </div>
                  <Badge variant='outline'>{activeTasks.length}</Badge>
                </div>
                {activeTasks.length > 0 ? (
                  <SystemTasksTable tasks={activeTasks} />
                ) : (
                  <div className='text-muted-foreground rounded-md border border-dashed px-4 py-6 text-center text-sm'>
                    {t('No active system tasks.')}
                  </div>
                )}
              </div>

              <div>
                <div className='mb-2 flex items-center justify-between gap-3'>
                  <div>
                    <h4 className='text-sm font-medium'>{t('Task History')}</h4>
                    <p className='text-muted-foreground mt-0.5 text-xs'>
                      {t('Recently completed or failed system task runs.')}
                    </p>
                  </div>
                  <Badge variant='outline'>{historyTasks.length}</Badge>
                </div>
                {historyTasks.length > 0 ? (
                  <SystemTasksTable tasks={historyTasks} />
                ) : (
                  <div className='text-muted-foreground rounded-md border border-dashed px-4 py-6 text-center text-sm'>
                    {t('No historical system tasks.')}
                  </div>
                )}
              </div>
            </div>
          )
        })()}
      </div>
    </section>
  )
}
