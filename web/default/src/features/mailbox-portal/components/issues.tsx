import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { mailboxApi } from '../api'
import type { AccountType } from '../types'
import { Empty, Pagination, QueryState, Time } from './common'
import { PrivateImage } from './private-image'

export function Issues(props: { accountType: AccountType }) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<number>()
  const query = useQuery({
    queryKey: ['mailbox', 'issues', props.accountType, page],
    queryFn: ({ signal }) =>
      mailboxApi.issues(
        { account_type: props.accountType, page, page_size: 20 },
        signal
      ),
  })
  return (
    <section className='min-w-0 py-4'>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
      >
        {!query.data?.items.length && <Empty />}
        <div className='divide-y'>
          {query.data?.items.map((item) => (
            <article key={item.id} className='space-y-3 py-4'>
              <div className='flex flex-wrap justify-between gap-2'>
                <h3 className='min-w-0 text-sm font-medium break-all'>
                  {item.email}
                </h3>
                <span className='text-xs'>
                  {t(`mailbox.issues.${item.status}`)}
                </span>
              </div>
              <p className='text-muted-foreground text-xs'>
                {t(`mailbox.issues.${item.kind}`)} ·{' '}
                <Time value={item.created_at} />
              </p>
              <p className='text-sm [overflow-wrap:anywhere] whitespace-pre-wrap'>
                {item.description}
              </p>
              {item.reply && (
                <div className='bg-muted/40 p-3 text-sm'>
                  <h4 className='font-medium'>{t('mailbox.issues.reply')}</h4>
                  <p className='[overflow-wrap:anywhere] whitespace-pre-wrap'>
                    {item.reply}
                  </p>
                  <Time value={item.resolved_at} />
                </div>
              )}
              {item.status === 'invalidated' && (
                <p className='text-muted-foreground text-sm'>
                  {t('mailbox.issues.inactive')}
                </p>
              )}
              {!!item.attachments.length && (
                <Button
                  variant='outline'
                  onClick={() =>
                    setSelected(selected === item.id ? undefined : item.id)
                  }
                >
                  {t('mailboxPortal.screenshots')} ({item.attachments.length})
                </Button>
              )}
              {selected === item.id && (
                <div className='grid gap-3 sm:grid-cols-2'>
                  {item.attachments.map((attachment) => (
                    <PrivateImage
                      key={attachment.id}
                      accountType={props.accountType}
                      attachment={attachment}
                    />
                  ))}
                </div>
              )}
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
