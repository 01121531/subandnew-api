import { useBlocker } from '@tanstack/react-router'
import {
  ChevronLeft,
  ChevronRight,
  Copy,
  Inbox,
  RefreshCw,
  X,
} from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'

import { statusLabelKey } from '../lib/display'
import { errorKey } from '../lib/errors'
import type { MailboxStatus } from '../types'

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
        <p role='alert' className='text-destructive text-xs'>
          {t(props.error, { defaultValue: t('mailbox.admin.invalidInput') })}
        </p>
      )}
    </div>
  )
}
export function Modal(props: {
  title: string
  description?: string
  children: ReactNode
  footer?: ReactNode
  dirty?: boolean
  pending?: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [discard, setDiscard] = useState(false)
  const blocker = useBlocker({
    shouldBlockFn: () => !!props.dirty || !!props.pending,
    enableBeforeUnload: !!props.dirty || !!props.pending,
    withResolver: true,
  })
  function close() {
    if (props.pending) return
    if (props.dirty) setDiscard(true)
    else props.onClose()
  }
  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) close()
        }}
      >
        <DialogContent
          showCloseButton={false}
          className='flex max-h-[90dvh] min-w-0 flex-col gap-0 overflow-hidden rounded-lg p-0 sm:max-w-2xl'
        >
          <header className='flex shrink-0 items-start justify-between gap-3 border-b p-4'>
            <div className='min-w-0 space-y-2'>
              <DialogTitle className='text-lg leading-snug break-all'>
                {props.title}
              </DialogTitle>
              {props.description && (
                <DialogDescription>{props.description}</DialogDescription>
              )}
            </div>
            <Button
              variant='ghost'
              size='icon'
              title={t('mailbox.admin.close')}
              aria-label={t('mailbox.admin.close')}
              disabled={props.pending}
              onClick={close}
            >
              <X />
            </Button>
          </header>
          <div className='min-h-0 overflow-y-auto p-4'>{props.children}</div>
          <footer className='bg-muted/30 flex shrink-0 flex-wrap justify-end gap-2 border-t p-4'>
            <Button variant='outline' disabled={props.pending} onClick={close}>
              {t('mailbox.admin.close')}
            </Button>
            {props.footer}
          </footer>
        </DialogContent>
      </Dialog>
      {discard && (
        <Confirm
          title={t('mailbox.admin.discardTitle')}
          description={t('mailbox.admin.discardBody')}
          pending={false}
          onClose={() => setDiscard(false)}
          onConfirm={props.onClose}
        />
      )}
      {blocker.status === 'blocked' && (
        <Confirm
          title={t('mailbox.admin.discardTitle')}
          description={t('mailbox.admin.discardBody')}
          pending={!!props.pending}
          onClose={() => blocker.reset?.()}
          onConfirm={() => blocker.proceed?.()}
        />
      )}
    </>
  )
}
export function Confirm(props: {
  title: string
  description: string
  pending: boolean
  onClose: () => void
  onConfirm: () => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !props.pending) props.onClose()
      }}
    >
      <DialogContent showCloseButton={false} className='rounded-lg'>
        <DialogTitle>{props.title}</DialogTitle>
        <DialogDescription>{props.description}</DialogDescription>
        <div className='flex justify-end gap-2'>
          <Button
            variant='outline'
            disabled={props.pending}
            onClick={props.onClose}
          >
            {t('mailbox.admin.cancel')}
          </Button>
          <Button
            variant='destructive'
            disabled={props.pending}
            onClick={props.onConfirm}
          >
            {t('mailbox.admin.confirm')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
export function QueryState(props: {
  pending: boolean
  error: unknown
  retry: () => void
  empty?: boolean
  children: ReactNode
}) {
  const { t } = useTranslation()
  if (props.error) {
    return (
      <div
        role='alert'
        className='text-destructive flex flex-wrap items-center gap-3 py-8'
      >
        <span>
          {t(errorKey(props.error), {
            defaultValue: t('mailbox.errors.mailbox_request_failed'),
          })}
        </span>
        <Button variant='outline' onClick={props.retry}>
          <RefreshCw />
          {t('mailbox.admin.retry')}
        </Button>
      </div>
    )
  }
  if (props.pending) {
    return (
      <div role='status' className='text-muted-foreground py-12 text-center'>
        {t('mailbox.admin.loading')}
      </div>
    )
  }
  if (props.empty) {
    return (
      <div
        role='status'
        className='text-muted-foreground grid justify-items-center gap-3 py-16'
      >
        <Inbox className='size-8' />
        <span>{t('mailbox.admin.empty')}</span>
      </div>
    )
  }
  return props.children
}
export function Pager(props: {
  page: number
  total?: number
  hasMore?: boolean
  pending: boolean
  onPage: (page: number) => void
}) {
  const { t } = useTranslation()
  return (
    <footer className='flex flex-wrap items-center justify-between gap-2 border-t py-3 text-sm'>
      <span>
        {t('mailbox.admin.pagination', {
          page: props.page,
          total: props.total ?? 0,
        })}
      </span>
      <div className='flex gap-2'>
        <Button
          variant='outline'
          size='icon'
          title={t('mailbox.admin.previous')}
          aria-label={t('mailbox.admin.previous')}
          disabled={props.pending || props.page <= 1}
          onClick={() => props.onPage(props.page - 1)}
        >
          <ChevronLeft />
        </Button>
        <Button
          variant='outline'
          size='icon'
          title={t('mailbox.admin.next')}
          aria-label={t('mailbox.admin.next')}
          disabled={props.pending || !props.hasMore}
          onClick={() => props.onPage(props.page + 1)}
        >
          <ChevronRight />
        </Button>
      </div>
    </footer>
  )
}
export function SearchBar(props: {
  value: string
  onChange: (value: string) => void
  status?: string
  onStatus?: (value: string) => void
  review?: boolean
  refresh: () => void
  pending: boolean
  children?: ReactNode
}) {
  const { t } = useTranslation()
  const statuses = props.review
    ? ['pending', 'approved', 'rejected']
    : ['unassigned', 'pending', 'submitted', 'approved', 'rejected']
  return (
    <div className='flex flex-wrap items-center gap-2 py-4'>
      <Input
        className='min-w-0 flex-1 basis-48 sm:max-w-80'
        aria-label={t('mailbox.admin.search')}
        placeholder={t('mailbox.admin.search')}
        value={props.value}
        onChange={(event) => props.onChange(event.target.value)}
      />
      {props.onStatus && (
        <NativeSelect
          aria-label={t('mailbox.admin.status')}
          value={props.status}
          onChange={(event) => props.onStatus?.(event.target.value)}
        >
          <option value=''>{t('mailbox.admin.allStatuses')}</option>
          {statuses.map((status) => (
            <option key={status} value={status}>
              {t(statusLabelKey(status, props.review))}
            </option>
          ))}
        </NativeSelect>
      )}
      <Button
        variant='outline'
        size='icon'
        title={t('mailbox.admin.refresh')}
        aria-label={t('mailbox.admin.refresh')}
        disabled={props.pending}
        onClick={props.refresh}
      >
        <RefreshCw />
      </Button>
      {props.children}
    </div>
  )
}
export function Status(props: {
  value: MailboxStatus | 'pending'
  review?: boolean
}) {
  const { t } = useTranslation()
  const colors: Record<string, string> = {
    approved:
      'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
    rejected: 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-400',
    submitted:
      'border-blue-500/30 bg-blue-500/10 text-blue-700 dark:text-blue-400',
    pending:
      'border-amber-500/30 bg-amber-500/10 text-amber-800 dark:text-amber-400',
  }
  return (
    <Badge variant='outline' className={colors[props.value]}>
      {t(statusLabelKey(props.value, props.review))}
    </Badge>
  )
}
export function Time(props: { value?: number }) {
  const { i18n } = useTranslation()
  return props.value ? (
    <time dateTime={new Date(props.value * 1000).toISOString()}>
      {new Date(props.value * 1000).toLocaleString(
        i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US'
      )}
    </time>
  ) : (
    <span>--</span>
  )
}
export function CopyButton(props: { value: string; disabled?: boolean }) {
  const { t } = useTranslation()
  return (
    <Button
      variant='outline'
      size='icon'
      disabled={props.disabled}
      aria-label={t('mailbox.admin.copy')}
      title={t('mailbox.admin.copy')}
      onClick={() => {
        void navigator.clipboard.writeText(props.value).then(
          () => toast.success(t('mailbox.admin.copied')),
          () => toast.error(t('mailbox.admin.copyFailed'))
        )
      }}
    >
      <Copy />
    </Button>
  )
}
