import { Eye } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'

import { mailboxApi } from '../api'
import { useMailboxQuery } from '../hooks'
import { workScopes, type WorkSelection } from '../lib/work'
import type { WorkAccount, WorkRangeQuery, WorkScope } from '../types'
import { Pager, QueryState, SearchBar } from './common'
import { WorkStatus, WorkTime as Time } from './work-controls'

export function WorkAccountFacts(props: { account: WorkAccount }) {
  const { t } = useTranslation()
  const item = props.account
  return (
    <div className='space-y-2 text-xs'>
      <div className='flex flex-wrap items-center gap-2'>
        <Badge variant='outline'>
          {t(`mailbox.admin.pools.${item.account_type}`)}
        </Badge>
        {item.assignment_active && !item.revoked_at && (
          <WorkStatus value={item.status} />
        )}
        <span>
          {t(
            item.assignment_active && !item.revoked_at
              ? 'mailbox.work.active'
              : 'mailbox.work.inactive'
          )}
        </span>
        {!!item.archived_at && (
          <Badge variant='outline'>{t('mailbox.archive.archived')}</Badge>
        )}
        {item.card_last4 && (
          <span>
            {t('mailbox.admin.cardEnding', { last4: item.card_last4 })}
          </span>
        )}
      </div>
      <dl className='grid grid-cols-1 gap-2 text-xs sm:grid-cols-2'>
        <div>
          <dt className='text-muted-foreground'>
            {t('mailbox.work.latestAssignment')}
          </dt>
          <dd>
            #{item.assignment_id} ·{' '}
            {t('mailbox.work.version', { version: item.assignment_version })}
            <span className='ml-2'>
              <WorkStatus value={item.status} />
            </span>
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            {t('mailbox.work.latestSubmission')}
          </dt>
          <dd>
            {item.latest_submission_id ? `#${item.latest_submission_id} ` : ''}
            {item.latest_submission_status ? (
              <WorkStatus value={item.latest_submission_status} review />
            ) : (
              '--'
            )}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            {t('mailbox.admin.assignedAt')}
          </dt>
          <dd>
            <Time value={item.assigned_at} />
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            {t('mailbox.work.lifetimeSubmissions')}
          </dt>
          <dd>{item.submission_count}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            {t('mailbox.work.lastSubmitted')}
          </dt>
          <dd>
            <Time value={item.last_submitted_at} />
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            {t('mailbox.work.lastIssue')}
          </dt>
          <dd>
            <Time value={item.last_issue_at} />
          </dd>
        </div>
        {!!item.revoked_at && (
          <div>
            <dt className='text-muted-foreground'>
              {t('mailbox.work.revokedAt')}
            </dt>
            <dd>
              <Time value={item.revoked_at} />
            </dd>
          </div>
        )}
        {!!item.archived_at && (
          <div>
            <dt className='text-muted-foreground'>
              {t('mailbox.archive.archivedAt')}
            </dt>
            <dd>
              <Time value={item.archived_at} />
            </dd>
          </div>
        )}
      </dl>
    </div>
  )
}

export function WorkAccounts(props: {
  operatorID: number
  range: WorkRangeQuery
  selection: WorkSelection
  onSelection: (selection: WorkSelection) => void
  onAccount: (account: WorkAccount) => void
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const params = {
    ...props.range,
    ...props.selection,
    page,
    page_size: 20,
    search,
  }
  const query = useMailboxQuery(
    ['operator-work-accounts', props.operatorID, params],
    (signal) => mailboxApi.workAccounts(props.operatorID, params, signal)
  )
  return (
    <section className='min-w-0 py-3'>
      <h3 className='text-sm font-semibold'>{t('mailbox.work.accounts')}</h3>
      <div className='flex flex-wrap gap-2 pt-3'>
        <NativeSelect
          className='max-w-full min-w-0'
          aria-label={t('mailbox.admin.accountType')}
          value={props.selection.account_type}
          onChange={(event) => {
            props.onSelection({
              ...props.selection,
              account_type: event.target.value as WorkSelection['account_type'],
            })
            setPage(1)
          }}
        >
          <option value='all'>{t('mailbox.work.allTypes')}</option>
          {(['refund', 'opening'] as const).map((type) => (
            <option key={type} value={type}>
              {t(`mailbox.admin.pools.${type}`)}
            </option>
          ))}
        </NativeSelect>
        <NativeSelect
          className='max-w-full min-w-0'
          aria-label={t('mailbox.work.scope')}
          value={props.selection.scope}
          onChange={(event) => {
            props.onSelection({
              ...props.selection,
              scope: event.target.value as WorkScope,
            })
            setPage(1)
          }}
        >
          {workScopes.map((scope) => (
            <option key={scope} value={scope}>
              {t(`mailbox.work.scopes.${scope}`)}
            </option>
          ))}
        </NativeSelect>
      </div>
      <SearchBar
        value={search}
        onChange={(value) => {
          setSearch(value)
          setPage(1)
        }}
        pending={query.isFetching}
        refresh={() => void query.refetch()}
      />
      <p className='text-muted-foreground pb-3 text-xs'>
        {t(
          props.selection.scope.startsWith('current_')
            ? 'mailbox.work.currentRange'
            : 'mailbox.work.eventRange'
        )}
      </p>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
        empty={!query.data?.items.length}
      >
        <div className='divide-y'>
          {(query.data?.items ?? []).map((item) => (
            <article
              key={`${item.account_type}:${item.id}`}
              className='min-w-0 space-y-3 py-4'
            >
              <div className='flex items-start justify-between gap-2'>
                <button
                  type='button'
                  className='focus-visible:ring-ring min-w-0 rounded text-left text-sm font-medium break-all hover:underline focus-visible:ring-2'
                  onClick={() => props.onAccount(item)}
                >
                  {item.email}
                </button>
                <Button
                  variant='outline'
                  size='icon'
                  aria-label={t('mailbox.admin.details')}
                  title={t('mailbox.admin.details')}
                  onClick={() => props.onAccount(item)}
                >
                  <Eye />
                </Button>
              </div>
              <WorkAccountFacts account={item} />
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
    </section>
  )
}
