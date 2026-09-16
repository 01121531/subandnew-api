import { ArrowLeft, Eye, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { mailboxApi } from '../api'
import { useMailboxQuery } from '../hooks'
import { canReviewWorkSubmission } from '../lib/work'
import type {
  AccountType,
  Attachment,
  Submission,
  WorkAccount,
  WorkAssignment,
} from '../types'
import { Pager, QueryState } from './common'
import { IssueDialog } from './issues'
import { PrivateImage } from './private-image'
import { ReviewDialog } from './review-dialog'
import { WorkAccountFacts } from './work-accounts'
import { WorkStatus, WorkTime as Time } from './work-controls'

function HistoryImages(props: {
  attachments: Attachment[]
  accountType: AccountType
}) {
  const { t } = useTranslation()
  if (!props.attachments.length) {
    return (
      <p className='text-muted-foreground text-xs'>
        {t('mailbox.issues.noAttachments')}
      </p>
    )
  }
  return (
    <div className='grid gap-3 sm:grid-cols-2'>
      {props.attachments.map((attachment, index) => (
        <PrivateImage
          key={attachment.id}
          attachment={attachment}
          index={index}
          accountType={props.accountType}
        />
      ))}
    </div>
  )
}

function AssignmentRecord(props: { item: WorkAssignment }) {
  const { t } = useTranslation()
  const item = props.item
  return (
    <article className='space-y-2 py-4 text-sm'>
      <div className='flex flex-wrap items-center gap-2'>
        <span>
          #{item.id} · {t('mailbox.work.version', { version: item.version })}
        </span>
        <WorkStatus value={item.status} />
        <span>
          {t(
            item.assignment_active
              ? 'mailbox.work.active'
              : 'mailbox.work.inactive'
          )}
        </span>
      </div>
      <dl className='grid gap-2 text-xs sm:grid-cols-2'>
        <div>
          <dt className='text-muted-foreground'>
            {t('mailbox.work.assignedBy')}
          </dt>
          <dd>#{item.assigned_by}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            {t('mailbox.admin.assignedAt')}
          </dt>
          <dd>
            <Time value={item.created_at} />
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            {t('mailbox.admin.updatedAt')}
          </dt>
          <dd>
            <Time value={item.updated_at} />
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            {t('mailbox.work.revokedAt')}
          </dt>
          <dd>
            <Time value={item.revoked_at} />
          </dd>
        </div>
      </dl>
    </article>
  )
}

const historyTabs = ['assignments', 'submissions', 'issues'] as const
type HistoryTab = (typeof historyTabs)[number]
const pageKeys = {
  assignments: 'assignment_page',
  submissions: 'submission_page',
  issues: 'issue_page',
} as const

export function WorkHistoryView(props: {
  operatorID: number
  account: WorkAccount
  onBack: () => void
}) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<HistoryTab>('assignments')
  const [pages, setPages] = useState({
    assignment_page: 1,
    submission_page: 1,
    issue_page: 1,
  })
  const [review, setReview] = useState<Submission>()
  const [issue, setIssue] = useState<number>()
  const accountType = props.account.account_type
  const query = useMailboxQuery(
    [
      'operator-work-history',
      props.operatorID,
      accountType,
      props.account.id,
      pages,
    ],
    (signal) =>
      mailboxApi.workHistory(
        props.operatorID,
        props.account.id,
        { account_type: accountType, ...pages, page_size: 20 },
        signal
      )
  )
  const data = query.data
  return (
    <section className='min-w-0 space-y-4'>
      <div className='flex flex-wrap justify-between gap-2'>
        <Button variant='outline' onClick={props.onBack}>
          <ArrowLeft />
          {t('mailbox.work.back')}
        </Button>
        <Button
          variant='outline'
          size='icon'
          disabled={query.isFetching}
          title={t('mailbox.admin.refresh')}
          aria-label={t('mailbox.admin.refresh')}
          onClick={() => void query.refetch()}
        >
          <RefreshCw />
        </Button>
      </div>
      <h3 className='text-sm font-semibold break-all'>{props.account.email}</h3>
      <p className='text-muted-foreground text-xs'>
        {t('mailbox.work.historyRange')}
      </p>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
      >
        {data && (
          <>
            <WorkAccountFacts account={data.account} />
            <Tabs
              value={tab}
              onValueChange={(value) => setTab(value as HistoryTab)}
              className='min-w-0'
            >
              <div className='overflow-x-auto border-b'>
                <TabsList variant='line'>
                  {historyTabs.map((key) => (
                    <TabsTrigger key={key} value={key} className='px-2 text-xs'>
                      {t(`mailbox.work.${key}`)} ({data[key].total})
                    </TabsTrigger>
                  ))}
                </TabsList>
              </div>
              {historyTabs.map((key) => (
                <TabsContent key={key} value={key} className='min-w-0'>
                  {tab === key && (
                    <>
                      {!data[key].items.length && (
                        <p
                          role='status'
                          className='text-muted-foreground py-8 text-center text-sm'
                        >
                          {t('mailbox.admin.empty')}
                        </p>
                      )}
                      <div className='divide-y'>
                        {key === 'assignments' &&
                          data.assignments.items.map((item) => (
                            <AssignmentRecord key={item.id} item={item} />
                          ))}
                        {key === 'submissions' &&
                          data.submissions.items.map((item) => (
                            <article key={item.id} className='space-y-3 py-4'>
                              <div className='flex flex-wrap items-center gap-2 text-xs'>
                                <span>
                                  #{item.id} ·{' '}
                                  {t('mailbox.work.assignmentRef', {
                                    id: item.assignment_id,
                                  })}
                                </span>
                                <WorkStatus value={item.status} review />
                                <Time value={item.created_at} />
                                {canReviewWorkSubmission(
                                  data.account,
                                  item
                                ) && (
                                  <Button
                                    variant='outline'
                                    size='sm'
                                    onClick={() => setReview(item)}
                                  >
                                    <Eye />
                                    {t('mailbox.admin.tabs.review')}
                                  </Button>
                                )}
                              </div>
                              {!!item.reviewed_at && (
                                <div className='text-xs'>
                                  <p>
                                    {t('mailbox.work.reviewedBy', {
                                      id: item.reviewed_by,
                                    })}{' '}
                                    · <Time value={item.reviewed_at} />
                                  </p>
                                  <p className='[overflow-wrap:anywhere] whitespace-pre-wrap'>
                                    {item.review_reason}
                                  </p>
                                </div>
                              )}
                              <HistoryImages
                                attachments={item.attachments}
                                accountType={accountType}
                              />
                            </article>
                          ))}
                        {key === 'issues' &&
                          data.issues.items.map((item) => (
                            <article key={item.id} className='space-y-3 py-4'>
                              <div className='flex flex-wrap items-center gap-2 text-xs'>
                                <span>
                                  #{item.id} ·{' '}
                                  {t('mailbox.work.assignmentRef', {
                                    id: item.assignment_id,
                                  })}
                                </span>
                                <span>{t(`mailbox.issues.${item.kind}`)}</span>
                                <span>
                                  {t(`mailbox.issues.${item.status}`)}
                                </span>
                                <Time value={item.created_at} />
                                <Button
                                  variant='outline'
                                  size='sm'
                                  onClick={() => setIssue(item.id)}
                                >
                                  <Eye />
                                  {t('mailbox.admin.details')}
                                </Button>
                              </div>
                              <p className='text-sm [overflow-wrap:anywhere] whitespace-pre-wrap'>
                                {item.description}
                              </p>
                              {item.reply && (
                                <div className='text-xs'>
                                  <h4 className='font-medium'>
                                    {t('mailbox.issues.reply')}
                                  </h4>
                                  <p className='[overflow-wrap:anywhere] whitespace-pre-wrap'>
                                    {item.reply}
                                  </p>
                                  <Time value={item.resolved_at} />
                                </div>
                              )}
                              <HistoryImages
                                attachments={item.attachments}
                                accountType={accountType}
                              />
                            </article>
                          ))}
                      </div>
                      <Pager
                        page={pages[pageKeys[key]]}
                        total={data[key].total}
                        hasMore={data[key].has_more}
                        pending={query.isFetching}
                        onPage={(page) =>
                          setPages((old) => ({ ...old, [pageKeys[key]]: page }))
                        }
                      />
                    </>
                  )}
                </TabsContent>
              ))}
            </Tabs>
          </>
        )}
      </QueryState>
      {review && (
        <ReviewDialog
          accountType={accountType}
          submission={review}
          onClose={() => setReview(undefined)}
        />
      )}
      {issue !== undefined && (
        <IssueDialog
          id={issue}
          accountType={accountType}
          onClose={() => setIssue(undefined)}
        />
      )}
    </section>
  )
}
