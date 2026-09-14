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

import { mailboxApi } from '../api'
import { useMailboxMutation, useMailboxQuery } from '../hooks'
import type { Operator } from '../types'
import { Confirm, Pager, QueryState, SearchBar, Time } from './common'
import { OperatorDialog, ResetPasswordDialog } from './operator-dialog'

export function Operators() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [editing, setEditing] = useState<Operator | 'new'>()
  const [reset, setReset] = useState<Operator>()
  const [revoke, setRevoke] = useState<Operator>()
  const query = useMailboxQuery(['operators', page, search], (signal) =>
    mailboxApi.operators({ page, page_size: 20, search }, signal)
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
  return (
    <section className='min-w-0'>
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
                {[
                  'username',
                  'displayName',
                  'status',
                  'updatedAt',
                  'actions',
                ].map((key) => (
                  <TableHead key={key}>{t(`mailbox.admin.${key}`)}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className='max-w-52 break-all whitespace-normal'>
                    {item.username}
                  </TableCell>
                  <TableCell className='max-w-52 break-all whitespace-normal'>
                    {item.display_name}
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
                    {item.display_name}
                  </h3>
                  <p className='text-muted-foreground text-xs break-all'>
                    {item.username}
                  </p>
                </div>
                {actions(item)}
              </div>
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
