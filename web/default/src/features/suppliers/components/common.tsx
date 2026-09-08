import { ChevronLeft, ChevronRight, RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'
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

import { errorKey } from '../lib/errors'
import { timestampMs } from '../lib/schemas'
import type { Snapshot, Timestamp } from '../types'

export function Field(props: {
  id: string
  label: string
  error?: boolean
  children: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <div className='grid min-w-0 gap-2'>
      <Label htmlFor={props.id}>{props.label}</Label>
      {props.children}
      {props.error && (
        <p role='alert' className='text-destructive text-xs'>
          {t('supplier.required')}
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
}) {
  return (
    <Field id={props.id} label={props.label}>
      <NativeSelect
        id={props.id}
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
}) {
  const { t } = useTranslation()
  if (props.error) {
    return (
      <div
        role='alert'
        className='flex flex-wrap items-center gap-3 border-y py-5 text-sm'
      >
        <span>{t(errorKey(props.error))}</span>
        <Button variant='outline' onClick={props.retry}>
          {t('supplier.retry')}
        </Button>
      </div>
    )
  }
  if (props.pending) {
    return (
      <div
        role='status'
        className='text-muted-foreground animate-pulse py-12 text-center text-sm'
      >
        {t('supplier.loading')}
      </div>
    )
  }
  return props.children
}
export function Empty() {
  const { t } = useTranslation()
  return (
    <p className='text-muted-foreground py-12 text-center text-sm'>
      {t('supplier.empty')}
    </p>
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
  total: number
  size: number
  pending?: boolean
  onPage: (page: number) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='flex items-center justify-between gap-2 border-t pt-3 text-xs'>
      <span>
        {t('supplier.page', { page: props.page, total: props.total })}
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
          disabled={props.pending || props.page * props.size >= props.total}
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
}) {
  const { t } = useTranslation()
  return (
    <AlertDialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open && !props.pending) props.onClose()
      }}
    >
      <AlertDialogContent>
        <AlertDialogTitle>{props.title}</AlertDialogTitle>
        <AlertDialogDescription>{props.description}</AlertDialogDescription>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.pending}>
            {t('supplier.cancel')}
          </AlertDialogCancel>
          <Button
            variant='destructive'
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
