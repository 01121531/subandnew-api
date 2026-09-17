import { useQuery } from '@tanstack/react-query'
import { ArrowRight, RefreshCw, Search } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'

import { mailboxApi } from '../api'
import { useRetryCooldown } from '../hooks/use-retry-cooldown'
import {
  clearPageCredentials,
  credentialPageGeneration,
  failPageCredentials,
  reconcilePageCredentials,
} from '../lib/visible-credentials'
import type { Account, AccountType, Page } from '../types'
import { AccountDetail } from './account-detail'
import { Empty, Pagination, QueryState, Status, Time } from './common'

export function Accounts(props: { accountType: AccountType; csrf: string }) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('')
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<number | null>(null)
  const navigation = useRef(new AbortController())
  useLayoutEffect(
    () => () => clearPageCredentials(),
    [props.csrf, props.accountType, filter, status, page]
  )
  useEffect(() => {
    const active = new AbortController()
    navigation.current = active
    return () => active.abort()
  }, [])
  const query = useQuery<Page<Account>>({
    queryKey: ['mailbox', 'accounts', props.accountType, filter, status, page],
    refetchInterval: (query) => (query.state.error ? false : 60000),
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    queryFn: async ({ signal }) => {
      const generation = credentialPageGeneration()
      try {
        const result = await mailboxApi.accounts(
          {
            account_type: props.accountType,
            search: filter,
            status,
            page,
            page_size: 20,
            include_summary: true,
            sort: 'pending_first',
          },
          signal
        )
        if (!signal.aborted) reconcilePageCredentials(result.items, generation)
        return result
      } catch (error) {
        if (!signal.aborted && generation === credentialPageGeneration()) {
          failPageCredentials(error)
        }
        throw error
      }
    },
  })
  const counts = Object.entries(query.data?.status_counts ?? {})
  const cooldown = useRetryCooldown(query.error)
  return (
    <section className='min-w-0'>
      <div className='flex flex-wrap items-center gap-3 border-b py-4'>
        <form
          className='flex min-w-0 flex-1 gap-2'
          onSubmit={(event) => {
            event.preventDefault()
            setFilter(search.trim())
            setPage(1)
          }}
        >
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            aria-label={t('mailboxPortal.searchEmail')}
            placeholder={t('mailboxPortal.searchEmail')}
            className='min-w-0'
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
          {[
            'pending',
            'submitted',
            'approved',
            'rejected',
            'issue_pending',
          ].map((value) => (
            <option key={value} value={value}>
              {t(`mailboxPortal.status_${value}`)}
            </option>
          ))}
        </NativeSelect>
        <Button
          variant='outline'
          size='icon'
          disabled={query.isFetching || cooldown > 0}
          title={t('mailboxPortal.refresh')}
          aria-label={t('mailboxPortal.refresh')}
          onClick={() => void query.refetch()}
        >
          <RefreshCw />
        </Button>
      </div>
      {!!counts.length && !query.error && (
        <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-2 border-b py-3 text-xs'>
          <span>
            {t('mailboxPortal.filteredTotal', {
              count: query.data?.total ?? 0,
            })}
          </span>
          {counts.map(([value, count]) => (
            <span key={value}>
              {t(`mailboxPortal.status_${value}`)}{' '}
              <strong className='text-foreground tabular-nums'>{count}</strong>
            </span>
          ))}
        </div>
      )}
      <QueryState
        pending={query.isPending}
        error={query.error}
        preserveData={!!query.data}
        retry={() => void query.refetch()}
      >
        {!query.data?.items.length && <Empty />}
        <div className='divide-y'>
          {query.data?.items.map((account) => (
            <article
              key={account.id}
              className='grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-2 py-4 md:grid-cols-[minmax(0,1fr)_110px_180px_40px]'
            >
              <div className='grid min-w-0 gap-1'>
                <button
                  type='button'
                  className='min-w-0 text-left text-sm font-medium [overflow-wrap:anywhere] underline-offset-4 hover:underline focus-visible:underline'
                  onClick={() => setSelected(account.id)}
                >
                  {account.email}
                </button>
              </div>
              <div>
                <Status status={account.status} />
              </div>
              <span className='text-muted-foreground text-xs'>
                <Time value={account.assigned_at} />
              </span>
              <Button
                variant='ghost'
                size='icon'
                title={t('mailboxPortal.accountDetails')}
                aria-label={t('mailboxPortal.detailsFor', {
                  email: account.email,
                })}
                onClick={() => setSelected(account.id)}
              >
                <ArrowRight />
              </Button>
            </article>
          ))}
        </div>
      </QueryState>
      {query.data && (
        <Pagination
          data={query.data}
          page={page}
          pending={query.isFetching}
          onPage={setPage}
        />
      )}
      {selected !== null && (
        <AccountDetail
          accountType={props.accountType}
          key={selected}
          id={selected}
          csrf={props.csrf}
          onReported={() => {
            const previous = selected
            setSelected(null)
            const signal = navigation.current.signal
            void (async () => {
              let first: number | undefined
              for (let nextPage = 1; !signal.aborted; nextPage++) {
                const result = await mailboxApi.accounts(
                  {
                    account_type: props.accountType,
                    status: 'actionable',
                    page: nextPage,
                    page_size: 100,
                  },
                  signal
                )
                first ??= result.items[0]?.id
                const next = result.items.find((item) => item.id < previous)
                if (signal.aborted) return
                if (next || !result.has_more || !result.items.length) {
                  setSelected(next?.id ?? first ?? null)
                  break
                }
              }
            })()
              .then(() => {
                void query.refetch()
              })
              .catch(() => {
                void query.refetch()
              })
          }}
          onClose={() => setSelected(null)}
          onSubmitted={() => {
            setSelected(null)
            toast.success(t('mailboxPortal.submissionSucceeded'))
          }}
        />
      )}
    </section>
  )
}
