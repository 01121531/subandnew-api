import {
  AlertCircle,
  ChevronLeft,
  ChevronRight,
  Inbox,
  RefreshCw,
} from 'lucide-react'
import type { ReactNode, Ref } from 'react'
import { useTranslation } from 'react-i18next'

import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'

import { canRetainQueryData, errorKey } from '../lib/errors'
import { timestampMs } from '../lib/schemas'
import type { Snapshot, Timestamp } from '../types'

export function Field(props: {
  id: string
  label: string
  error?: ReactNode
  children: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <div
      className='supplier-field grid min-w-0 gap-2'
      role='group'
      aria-labelledby={`${props.id}-label`}
      aria-describedby={props.error ? `${props.id}-error` : undefined}
      data-invalid={!!props.error}
    >
      <Label id={`${props.id}-label`} htmlFor={props.id}>
        {props.label}
      </Label>
      {props.children}
      {props.error && (
        <p
          id={`${props.id}-error`}
          role='alert'
          className='text-destructive text-xs'
        >
          {props.error === true ? t('supplier.required') : props.error}
        </p>
      )}
    </div>
  )
}
export function SelectField(props: {
  id: string
  label: string
  value: string | number
  onChange: (value: string) => void
  children: ReactNode
  disabled?: boolean
  error?: ReactNode
  inputRef?: Ref<HTMLSelectElement>
}) {
  return (
    <Field id={props.id} label={props.label} error={props.error}>
      <NativeSelect
        id={props.id}
        ref={props.inputRef}
        aria-invalid={!!props.error}
        aria-describedby={props.error ? `${props.id}-error` : undefined}
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
        disabled={props.disabled}
        className='w-full'
      >
        {props.children}
      </NativeSelect>
    </Field>
  )
}
export function QueryState(props: {
  pending: boolean
  error: unknown
  retry: () => void
  children?: ReactNode
  hasData?: boolean
}) {
  const { t } = useTranslation()
  if (props.error) {
    return (
      <>
        <div
          role='alert'
          className='flex flex-wrap items-center gap-3 border-y border-amber-500/30 bg-amber-500/5 px-3 py-3 text-sm'
        >
          <AlertCircle className='size-4 shrink-0 text-amber-600 dark:text-amber-400' />
          <span className='min-w-0 flex-1 break-words'>
            {t(errorKey(props.error))}
          </span>
          <Button
            variant='outline'
            disabled={props.pending}
            onClick={props.retry}
          >
            <RefreshCw className='size-4' />
            {t('supplier.retry')}
          </Button>
        </div>
        {props.hasData && canRetainQueryData(props.error) && props.children}
      </>
    )
  }
  if (props.pending) {
    return (
      <div
        role='status'
        aria-label={t('supplier.loading')}
        aria-busy='true'
        className='grid min-h-48 content-start gap-4 py-5'
      >
        <span className='sr-only'>{t('supplier.loading')}</span>
        {[0, 1, 2, 3].map((row) => (
          <div
            key={row}
            aria-hidden='true'
            className='bg-muted h-7 w-full animate-pulse rounded motion-reduce:animate-none'
          />
        ))}
      </div>
    )
  }
  return props.children
}
export function Empty(props: { message?: string; action?: ReactNode }) {
  const { t } = useTranslation()
  return (
    <div
      role='status'
      className='text-muted-foreground grid min-h-48 content-center justify-items-center gap-3 py-8 text-center text-sm'
    >
      <Inbox aria-hidden='true' className='size-7 opacity-60' />
      <p>{props.message ?? t('supplier.empty')}</p>
      {props.action}
    </div>
  )
}
export function Time(props: { value?: Timestamp }) {
  const { i18n } = useTranslation()
  const date = props.value === undefined ? Number.NaN : timestampMs(props.value)
  return Number.isFinite(date)
    ? new Date(date).toLocaleString(
        i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US',
        { timeZone: 'Asia/Shanghai' }
      )
    : '--'
}
export function Freshness(props: {
  data?: Snapshot
  pending: boolean
  refresh: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs'>
      {props.data?.stale && (
        <span role='status' className='text-amber-700 dark:text-amber-400'>
          {t('supplier.stale')}
        </span>
      )}
      {props.data && (
        <span>
          {t('supplier.observed')}: <Time value={props.data.observed_at} />
        </span>
      )}
      <Button
        variant='ghost'
        size='icon'
        title={t('supplier.refresh')}
        aria-label={t('supplier.refresh')}
        disabled={props.pending}
        onClick={props.refresh}
      >
        <RefreshCw className={props.pending ? 'animate-spin' : ''} />
      </Button>
    </div>
  )
}
export function Pagination(props: {
  page: number
  total?: number
  hasMore?: boolean
  size: number
  pending?: boolean
  onPage: (page: number) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap items-center justify-between gap-2 border-t pt-3 text-xs'>
      <span>
        {props.total === undefined
          ? t('supplier.pageOnly', { page: props.page })
          : t('supplier.ui_pagination', {
              page: props.page,
              pages: Math.max(1, Math.ceil(props.total / props.size)),
              total: props.total,
            })}
      </span>
      <div className='flex gap-1'>
        <Button
          variant='outline'
          size='icon'
          title={t('supplier.previous')}
          aria-label={t('supplier.previous')}
          disabled={props.pending || props.page <= 1}
          onClick={() => props.onPage(props.page - 1)}
        >
          <ChevronLeft />
        </Button>
        <Button
          variant='outline'
          size='icon'
          title={t('supplier.next')}
          aria-label={t('supplier.next')}
          disabled={
            props.pending ||
            (props.total === undefined
              ? !props.hasMore
              : props.page * props.size >= props.total)
          }
          onClick={() => props.onPage(props.page + 1)}
        >
          <ChevronRight />
        </Button>
      </div>
    </div>
  )
}
export function Confirm(props: {
  open: boolean
  title: string
  description: string
  pending: boolean
  onClose: () => void
  onConfirm: () => void
  destructive?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  return (
    <AlertDialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open && !props.pending) props.onClose()
      }}
    >
      <AlertDialogContent
        className={`supplier-portal ${props.className ?? ''}`}
      >
        <AlertDialogTitle>{props.title}</AlertDialogTitle>
        <AlertDialogDescription>{props.description}</AlertDialogDescription>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.pending}>
            {t('supplier.cancel')}
          </AlertDialogCancel>
          <Button
            variant={props.destructive === false ? 'default' : 'destructive'}
            disabled={props.pending}
            onClick={props.onConfirm}
          >
            {t('supplier.confirm')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
