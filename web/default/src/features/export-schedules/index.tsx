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
  Clock3,
  Download,
  History,
  Pause,
  Pencil,
  Play,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { downloadUsageRecordsExport } from '@/features/usage-records/api'

import {
  listSchedules,
  newSchedule,
  scheduleAction,
  scheduleRuns,
  retryDelivery,
  type ExportSchedule,
  type ScheduleConfig,
} from './api'
import { ScheduleForm } from './form'
import { scheduleMessage, statusText, useScheduleText } from './messages'

export function ScheduleEntry({
  query,
  accounts,
}: {
  query: ScheduleConfig['query']
  accounts: ScheduleConfig['accounts']
}) {
  const [open, setOpen] = useState(false)
  const text = useScheduleText()
  const { i18n } = useTranslation()
  return (
    <>
      <Button
        size='sm'
        variant='outline'
        disabled={!query.instance_ids.length}
        onClick={() => setOpen(true)}
      >
        <Clock3 />
        {text('定时导出', 'Schedule export')}
      </Button>
      {open && (
        <ScheduleForm
          initial={newSchedule(query, accounts, i18n.language)}
          onClose={() => setOpen(false)}
          onSaved={() => setOpen(false)}
        />
      )}
    </>
  )
}
function dateTime(value: number) {
  return value
    ? new Intl.DateTimeFormat('zh-CN', {
        timeZone: 'Asia/Shanghai',
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hourCycle: 'h23',
      }).format(new Date(value * 1000))
    : '—'
}

export function ExportSchedules() {
  const text = useScheduleText()
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<ExportSchedule | null>(null)
  const [history, setHistory] = useState<ExportSchedule | null>(null)
  const [busy, setBusy] = useState(0)
  const query = useQuery({
    queryKey: ['account-export-schedules', page],
    queryFn: () => listSchedules(page),
    refetchInterval: 15000,
    retry: false,
  })
  const action = async (
    plan: ExportSchedule,
    kind: 'pause' | 'resume' | 'delete' | 'execute'
  ) => {
    if (
      kind === 'delete' &&
      !window.confirm(
        text(
          '删除计划？已有文件保留，已发送邮件无法撤回。',
          'Delete schedule? Existing files remain; sent emails cannot be recalled.'
        )
      )
    ) {
      return
    }
    if (
      kind === 'execute' &&
      !window.confirm(
        text(
          '立即生成报表并发送给计划中的收件人？',
          'Generate and email this report to its recipients now?'
        )
      )
    ) {
      return
    }
    setBusy(plan.id)
    try {
      await scheduleAction(plan, kind)
      toast.success(text('操作成功', 'Updated'))
      await query.refetch()
    } catch (e) {
      toast.error(scheduleMessage(e, text))
      void query.refetch()
    } finally {
      setBusy(0)
    }
  }
  return (
    <section className='grid min-w-0 gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div>
          <h2 className='text-base font-semibold'>
            {text('定时任务', 'Scheduled tasks')}
          </h2>
          <p className='text-muted-foreground text-xs'>
            {text(
              '北京时间 · 文件保留 30 天',
              'Asia/Shanghai · Files retained for 30 days'
            )}
          </p>
        </div>
        <Button
          variant='outline'
          size='icon-sm'
          title={text('刷新', 'Refresh')}
          aria-label={text('刷新', 'Refresh')}
          onClick={() => void query.refetch()}
        >
          <RefreshCw className={query.isFetching ? 'animate-spin' : ''} />
        </Button>
      </div>
      {query.isError && (
        <p role='alert' className='text-destructive text-sm'>
          {text(
            '读取失败，请重试。',
            'Could not load schedules. Please retry.'
          )}
        </p>
      )}
      {query.data && !query.data.smtp_ready && (
        <p className='rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200'>
          {text(
            'SMTP 未启用或未配置，不能启用或执行邮件计划。',
            'SMTP is not enabled or configured. Schedules cannot be enabled or executed.'
          )}
        </p>
      )}
      {query.isLoading ? <Skeleton className='h-56 w-full' /> : null}
      {!query.isLoading && query.data?.items.length === 0 && (
        <p className='text-muted-foreground py-12 text-center text-sm'>
          {text(
            '暂无定时任务，可在账号明细或账号产出中创建。',
            'No schedules. Create one from account details or account output.'
          )}
        </p>
      )}
      {!query.isLoading && !!query.data?.items.length && (
        <div className='divide-y border-y'>
          <div className='bg-muted/40 text-muted-foreground hidden grid-cols-[minmax(0,2fr)_minmax(0,2fr)_10rem_14rem] gap-4 px-3 py-2 text-xs lg:grid'>
            <span>{text('任务 / 范围', 'Task / Scope')}</span>
            <span>{text('收件邮箱', 'Recipients')}</span>
            <span>{text('下次执行', 'Next run')}</span>
            <span>{text('操作', 'Actions')}</span>
          </div>
          {query.data?.items.map((plan) => (
            <div
              key={plan.id}
              className='grid min-w-0 items-center gap-3 px-3 py-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,2fr)_10rem_14rem]'
            >
              <div className='min-w-0'>
                <div className='flex flex-wrap items-center gap-2'>
                  <strong className='text-sm break-all'>{plan.name}</strong>
                  <Badge variant={plan.enabled ? 'secondary' : 'outline'}>
                    {plan.enabled
                      ? text('启用', 'Enabled')
                      : text('暂停', 'Paused')}
                  </Badge>
                </div>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {plan.config.scope === 'fixed'
                    ? text('固定账号', 'Fixed accounts')
                    : text('动态筛选', 'Dynamic filters')}{' '}
                  ·{' '}
                  {plan.config.query.dataset === 'inventory'
                    ? text('账号明细', 'Account details')
                    : text('账号产出', 'Account output')}{' '}
                  · #{plan.id}
                </p>
                {plan.error_code && (
                  <p className='text-destructive mt-1 text-xs'>
                    {statusText(plan.error_code, text)}
                  </p>
                )}
              </div>
              <div className='text-sm break-all'>
                {plan.config.recipients.map((email) => (
                  <div key={email}>{email}</div>
                ))}
              </div>
              <div className='text-xs tabular-nums'>
                {plan.enabled ? dateTime(plan.next_at) : '—'}
                {plan.active_run_id > 0 && (
                  <p className='mt-1 text-blue-600 dark:text-blue-400'>
                    {text('运行中', 'Active run')} #{plan.active_run_id}
                  </p>
                )}
              </div>
              <div className='flex flex-wrap gap-1'>
                <Button
                  variant='ghost'
                  size='icon-sm'
                  title={text('编辑', 'Edit')}
                  aria-label={text('编辑', 'Edit')}
                  disabled={busy !== 0}
                  onClick={() => setEditing(plan)}
                >
                  <Pencil />
                </Button>
                <Button
                  variant='ghost'
                  size='icon-sm'
                  title={
                    plan.enabled
                      ? text('暂停', 'Pause')
                      : text('恢复', 'Resume')
                  }
                  aria-label={
                    plan.enabled
                      ? text('暂停', 'Pause')
                      : text('恢复', 'Resume')
                  }
                  disabled={
                    busy !== 0 || (!plan.enabled && !query.data?.smtp_ready)
                  }
                  onClick={() =>
                    void action(plan, plan.enabled ? 'pause' : 'resume')
                  }
                >
                  {plan.enabled ? <Pause /> : <Play />}
                </Button>
                <Button
                  variant='ghost'
                  size='icon-sm'
                  title={text('立即执行', 'Run now')}
                  aria-label={text('立即执行', 'Run now')}
                  disabled={
                    busy !== 0 ||
                    !plan.enabled ||
                    !query.data?.smtp_ready ||
                    plan.active_run_id > 0
                  }
                  onClick={() => void action(plan, 'execute')}
                >
                  <RefreshCw
                    className={busy === plan.id ? 'animate-spin' : ''}
                  />
                </Button>
                <Button
                  variant='ghost'
                  size='icon-sm'
                  title={text('运行记录', 'Run history')}
                  aria-label={text('运行记录', 'Run history')}
                  onClick={() => setHistory(plan)}
                >
                  <History />
                </Button>
                <Button
                  variant='ghost'
                  size='icon-sm'
                  title={text('删除', 'Delete')}
                  aria-label={text('删除', 'Delete')}
                  disabled={busy !== 0}
                  onClick={() => void action(plan, 'delete')}
                >
                  <Trash2 />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
      {query.data && (
        <div className='flex items-center justify-end gap-3 text-sm'>
          <Button
            variant='outline'
            disabled={page <= 1}
            onClick={() => setPage((p) => p - 1)}
          >
            {text('上一页', 'Previous')}
          </Button>
          <span>
            {page} / {Math.max(1, Math.ceil(query.data.total / 20))}
          </span>
          <Button
            variant='outline'
            disabled={page * 20 >= query.data.total}
            onClick={() => setPage((p) => p + 1)}
          >
            {text('下一页', 'Next')}
          </Button>
        </div>
      )}
      {editing && (
        <ScheduleForm
          id={editing.id}
          initial={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            void query.refetch()
          }}
        />
      )}
      {history && (
        <RunHistory plan={history} onClose={() => setHistory(null)} />
      )}
    </section>
  )
}

function RunHistory({
  plan,
  onClose,
}: {
  plan: ExportSchedule
  onClose: () => void
}) {
  const text = useScheduleText()
  const [page, setPage] = useState(1)
  const [busy, setBusy] = useState('')
  const query = useQuery({
    queryKey: ['account-export-schedule-runs', plan.id, page],
    queryFn: () => scheduleRuns(plan.id, page),
    refetchInterval: 10000,
    retry: false,
  })
  const download = async (task: string) => {
    setBusy(task)
    try {
      await downloadUsageRecordsExport(task)
    } catch (e) {
      toast.error(scheduleMessage(e, text))
    } finally {
      setBusy('')
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose()
      }}
    >
      <DialogContent className='flex max-h-[90dvh] flex-col overflow-hidden sm:max-w-4xl'>
        <DialogHeader>
          <DialogTitle>
            {text('运行与邮件记录', 'Run and delivery history')}
          </DialogTitle>
          <DialogDescription className='break-all'>
            {plan.name}
          </DialogDescription>
        </DialogHeader>
        <div className='min-h-0 overflow-y-auto'>
          {query.isLoading && <Skeleton className='h-48 w-full' />}
          {query.isError && (
            <Button onClick={() => void query.refetch()} variant='outline'>
              {text('读取失败，重试', 'Load failed; retry')}
            </Button>
          )}
          {query.data?.items.length === 0 && (
            <p className='text-muted-foreground p-6 text-center'>
              {text('暂无运行记录', 'No runs yet')}
            </p>
          )}
          <div className='divide-y'>
            {query.data?.items.map((run) => (
              <article key={run.id} className='grid gap-3 py-4'>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <div className='text-sm font-medium'>
                    #{run.id} · {dateTime(run.scheduled_at)} ·{' '}
                    {statusText(run.status, text)}
                  </div>
                  {run.export?.status === 'succeeded' && (
                    <Button
                      variant='outline'
                      size='sm'
                      disabled={!!busy}
                      onClick={() => void download(run.task_id)}
                    >
                      <Download />
                      {text('下载', 'Download')}
                    </Button>
                  )}
                </div>
                <p className='text-muted-foreground text-xs'>
                  {text('导出状态', 'Export status')}:{' '}
                  {statusText(run.export?.status ?? run.status, text)}
                  {run.missing_count > 0 &&
                    ` · ${text('缺失账号', 'Missing accounts')}: ${run.missing_count}`}
                </p>
                {run.error_code && (
                  <p className='text-sm text-amber-700 dark:text-amber-400'>
                    {statusText(run.error_code, text)}
                  </p>
                )}
                <div className='grid gap-2'>
                  {run.deliveries.map((d) => (
                    <div
                      key={d.id}
                      className='bg-muted/40 flex flex-wrap items-center justify-between gap-2 rounded-md px-3 py-2 text-sm'
                    >
                      <div className='min-w-0'>
                        <div className='break-all'>{d.recipient}</div>
                        <p className='text-muted-foreground text-xs'>
                          {statusText(d.status, text)} ·{' '}
                          {text('尝试次数', 'Attempts')}: {d.attempts}
                          {d.next_attempt_at > 0 &&
                            ` · ${dateTime(d.next_attempt_at)}`}
                        </p>
                        {d.error_code && (
                          <p className='text-destructive text-xs'>
                            {statusText(d.error_code, text)}
                          </p>
                        )}
                      </div>
                      {['failed', 'uncertain'].includes(d.status) && (
                        <Button
                          variant='outline'
                          size='sm'
                          disabled={!!busy}
                          onClick={async () => {
                            if (
                              !window.confirm(
                                d.status === 'uncertain'
                                  ? text(
                                      '请先核对是否已收到邮件。确认再次发送可能产生重复邮件，仍要重发？',
                                      'Verify receipt first. Resending may duplicate a delivered email. Continue?'
                                    )
                                  : text(
                                      '使用原文件重新发送？',
                                      'Resend the existing file?'
                                    )
                              )
                            ) {
                              return
                            }
                            setBusy(String(d.id))
                            try {
                              await retryDelivery(plan.id, d, true)
                              await query.refetch()
                              toast.success(text('已安排重发', 'Resend queued'))
                            } catch (e) {
                              toast.error(scheduleMessage(e, text))
                            } finally {
                              setBusy('')
                            }
                          }}
                        >
                          <RefreshCw />
                          {text('重发', 'Resend')}
                        </Button>
                      )}
                    </div>
                  ))}
                </div>
              </article>
            ))}
          </div>
        </div>
        {query.data && (
          <div className='flex items-center justify-end gap-3 border-t pt-3 text-sm'>
            <Button
              variant='outline'
              disabled={page <= 1}
              onClick={() => setPage((p) => p - 1)}
            >
              {text('上一页', 'Previous')}
            </Button>
            <span>
              {page} / {Math.max(1, Math.ceil(query.data.total / 20))}
            </span>
            <Button
              variant='outline'
              disabled={page * 20 >= query.data.total}
              onClick={() => setPage((p) => p + 1)}
            >
              {text('下一页', 'Next')}
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
