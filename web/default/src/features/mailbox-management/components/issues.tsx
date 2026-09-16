import { Eye, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { useAuthStore } from '@/stores/auth-store'

import { mailboxApi } from '../api'
import { useMailboxMutation, useMailboxQuery } from '../hooks'
import { canMailbox } from '../lib/permissions'
import type { AccountType, ResolveIssueInput, CardFilter } from '../types'
import { CardFilters } from './card-filters'
import { Field, Modal, Pager, QueryState, Time } from './common'
import { DataExportButton } from './data-export-button'
import { PoolScope } from './pool-scope'
import { PrivateImage } from './private-image'

export function Issues() {
  return (
    <PoolScope>
      {(accountType) => (
        <PoolIssues key={accountType} accountType={accountType} />
      )}
    </PoolScope>
  )
}

function PoolIssues(props: { accountType: AccountType }) {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('pending')
  const [kind, setKind] = useState('')
  const [operator, setOperator] = useState('')
  const [selected, setSelected] = useState<number>()
  const [cardFilters, setCardFilters] = useState<CardFilter[]>([])
  const operators = useMailboxQuery(['issue-operators'], (signal) =>
    mailboxApi.issueOperators(signal)
  )
  const query = useMailboxQuery(
    [
      'issues',
      props.accountType,
      page,
      search,
      status,
      kind,
      operator,
      cardFilters,
    ],
    (signal) =>
      mailboxApi.issues(
        {
          account_type: props.accountType,
          card_filters: cardFilters,
          page,
          page_size: 20,
          search,
          status,
          kind,
          operator_id: operator ? Number(operator) : undefined,
        },
        signal
      )
  )
  return (
    <section className='min-w-0'>
      {props.accountType === 'opening' &&
        canMailbox(user, 'view') &&
        canMailbox(user, 'credentials') && (
          <CardFilters
            onApply={(values) => {
              setCardFilters(values)
              setPage(1)
            }}
          />
        )}
      <div className='flex flex-wrap gap-2 border-b py-3'>
        {canMailbox(user, 'view') &&
          canMailbox(user, 'review') &&
          canMailbox(user, 'credentials') && (
            <DataExportButton
              filters={{
                card_filters: cardFilters,
                account_type: props.accountType,
                search,
                status,
                kind,
                operator_id: operator ? Number(operator) : undefined,
              }}
            />
          )}
        <Input
          className='min-w-0 sm:max-w-64'
          aria-label={t('mailbox.admin.email')}
          placeholder={t('mailbox.admin.email')}
          value={search}
          onChange={(e) => {
            setSearch(e.target.value)
            setPage(1)
          }}
        />
        <NativeSelect
          aria-label={t('mailbox.admin.status')}
          value={status}
          onChange={(e) => {
            setStatus(e.target.value)
            setPage(1)
          }}
        >
          {['', 'pending', 'processed'].map((value) => (
            <option key={value} value={value}>
              {t(`mailbox.issues.${value || 'all'}`)}
            </option>
          ))}
        </NativeSelect>
        <NativeSelect
          aria-label={t('mailbox.issues.kind')}
          value={kind}
          onChange={(e) => {
            setKind(e.target.value)
            setPage(1)
          }}
        >
          {[
            '',
            'email_login',
            'otp',
            ...(props.accountType === 'opening' ? ['card'] : []),
            'other',
          ].map((value) => (
            <option key={value} value={value}>
              {t(`mailbox.issues.${value || 'all'}`)}
            </option>
          ))}
        </NativeSelect>
        <NativeSelect
          className='min-w-0 sm:max-w-64'
          aria-label={t('mailbox.admin.operator')}
          value={operator}
          disabled={operators.isPending || operators.isError}
          onChange={(e) => {
            setOperator(e.target.value)
            setPage(1)
          }}
        >
          <option value=''>{t('mailbox.admin.allOperators')}</option>
          {(operators.data ?? []).map((op) => (
            <option key={op.id} value={op.id}>
              {op.display_name || op.username}
            </option>
          ))}
        </NativeSelect>
        <Button
          variant='outline'
          size='icon'
          disabled={query.isFetching}
          aria-label={t('mailbox.admin.refresh')}
          title={t('mailbox.admin.refresh')}
          onClick={() => {
            void query.refetch()
            if (operators.isError) void operators.refetch()
          }}
        >
          <RefreshCw />
        </Button>
      </div>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
        empty={!query.data?.items.length}
      >
        <div className='divide-y'>
          {(query.data?.items ?? []).map((item) => (
            <article
              key={item.id}
              className='grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 py-4 md:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_150px_170px_40px]'
            >
              <div className='min-w-0'>
                <p className='text-sm font-medium break-all'>{item.email}</p>
                <p className='text-muted-foreground text-xs'>
                  {t(`mailbox.issues.${item.kind}`)}
                </p>
              </div>
              <p className='text-sm break-all'>{item.operator_name}</p>
              <p className='text-sm'>{t(`mailbox.issues.${item.status}`)}</p>
              <p className='text-muted-foreground text-xs'>
                <Time value={item.created_at} />
              </p>
              <Button
                variant='outline'
                size='icon'
                title={t('mailbox.admin.details')}
                aria-label={t('mailbox.admin.details')}
                onClick={() => setSelected(item.id)}
              >
                <Eye />
              </Button>
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
      {selected !== undefined && (
        <IssueDialog
          id={selected}
          accountType={props.accountType}
          onClose={() => setSelected(undefined)}
        />
      )}
    </section>
  )
}

export function IssueDialog(props: {
  id: number
  accountType: AccountType
  onClose: () => void
}) {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user)
  const query = useMailboxQuery(
    ['issue', props.id, props.accountType],
    (signal) => mailboxApi.issue(props.id, props.accountType, signal)
  )
  const [resolution, setResolution] = useState<'resume' | 'recall'>('resume')
  const [reply, setReply] = useState('')
  const [enabled, setEnabled] = useState({
    password: false,
    otp: false,
    card: false,
  })
  const [values, setValues] = useState({
    password: '',
    otp: '',
    card_number: '',
    card_expiry: '',
  })
  const replace = canMailbox(user, 'manage') && canMailbox(user, 'credentials')
  const dirty =
    !!reply ||
    Object.values(values).some(Boolean) ||
    Object.values(enabled).some(Boolean)
  const mutation = useMailboxMutation(async () => {
    const item = query.data
    if (!item?.assignment_active || item.status !== 'pending') return
    const credentials: ResolveIssueInput['credentials'] = {}
    if (enabled.password) credentials.password = values.password
    if (enabled.otp) credentials.otp = values.otp
    if (enabled.card) {
      credentials.card_number = values.card_number
      credentials.card_expiry = values.card_expiry
    }
    try {
      await mailboxApi.resolveIssue(props.id, props.accountType, {
        version: item.version,
        account_version: item.account_version,
        assignment_version: item.assignment_version,
        resolution,
        reply,
        ...(Object.keys(credentials).length ? { credentials } : {}),
      })
    } finally {
      setValues({ password: '', otp: '', card_number: '', card_expiry: '' })
      void query.refetch()
    }
  }, props.onClose)
  const item = query.data
  const editable =
    item?.status === 'pending' && item.assignment_active && !query.isError
  return (
    <Modal
      title={item?.email || t('mailbox.issues.title')}
      dirty={dirty}
      pending={mutation.isPending}
      onClose={props.onClose}
      footer={
        editable && (
          <Button
            type='submit'
            form='mailbox-issue-resolution'
            disabled={mutation.isPending}
          >
            {t('mailbox.issues.save')}
          </Button>
        )
      }
    >
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
      >
        {item && (
          <div className='space-y-4'>
            <div className='flex flex-wrap gap-3 text-sm'>
              <span>{t(`mailbox.issues.${item.kind}`)}</span>
              <span>{t(`mailbox.issues.${item.status}`)}</span>
              <Time value={item.created_at} />
            </div>
            <p className='text-sm [overflow-wrap:anywhere] whitespace-pre-wrap'>
              {item.description}
            </p>
            <div className='grid gap-3 sm:grid-cols-2'>
              {item.attachments.map((attachment, index) => (
                <PrivateImage
                  key={attachment.id}
                  attachment={attachment}
                  index={index}
                  accountType={props.accountType}
                />
              ))}
            </div>
            {item.reply && (
              <div className='border-t pt-3'>
                <h3 className='text-sm font-medium'>
                  {t('mailbox.issues.reply')}
                </h3>
                <p className='[overflow-wrap:anywhere] whitespace-pre-wrap'>
                  {item.reply}
                </p>
                <Time value={item.resolved_at} />
              </div>
            )}
            {item.status === 'invalidated' && (
              <p role='status'>{t('mailbox.issues.inactive')}</p>
            )}
            {editable && (
              <form
                id='mailbox-issue-resolution'
                className='grid gap-4 border-t pt-4'
                onSubmit={(e) => {
                  e.preventDefault()
                  if (window.confirm(t('mailbox.issues.confirm'))) {
                    mutation.submit(undefined)
                  }
                }}
              >
                <fieldset disabled={mutation.isPending} className='grid gap-4'>
                  <Field
                    id='issue-resolution'
                    label={t('mailbox.admin.decision')}
                  >
                    <NativeSelect
                      id='issue-resolution'
                      value={resolution}
                      onChange={(e) =>
                        setResolution(e.target.value as 'resume' | 'recall')
                      }
                    >
                      <option value='resume'>
                        {t('mailbox.issues.resume')}
                      </option>
                      {canMailbox(user, 'assign') && (
                        <option value='recall'>
                          {t('mailbox.issues.recall')}
                        </option>
                      )}
                    </NativeSelect>
                  </Field>
                  <Field id='issue-reply' label={t('mailbox.issues.reply')}>
                    <Textarea
                      id='issue-reply'
                      required
                      maxLength={2000}
                      value={reply}
                      onChange={(e) => setReply(e.target.value)}
                    />
                  </Field>
                  {replace && (
                    <section className='grid gap-3 border-t pt-3'>
                      <h3 className='text-sm font-medium'>
                        {t('mailbox.issues.replace')}
                      </h3>
                      <p className='text-muted-foreground text-xs'>
                        {t('mailbox.issues.replaceHint')}
                      </p>
                      {(
                        [
                          'password',
                          'otp',
                          ...(props.accountType === 'opening' ? ['card'] : []),
                        ] as const
                      ).map((key) => (
                        <div key={key} className='grid gap-2'>
                          <label className='flex gap-2 text-sm'>
                            <input
                              type='checkbox'
                              checked={enabled[key as keyof typeof enabled]}
                              onChange={(e) =>
                                setEnabled((old) => ({
                                  ...old,
                                  [key]: e.target.checked,
                                }))
                              }
                            />
                            {t(
                              key === 'card'
                                ? 'mailbox.admin.card'
                                : `mailbox.issues.${key === 'otp' ? 'otpSecret' : 'password'}`
                            )}
                          </label>
                          {enabled[key as keyof typeof enabled] &&
                            (key === 'card' ? (
                              <div className='grid gap-2 sm:grid-cols-2'>
                                <Field
                                  id='issue-card-number'
                                  label={t('mailbox.issues.cardNumber')}
                                >
                                  <Input
                                    id='issue-card-number'
                                    type='password'
                                    autoComplete='off'
                                    required
                                    value={values.card_number}
                                    onChange={(e) =>
                                      setValues((old) => ({
                                        ...old,
                                        card_number: e.target.value,
                                      }))
                                    }
                                  />
                                </Field>
                                <Field
                                  id='issue-expiry'
                                  label={t('mailbox.issues.cardExpiry')}
                                >
                                  <Input
                                    id='issue-expiry'
                                    required
                                    placeholder='MM/YY'
                                    value={values.card_expiry}
                                    onChange={(e) =>
                                      setValues((old) => ({
                                        ...old,
                                        card_expiry: e.target.value,
                                      }))
                                    }
                                  />
                                </Field>
                              </div>
                            ) : (
                              <Input
                                aria-label={t(
                                  `mailbox.issues.${key === 'otp' ? 'otpSecret' : 'password'}`
                                )}
                                type='password'
                                autoComplete='new-password'
                                required
                                value={values[key as 'password' | 'otp']}
                                onChange={(e) =>
                                  setValues((old) => ({
                                    ...old,
                                    [key]: e.target.value,
                                  }))
                                }
                              />
                            ))}
                        </div>
                      ))}
                    </section>
                  )}
                </fieldset>
              </form>
            )}
          </div>
        )}
      </QueryState>
    </Modal>
  )
}
