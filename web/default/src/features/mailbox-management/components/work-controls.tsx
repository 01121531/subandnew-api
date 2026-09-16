import { Check } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'

import {
  workMetrics,
  workMetricSelection,
  workMetricValue,
  workPeriods,
  workRangeParams,
  workRangeSchema,
  type WorkMetric,
  type WorkSelection,
} from '../lib/work'
import type {
  MailboxStatus,
  WorkPeriod,
  WorkRange,
  WorkRangeQuery,
  WorkSummary,
} from '../types'
import { Status, Time } from './common'

export function WorkTime(props: { value?: number }) {
  return <Time value={props.value} timeZone='Asia/Shanghai' />
}

export function WorkStatus(props: { value: MailboxStatus; review?: boolean }) {
  let labelKey: string | undefined
  if (props.value === 'rejected') labelKey = 'mailbox.work.returned'
  if (props.value === 'submitted') labelKey = 'mailbox.admin.awaitingReview'
  return (
    <Status value={props.value} review={props.review} labelKey={labelKey} />
  )
}

export function WorkDateFilter(props: {
  value: WorkRangeQuery
  onChange: (value: WorkRangeQuery) => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState({
    period: props.value.period ?? 'all',
    start_date: props.value.start_date ?? '',
    end_date: props.value.end_date ?? '',
  })
  const valid = workRangeSchema.safeParse(draft).success
  return (
    <div className='space-y-2 border-b py-3'>
      <form
        className='flex min-w-0 flex-wrap items-end gap-2'
        onSubmit={(event) => {
          event.preventDefault()
          if (valid) props.onChange(workRangeParams(draft))
        }}
      >
        <label className='grid min-w-0 gap-1 text-xs'>
          {t('mailbox.work.period')}
          <NativeSelect
            aria-label={t('mailbox.work.period')}
            value={draft.period}
            onChange={(event) => {
              const period = event.target.value as WorkPeriod
              setDraft((old) => ({ ...old, period }))
              if (period !== 'custom') props.onChange({ period })
            }}
          >
            {workPeriods.map((period) => (
              <option key={period} value={period}>
                {t(`mailbox.work.periods.${period}`)}
              </option>
            ))}
          </NativeSelect>
        </label>
        {draft.period === 'custom' && (
          <>
            <label className='grid min-w-0 flex-1 basis-36 gap-1 text-xs sm:max-w-44'>
              {t('mailbox.work.startDate')}
              <Input
                type='date'
                required
                aria-label={t('mailbox.work.startDate')}
                value={draft.start_date}
                onChange={(event) =>
                  setDraft((old) => ({
                    ...old,
                    start_date: event.target.value,
                  }))
                }
              />
            </label>
            <label className='grid min-w-0 flex-1 basis-36 gap-1 text-xs sm:max-w-44'>
              {t('mailbox.work.endDate')}
              <Input
                type='date'
                required
                aria-label={t('mailbox.work.endDate')}
                min={draft.start_date || undefined}
                value={draft.end_date}
                onChange={(event) =>
                  setDraft((old) => ({ ...old, end_date: event.target.value }))
                }
              />
            </label>
            <Button type='submit' variant='outline' disabled={!valid}>
              <Check />
              {t('mailbox.work.apply')}
            </Button>
          </>
        )}
      </form>
      {draft.period === 'custom' &&
        draft.start_date &&
        draft.end_date &&
        !valid && (
          <p role='alert' className='text-destructive text-xs'>
            {t('mailbox.errors.mailbox_invalid_work_query')}
          </p>
        )}
      <p className='text-muted-foreground text-xs'>
        {t('mailbox.work.appliedRange', {
          range:
            props.value.period === 'custom'
              ? `${props.value.start_date} - ${props.value.end_date}`
              : t(`mailbox.work.periods.${props.value.period ?? 'all'}`),
        })}
      </p>
    </div>
  )
}

export function WorkCount(props: {
  metric: WorkMetric
  operatorName: string
  summary?: WorkSummary
  onSelect: (selection: WorkSelection) => void
}) {
  const { t } = useTranslation()
  if (!props.summary) return <span>--</span>
  const count = workMetricValue(props.summary, props.metric)
  return (
    <button
      type='button'
      className='focus-visible:ring-ring min-h-8 min-w-8 rounded px-1 text-sm font-semibold tabular-nums underline-offset-4 hover:underline focus-visible:ring-2'
      aria-label={`${props.operatorName} · ${t(`mailbox.work.metrics.${props.metric}`)}: ${count}`}
      onClick={() => props.onSelect(workMetricSelection(props.metric))}
    >
      {count}
    </button>
  )
}

export function WorkSummaryView(props: {
  operatorName: string
  summary: WorkSummary
  range: WorkRange
  onSelect: (selection: WorkSelection) => void
}) {
  const { t, i18n } = useTranslation()
  const formatter = new Intl.DateTimeFormat(i18n.language, {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
  let range = t('mailbox.work.periods.all')
  if (props.range.start_at && props.range.end_at) {
    range = `${formatter.format(new Date(props.range.start_at * 1000))} - ${formatter.format(new Date((props.range.end_at - 1) * 1000))}`
  }
  return (
    <section className='space-y-3 border-b py-4'>
      <h3 className='text-sm font-semibold'>{t('mailbox.work.summary')}</h3>
      <p className='text-muted-foreground text-xs'>
        {range} · {props.range.timezone}
      </p>
      <dl className='grid grid-cols-2 gap-3 sm:grid-cols-3'>
        {workMetrics
          .filter((metric) => metric !== 'current_issue_pending')
          .map((metric) => (
            <div key={metric} className='min-w-0'>
              <dt className='text-muted-foreground text-xs'>
                {t(`mailbox.work.metrics.${metric}`)}
              </dt>
              <dd>
                <WorkCount
                  metric={metric}
                  operatorName={props.operatorName}
                  summary={props.summary}
                  onSelect={props.onSelect}
                />
              </dd>
            </div>
          ))}
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('mailbox.work.submissionCount')}
          </dt>
          <dd className='py-1.5 text-sm font-semibold tabular-nums'>
            {props.summary.submission_count}
          </dd>
        </div>
      </dl>
      <h3 className='text-sm font-semibold'>{t('mailbox.work.current')}</h3>
      <dl className='grid grid-cols-2 gap-3 sm:grid-cols-3'>
        {(
          [
            'pending',
            'submitted',
            'approved',
            'rejected',
            'issue_pending',
          ] as const
        ).map((status) => (
          <div key={status}>
            <dt className='text-muted-foreground text-xs'>
              {t(`mailbox.work.scopes.current_${status}`)}
            </dt>
            <dd>
              <button
                type='button'
                className='focus-visible:ring-ring min-h-8 min-w-8 rounded px-1 text-sm font-semibold tabular-nums hover:underline focus-visible:ring-2'
                aria-label={`${props.operatorName} · ${t(`mailbox.work.scopes.current_${status}`)}: ${props.summary.current[status]}`}
                onClick={() =>
                  props.onSelect({
                    scope: `current_${status}`,
                    account_type: 'all',
                  })
                }
              >
                {props.summary.current[status]}
              </button>
            </dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
