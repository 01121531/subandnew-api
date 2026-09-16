import { KeyRound, LogOut, Pencil, Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useAuthStore } from '@/stores/auth-store'

import { mailboxApi } from '../api'
import { useMailboxMutation, useMailboxQuery } from '../hooks'
import { canMailboxWork } from '../lib/permissions'
import { workMetrics, type WorkSelection } from '../lib/work'
import type { Operator, WorkRangeQuery } from '../types'
import { Confirm, Pager, QueryState, SearchBar, Time } from './common'
import { OperatorDialog, ResetPasswordDialog } from './operator-dialog'
import { OperatorWorkDialog } from './operator-work-dialog'
import { WorkCount, WorkDateFilter } from './work-controls'

export function Operators() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canStats = canMailboxWork(user)
  const [range, setRange] = useState<WorkRangeQuery>({ period: 'all' })
  const [work, setWork] = useState<{
    operator: Operator
    selection: WorkSelection
  }>()
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [editing, setEditing] = useState<Operator | 'new'>()
  const [reset, setReset] = useState<Operator>()
  const [revoke, setRevoke] = useState<Operator>()
  const query = useMailboxQuery(
    ['operators', page, search, canStats, canStats ? range : null],
    (signal) =>
      mailboxApi.operators(
        {
          page,
          page_size: 20,
          search,
          ...(canStats ? { include_stats: true, ...range } : {}),
        },
        signal
      )
  )
  const mutation = useMailboxMutation(
    async () => {
      if (revoke) await mailboxApi.revoke(revoke.id)
    },
    () => setRevoke(undefined)
  )
  function actions(item: Operator) {
    return (
      <div className='flex shrink-0 gap-1'>
        <Button
          variant='ghost'
          size='icon'
          title={t('mailbox.admin.editOperator')}
          aria-label={t('mailbox.admin.editOperator')}
          onClick={() => setEditing(item)}
        >
          <Pencil />
        </Button>
        <Button
          variant='ghost'
          size='icon'
          title={t('mailbox.admin.resetPassword')}
          aria-label={t('mailbox.admin.resetPassword')}
          onClick={() => setReset(item)}
        >
          <KeyRound />
        </Button>
        <Button
          variant='ghost'
          size='icon'
          title={t('mailbox.admin.revokeSessions')}
          aria-label={t('mailbox.admin.revokeSessions')}
          onClick={() => setRevoke(item)}
        >
          <LogOut />
        </Button>
      </div>
    )
  }
  const rows = query.data?.items ?? []
  function openWork(
    operator: Operator,
    selection: WorkSelection = { scope: 'submitted', account_type: 'all' }
  ) {
    setWork({ operator, selection })
  }
  function operatorName(item: Operator, display = false) {
    const name = display ? item.display_name : item.username
    if (!canStats) return name
    return (
      <button
        type='button'
        className='focus-visible:ring-ring rounded text-left break-all hover:underline focus-visible:ring-2'
        aria-label={t('mailbox.work.operatorDetails', {
          operator: item.display_name || item.username,
        })}
        onClick={() => openWork(item)}
      >
        {name}
      </button>
    )
  }
  return (
    <section className='min-w-0'>
      {canStats && (
        <WorkDateFilter
          value={range}
          onChange={(value) => {
            setRange(value)
            setPage(1)
          }}
        />
      )}
      <SearchBar
        value={search}
        onChange={(value) => {
          setSearch(value)
          setPage(1)
        }}
        pending={query.isFetching}
        refresh={() => void query.refetch()}
      >
        <Button onClick={() => setEditing('new')}>
          <Plus />
          {t('mailbox.admin.createOperator')}
        </Button>
      </SearchBar>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
        empty={!rows.length}
      >
        <div className='hidden md:block'>
          <Table>
            <TableHeader>
              <TableRow>
                {['username', 'displayName', 'status', 'updatedAt'].map(
                  (key) => (
                    <TableHead key={key}>{t(`mailbox.admin.${key}`)}</TableHead>
                  )
                )}
                {canStats &&
                  workMetrics.map((metric) => (
                    <TableHead
                      key={metric}
                      className='max-w-32 whitespace-normal'
                    >
                      {t(`mailbox.work.metrics.${metric}`)}
                    </TableHead>
                  ))}
                <TableHead>{t('mailbox.admin.actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className='max-w-52 break-all whitespace-normal'>
                    {operatorName(item)}
                  </TableCell>
                  <TableCell className='max-w-52 break-all whitespace-normal'>
                    {operatorName(item, true)}
                  </TableCell>
                  <TableCell>
                    <Badge variant={item.enabled ? 'secondary' : 'outline'}>
                      {t(
                        item.enabled
                          ? 'mailbox.admin.enabled'
                          : 'mailbox.admin.disabled'
                      )}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Time value={item.updated_at} />
                  </TableCell>
                  {canStats &&
                    workMetrics.map((metric) => (
                      <TableCell key={metric}>
                        <WorkCount
                          metric={metric}
                          operatorName={item.display_name || item.username}
                          summary={item.work_summary}
                          onSelect={(selection) => openWork(item, selection)}
                        />
                      </TableCell>
                    ))}
                  <TableCell>{actions(item)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <div className='grid gap-3 md:hidden'>
          {rows.map((item) => (
            <article key={item.id} className='min-w-0 rounded-lg border p-3'>
              <div className='flex flex-wrap items-start justify-between gap-2'>
                <div className='min-w-0 flex-1'>
                  <h3 className='text-sm font-medium break-all'>
                    {operatorName(item, true)}
                  </h3>
                  <p className='text-muted-foreground text-xs break-all'>
                    {item.username}
                  </p>
                </div>
                {actions(item)}
              </div>
              {canStats && (
                <dl className='mt-3 grid grid-cols-2 gap-2 border-t pt-3'>
                  {workMetrics.map((metric) => (
                    <div key={metric} className='min-w-0'>
                      <dt className='text-muted-foreground text-xs'>
                        {t(`mailbox.work.metrics.${metric}`)}
                      </dt>
                      <dd>
                        <WorkCount
                          metric={metric}
                          operatorName={item.display_name || item.username}
                          summary={item.work_summary}
                          onSelect={(selection) => openWork(item, selection)}
                        />
                      </dd>
                    </div>
                  ))}
                </dl>
              )}
              <div className='mt-3 flex flex-wrap items-center justify-between gap-2 text-xs'>
                <Badge variant='outline'>
                  {t(
                    item.enabled
                      ? 'mailbox.admin.enabled'
                      : 'mailbox.admin.disabled'
                  )}
                </Badge>
                <Time value={item.updated_at} />
              </div>
            </article>
          ))}
        </div>
      </QueryState>
      <Pager
        page={page}
        total={query.data?.total}
        hasMore={query.data?.has_more}
        pending={query.isFetching}
        onPage={setPage}
      />
      {editing && (
        <OperatorDialog
          operator={editing === 'new' ? undefined : editing}
          onClose={() => setEditing(undefined)}
        />
      )}
      {canStats && work && (
        <OperatorWorkDialog
          operator={work.operator}
          range={range}
          selection={work.selection}
          onClose={() => setWork(undefined)}
        />
      )}
      {reset && (
        <ResetPasswordDialog
          operator={reset}
          onClose={() => setReset(undefined)}
        />
      )}
      {revoke && (
        <Confirm
          title={t('mailbox.admin.revokeSessions')}
          description={t('mailbox.admin.revokeConfirm')}
          pending={mutation.isPending}
          onClose={() => setRevoke(undefined)}
          onConfirm={() => mutation.submit(undefined)}
        />
      )}
    </section>
  )
}
