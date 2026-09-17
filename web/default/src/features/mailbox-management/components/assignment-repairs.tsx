import { Wrench } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { useAuthStore } from '@/stores/auth-store'

import { mailboxApi } from '../api'
import { useMailboxMutation, useMailboxQuery } from '../hooks'
import { repairLimit } from '../lib/assignment-repairs'
import { errorKey } from '../lib/errors'
import { canMailboxRepair } from '../lib/permissions'
import type { AccountType, RepairCandidate } from '../types'
import {
  Confirm,
  Field,
  Modal,
  Pager,
  QueryState,
  SearchBar,
  Status,
  Time,
} from './common'
import { PoolScope } from './pool-scope'

export function RepairFacts({ item }: { item: RepairCandidate }) {
  const { t } = useTranslation()
  return (
    <dl className='grid min-w-0 gap-3 text-xs sm:grid-cols-3'>
      <div className='min-w-0 space-y-1 break-words'>
        <dt className='text-muted-foreground'>
          {t('mailbox.repairs.original')}
        </dt>
        <dd className='break-all'>
          {item.original_operator_name} (#{item.original_operator_id})
        </dd>
        <dd>#{item.original_assignment_id}</dd>
        <dd>
          {t('mailbox.work.revokedAt')}:{' '}
          <Time value={item.original_revoked_at} />
        </dd>
      </div>
      <div className='min-w-0 space-y-1 break-words'>
        <dt className='text-muted-foreground'>
          {t('mailbox.repairs.submission')}
        </dt>
        <dd>
          #{item.submission_id} <Status value={item.submission_status} review />
        </dd>
        <dd>
          {t('mailbox.admin.submittedAt')}: <Time value={item.submitted_at} />
        </dd>
        <dd>
          {t('mailbox.repairs.reviewedAt')}: <Time value={item.reviewed_at} />
        </dd>
        <dd className='break-all whitespace-pre-wrap'>{item.review_reason}</dd>
      </div>
      <div className='min-w-0 space-y-1 break-words'>
        <dt className='text-muted-foreground'>
          {t('mailbox.repairs.current')}
        </dt>
        <dd className='break-all'>{item.current_operator_name || '--'}</dd>
        <dd>
          {item.current_assignment_id ? (
            <>
              #{item.current_assignment_id}{' '}
              <Status value={item.current_status} />
            </>
          ) : (
            '--'
          )}
        </dd>
        <dd>
          {t('mailbox.admin.assignedAt')}:{' '}
          <Time value={item.current_assigned_at} />
        </dd>
      </div>
      {!item.can_repair && (
        <div role='status' className='text-destructive sm:col-span-3'>
          {t(`mailbox.errors.${item.conflict_code}`, {
            defaultValue: t('mailbox.repairs.conflict'),
          })}
        </div>
      )}
      <div className='min-w-0 sm:col-span-3'>
        <details>
          <summary className='cursor-pointer py-2 font-medium'>
            {t('mailbox.repairs.laterAssignments', {
              count: item.later_assignments?.length ?? 0,
            })}
          </summary>
          <ol className='divide-y border-l pl-3'>
            {(item.later_assignments ?? []).map((assignment) => (
              <li key={assignment.id} className='space-y-1 py-2 break-words'>
                <p className='break-all'>
                  #{assignment.id} {assignment.operator_name} (#
                  {assignment.operator_id})
                </p>
                <p>
                  {t(
                    assignment.revoked_at
                      ? 'mailbox.work.inactive'
                      : 'mailbox.work.active'
                  )}
                </p>
                <p>
                  {t('mailbox.work.assignmentStatus')}:{' '}
                  <Status value={assignment.status} />
                </p>
                <p>
                  {t('mailbox.admin.assignedAt')}:{' '}
                  <Time value={assignment.assigned_at} />
                </p>
                <p>
                  {t('mailbox.work.revokedAt')}:{' '}
                  <Time value={assignment.revoked_at} />
                </p>
              </li>
            ))}
          </ol>
        </details>
      </div>
    </dl>
  )
}

function RepairDialog(props: {
  accountType: AccountType
  items: RepairCandidate[]
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const [reason, setReason] = useState('')
  const [confirm, setConfirm] = useState(false)
  const mutation = useMailboxMutation(
    async () => {
      try {
        await mailboxApi.repairAssignments({
          account_type: props.accountType,
          reason,
          items: props.items,
        })
      } finally {
        setConfirm(false)
      }
    },
    () => {
      toast.success(t('mailbox.repairs.success'))
      props.onSuccess()
    }
  )
  const valid =
    reason.trim().length > 0 &&
    reason.trim().length <= 2000 &&
    props.items.length > 0 &&
    props.items.length <= repairLimit &&
    props.items.every((item) => item.can_repair)
  return (
    <>
      <Modal
        title={t('mailbox.repairs.title')}
        description={t('mailbox.repairs.confirm', {
          count: props.items.length,
        })}
        dirty={!!reason}
        pending={mutation.isPending}
        onClose={props.onClose}
        footer={
          <Button
            disabled={!valid || mutation.isPending || mutation.isError}
            onClick={() => setConfirm(true)}
          >
            <Wrench />
            {t('mailbox.repairs.apply')}
          </Button>
        }
      >
        <div className='space-y-4'>
          <p className='text-sm font-medium'>
            {t(`mailbox.admin.pools.${props.accountType}`)}
          </p>
          <ul className='max-h-48 list-inside list-disc overflow-y-auto text-sm'>
            {props.items.map((item) => (
              <li key={item.account_id} className='break-all'>
                {item.email} (#{item.account_id})
              </li>
            ))}
          </ul>
          <Field
            id='mailbox-repair-reason'
            label={t('mailbox.changeStatus.reason')}
          >
            <Textarea
              id='mailbox-repair-reason'
              required
              maxLength={2000}
              value={reason}
              disabled={mutation.isPending}
              onChange={(event) => setReason(event.target.value)}
            />
          </Field>
          {mutation.error && (
            <p role='alert' className='text-destructive text-sm'>
              {t(errorKey(mutation.error), {
                defaultValue: t('mailbox.repairs.conflict'),
              })}{' '}
              {t('mailbox.repairs.reload')}
            </p>
          )}
        </div>
      </Modal>
      {confirm && (
        <Confirm
          title={t('mailbox.repairs.apply')}
          description={t('mailbox.repairs.confirm', {
            count: props.items.length,
          })}
          pending={mutation.isPending}
          onClose={() => setConfirm(false)}
          onConfirm={() => {
            if (valid) mutation.submit(undefined)
          }}
        />
      )}
    </>
  )
}

export function AssignmentRepairs() {
  const user = useAuthStore((state) => state.auth.user)
  if (!canMailboxRepair(user)) return null
  return (
    <PoolScope>
      {(accountType) => (
        <PoolRepairs key={accountType} accountType={accountType} />
      )}
    </PoolScope>
  )
}

function PoolRepairs({ accountType }: { accountType: AccountType }) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [operator, setOperator] = useState('')
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<Map<number, RepairCandidate>>(
    new Map()
  )
  const [repair, setRepair] = useState<RepairCandidate[]>()
  const operatorValid =
    !operator ||
    (/^[1-9]\d*$/.test(operator) && Number.isSafeInteger(Number(operator)))
  const params = {
    account_type: accountType,
    search,
    operator_id: operator ? Number(operator) : undefined,
    page,
    page_size: 20,
  }
  const query = useMailboxQuery(
    ['assignment-repairs', params],
    (signal) => mailboxApi.assignmentRepairs(params, signal),
    operatorValid
  )
  const rows = query.data?.items ?? []
  const eligible = rows.filter((item) => item.can_repair)
  const allSelected =
    eligible.length > 0 &&
    eligible.every((item) => selected.has(item.account_id))
  function toggle(items: RepairCandidate[], checked: boolean) {
    setSelected((old) => {
      const next = new Map(old)
      for (const item of items) {
        if (!checked) next.delete(item.account_id)
        else if (
          item.can_repair &&
          (next.has(item.account_id) || next.size < repairLimit)
        ) {
          next.set(item.account_id, item)
        }
      }
      return next
    })
  }
  function refresh() {
    setSelected(new Map())
    void query.refetch()
  }
  return (
    <section className='min-w-0'>
      <SearchBar
        value={search}
        onChange={(value) => {
          setSearch(value)
          setPage(1)
          setSelected(new Map())
        }}
        pending={query.isFetching}
        refresh={refresh}
      >
        <Input
          type='number'
          min={1}
          step={1}
          className='w-full sm:w-56'
          value={operator}
          aria-label={t('mailbox.repairs.operator')}
          placeholder={t('mailbox.repairs.operator')}
          onChange={(event) => {
            setOperator(event.target.value)
            setPage(1)
            setSelected(new Map())
          }}
        />
      </SearchBar>
      {!operatorValid && <p role='alert'>{t('mailbox.admin.invalidInput')}</p>}
      <div className='flex flex-wrap items-center gap-3 border-b pb-3 text-sm'>
        <label className='flex items-center gap-2'>
          <input
            type='checkbox'
            checked={allSelected}
            disabled={
              !eligible.length ||
              query.isFetching ||
              !operatorValid ||
              (!allSelected &&
                selected.size +
                  eligible.filter((item) => !selected.has(item.account_id))
                    .length >
                  repairLimit)
            }
            onChange={(event) => toggle(eligible, event.target.checked)}
          />
          {t('mailbox.repairs.selectPage')}
        </label>
        <span>
          {t('mailbox.admin.selected', { count: selected.size })} /{' '}
          {repairLimit}
        </span>
        <Button
          variant='outline'
          disabled={!selected.size}
          onClick={() => setSelected(new Map())}
        >
          {t('mailbox.repairs.clear')}
        </Button>
        <Button
          disabled={
            !selected.size ||
            query.isFetching ||
            query.isError ||
            !operatorValid
          }
          onClick={() => setRepair([...selected.values()])}
        >
          <Wrench />
          {t('mailbox.repairs.apply')}
        </Button>
      </div>
      {operatorValid && (
        <QueryState
          pending={query.isPending}
          error={query.error}
          retry={refresh}
          empty={!rows.length}
        >
          <div className='divide-y'>
            {rows.map((item) => (
              <article key={item.account_id} className='min-w-0 space-y-3 py-4'>
                <div className='flex items-start justify-between gap-3'>
                  <label className='flex min-w-0 items-start gap-2 text-sm font-medium'>
                    <input
                      type='checkbox'
                      className='mt-1 shrink-0'
                      checked={selected.has(item.account_id)}
                      disabled={
                        !item.can_repair ||
                        query.isFetching ||
                        (!selected.has(item.account_id) &&
                          selected.size >= repairLimit)
                      }
                      onChange={(event) => toggle([item], event.target.checked)}
                    />
                    <span className='break-all'>
                      {item.email} (#{item.account_id})
                    </span>
                  </label>
                  <Button
                    variant='outline'
                    size='icon'
                    title={t('mailbox.repairs.apply')}
                    aria-label={`${t('mailbox.repairs.apply')}: ${item.email}`}
                    disabled={!item.can_repair || query.isFetching}
                    onClick={() => setRepair([item])}
                  >
                    <Wrench />
                  </Button>
                </div>
                <RepairFacts item={item} />
              </article>
            ))}
          </div>
        </QueryState>
      )}
      <Pager
        page={page}
        total={query.data?.total}
        hasMore={query.data?.has_more}
        pending={query.isFetching || !operatorValid}
        onPage={setPage}
      />
      {repair && (
        <RepairDialog
          accountType={accountType}
          items={repair}
          onClose={() => {
            setRepair(undefined)
            refresh()
          }}
          onSuccess={() => {
            setRepair(undefined)
            refresh()
          }}
        />
      )}
    </section>
  )
}
