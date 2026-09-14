import { useQuery } from '@tanstack/react-query'
import { RefreshCw, Search } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'

import { mailboxApi } from '../api'
import { Empty, Pagination, QueryState, Status, Time } from './common'
import { PrivateImage } from './private-image'

export function History() {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('')
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const query = useQuery({
    queryKey: ['mailbox', 'submissions', filter, status, page],
    queryFn: ({ signal }) =>
      mailboxApi.submissions(
        { search: filter, status, page, page_size: 20 },
        signal
      ),
  })
  return (
    <section>
      <div className='flex flex-wrap gap-3 border-b py-4'>
        <form
          className='flex min-w-0 flex-1 gap-2'
          onSubmit={(event) => {
            event.preventDefault()
            setFilter(search.trim())
            setPage(1)
          }}
        >
          <Input
            aria-label={t('mailboxPortal.searchEmail')}
            placeholder={t('mailboxPortal.searchEmail')}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <Button
            type='submit'
            variant='outline'
            size='icon'
            title={t('mailboxPortal.search')}
            aria-label={t('mailboxPortal.search')}
          >
            <Search />
          </Button>
        </form>
        <NativeSelect
          aria-label={t('mailboxPortal.status')}
          value={status}
          onChange={(event) => {
            setStatus(event.target.value)
            setPage(1)
          }}
        >
          <option value=''>{t('mailboxPortal.allStatuses')}</option>
          {['pending', 'approved', 'rejected'].map((value) => (
            <option key={value} value={value}>
              {t(
                value === 'pending'
                  ? 'mailboxPortal.submissionPending'
                  : `mailboxPortal.status_${value}`
              )}
            </option>
          ))}
        </NativeSelect>
        <Button
          variant='outline'
          size='icon'
          disabled={query.isFetching}
          title={t('mailboxPortal.refresh')}
          aria-label={t('mailboxPortal.refresh')}
          onClick={() => void query.refetch()}
        >
          <RefreshCw />
        </Button>
      </div>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
      >
        {!query.data?.items.length && <Empty />}
        <div className='divide-y'>
          {query.data?.items.map((submission) => (
            <article key={submission.id} className='grid gap-4 py-5'>
              <div className='flex flex-wrap items-start justify-between gap-3'>
                <div className='grid min-w-0 gap-1'>
                  <h2 className='text-sm font-semibold [overflow-wrap:anywhere]'>
                    {submission.email}
                  </h2>
                  <span className='text-muted-foreground text-xs'>
                    {t('mailboxPortal.submission', { id: submission.id })} /{' '}
                    <Time value={submission.created_at} />
                  </span>
                </div>
                <Status submission status={submission.status} />
              </div>
              {submission.reviewed_at > 0 && (
                <div className='border-l-2 border-emerald-600 pl-3 text-sm'>
                  <p className='text-muted-foreground mb-1 text-xs'>
                    {t('mailboxPortal.reviewed')}{' '}
                    <Time value={submission.reviewed_at} />
                  </p>
                  <p className='[overflow-wrap:anywhere] whitespace-pre-wrap'>
                    {submission.review_reason ||
                      t('mailboxPortal.noReviewReason')}
                  </p>
                </div>
              )}
              <div className='grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5'>
                {(submission.attachments ?? []).map((attachment) => (
                  <PrivateImage key={attachment.id} attachment={attachment} />
                ))}
              </div>
            </article>
          ))}
        </div>
      </QueryState>
      <Pagination
        data={query.data}
        page={page}
        pending={query.isFetching}
        onPage={setPage}
      />
    </section>
  )
}
