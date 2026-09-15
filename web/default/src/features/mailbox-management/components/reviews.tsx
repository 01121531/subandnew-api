import { Eye } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

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
import { useMailboxQuery } from '../hooks'
import type { AccountType, Submission } from '../types'
import { Pager, QueryState, SearchBar, Status, Time } from './common'
import { PoolScope } from './pool-scope'
import { ReviewDialog } from './review-dialog'

export function Reviews() {
  return (
    <PoolScope>
      {(accountType) => (
        <PoolReviews key={accountType} accountType={accountType} />
      )}
    </PoolScope>
  )
}

function PoolReviews(props: { accountType: AccountType }) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('pending')
  const [selected, setSelected] = useState<Submission>()
  const query = useMailboxQuery(
    ['submissions', props.accountType, page, search, status],
    (signal) =>
      mailboxApi.submissions(
        {
          page,
          page_size: 20,
          search,
          status,
          account_type: props.accountType,
        },
        signal
      )
  )
  const rows = query.data?.items ?? []
  function open(item: Submission) {
    return (
      <Button
        variant='outline'
        size='icon'
        title={t('mailbox.admin.details')}
        aria-label={t('mailbox.admin.details')}
        onClick={() => setSelected(item)}
      >
        <Eye />
      </Button>
    )
  }
  return (
    <section className='min-w-0'>
      <SearchBar
        review
        value={search}
        onChange={(value) => {
          setSearch(value)
          setPage(1)
        }}
        status={status}
        onStatus={(value) => {
          setStatus(value)
          setPage(1)
        }}
        pending={query.isFetching}
        refresh={() => void query.refetch()}
      />
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
                {['email', 'operator', 'status', 'submittedAt', 'actions'].map(
                  (key) => (
                    <TableHead key={key}>{t(`mailbox.admin.${key}`)}</TableHead>
                  )
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className='max-w-72 break-all whitespace-normal'>
                    {item.email}
                    <div className='text-muted-foreground text-xs'>
                      #{item.id}
                    </div>
                  </TableCell>
                  <TableCell className='max-w-40 break-all whitespace-normal'>
                    {item.operator_name}
                  </TableCell>
                  <TableCell>
                    <Status value={item.status} review />
                  </TableCell>
                  <TableCell>
                    <Time value={item.created_at} />
                  </TableCell>
                  <TableCell>{open(item)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <div className='grid gap-3 md:hidden'>
          {rows.map((item) => (
            <article
              key={item.id}
              className='min-w-0 space-y-3 rounded-lg border p-3'
            >
              <div className='flex items-start gap-3'>
                <h3 className='min-w-0 flex-1 text-sm font-medium break-all'>
                  {item.email}
                </h3>
                {open(item)}
              </div>
              <div className='flex flex-wrap items-center justify-between gap-2 text-sm'>
                <Status value={item.status} review />
                <span className='break-all'>{item.operator_name}</span>
              </div>
              <p className='text-muted-foreground text-xs'>
                <Time value={item.created_at} />
              </p>
              {item.review_reason && (
                <p className='line-clamp-3 text-sm break-words'>
                  {item.review_reason}
                </p>
              )}
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
      {selected && (
        <ReviewDialog
          accountType={props.accountType}
          submission={selected}
          onClose={() => setSelected(undefined)}
        />
      )}
    </section>
  )
}
