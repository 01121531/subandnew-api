import { ChevronLeft, ChevronRight, Inbox, RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { toIntlLocale } from '@/i18n/languages'

import { useRetryCooldown } from '../hooks/use-retry-cooldown'
import { errorKey } from '../lib/errors'
import type { Page } from '../types'

export function Field(props: {
  id: string
  label: string
  error?: string
  children: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <div className='grid min-w-0 gap-2'>
      <Label htmlFor={props.id}>{props.label}</Label>
      {props.children}
      {props.error && (
        <p
          id={`${props.id}-error`}
          role='alert'
          className='text-destructive text-xs'
        >
          {t(props.error)}
        </p>
      )}
    </div>
  )
}

export function ErrorMessage(props: { error: unknown }) {
  const { t } = useTranslation()
  if (!props.error) return null
  return (
    <p
      role='alert'
      className='text-destructive text-sm [overflow-wrap:anywhere]'
    >
      {t(errorKey(props.error))}
    </p>
  )
}

export function QueryState(props: {
  pending: boolean
  error: unknown
  retry: () => void
  children?: ReactNode
  preserveData?: boolean
}) {
  const { t } = useTranslation()
  const cooldown = useRetryCooldown(props.error)
  if (props.error) {
    return (
      <>
        <div className='grid justify-items-start gap-3 py-6'>
          <ErrorMessage error={props.error} />
          {props.preserveData && (
            <p role='status'>{t('mailboxPortal.refreshFailed')}</p>
          )}
          <Button
            variant='outline'
            disabled={cooldown > 0}
            onClick={props.retry}
          >
            <RefreshCw />
            {cooldown > 0
              ? t('mailboxPortal.retryCooldown', { count: cooldown })
              : t('mailboxPortal.retry')}
          </Button>
        </div>
        {props.preserveData && props.children}
      </>
    )
  }
  if (props.pending) {
    return (
      <p role='status' className='text-muted-foreground py-10 text-sm'>
        {t('mailboxPortal.loading')}
      </p>
    )
  }
  return props.children
}

export function Empty() {
  const { t } = useTranslation()
  return (
    <div
      role='status'
      className='text-muted-foreground grid min-h-40 place-content-center justify-items-center gap-3 text-sm'
    >
      <Inbox aria-hidden='true' className='size-7' />
      {t('mailboxPortal.empty')}
    </div>
  )
}

export function CredentialRetry(props: { error: unknown; retry: () => void }) {
  const { t } = useTranslation()
  const cooldown = useRetryCooldown(props.error)
  return (
    <Button
      size='sm'
      variant='ghost'
      disabled={cooldown > 0}
      onClick={props.retry}
    >
      <RefreshCw />
      {cooldown > 0
        ? t('mailboxPortal.retryCooldown', { count: cooldown })
        : t('mailboxPortal.retry')}
    </Button>
  )
}

export function Status(props: { status: string; submission?: boolean }) {
  const { t } = useTranslation()
  const colors: Record<string, string> = {
    pending:
      'text-amber-800 bg-amber-50 dark:text-amber-300 dark:bg-amber-950/50',
    submitted: 'text-sky-800 bg-sky-50 dark:text-sky-300 dark:bg-sky-950/50',
    approved:
      'text-emerald-800 bg-emerald-50 dark:text-emerald-300 dark:bg-emerald-950/50',
    rejected: 'text-red-800 bg-red-50 dark:text-red-300 dark:bg-red-950/50',
  }
  return (
    <span
      className={`inline-flex shrink-0 items-center rounded px-2 py-1 text-xs font-medium ${colors[props.status] ?? 'bg-muted text-muted-foreground'}`}
    >
      {t(
        props.submission && props.status === 'pending'
          ? 'mailboxPortal.submissionPending'
          : `mailboxPortal.status_${props.status}`
      )}
    </span>
  )
}

export function Time(props: { value?: number }) {
  const { i18n } = useTranslation()
  if (!props.value) return <span>--</span>
  return (
    <time dateTime={new Date(props.value * 1000).toISOString()}>
      {new Date(props.value * 1000).toLocaleString(toIntlLocale(i18n.language))}
    </time>
  )
}

export function Pagination(props: {
  data?: Page<unknown>
  page: number
  pending: boolean
  onPage: (page: number) => void
}) {
  const { t } = useTranslation()
  return (
    <footer className='flex flex-wrap items-center justify-between gap-3 border-t py-4 text-xs'>
      <span>
        {t('mailboxPortal.pagination', {
          page: props.page,
          total: props.data?.total ?? 0,
        })}
      </span>
      <div className='flex gap-2'>
        <Button
          variant='outline'
          size='icon'
          disabled={props.pending || props.page <= 1}
          title={t('mailboxPortal.previous')}
          aria-label={t('mailboxPortal.previous')}
          onClick={() => props.onPage(props.page - 1)}
        >
          <ChevronLeft />
        </Button>
        <Button
          variant='outline'
          size='icon'
          disabled={props.pending || !props.data?.has_more}
          title={t('mailboxPortal.next')}
          aria-label={t('mailboxPortal.next')}
          onClick={() => props.onPage(props.page + 1)}
        >
          <ChevronRight />
        </Button>
      </div>
    </footer>
  )
}
