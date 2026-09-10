import { Activity, LoaderCircle, Trash2 } from 'lucide-react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

import { proxyEnabled } from '../lib/display'
import type { Proxy } from '../types'

export function ProxyOwnership(props: { proxy: Proxy }) {
  const { t } = useTranslation()
  let label = t('supplier.ownershipUnknown')
  if (props.proxy.ownership === 'owned') label = t('supplier.owner')
  if (props.proxy.ownership === 'assigned') label = t('supplier.assigned')
  if (props.proxy.ownership === 'conflict') {
    label = t('supplier.ownershipConflict')
  }
  return (
    <Badge
      variant='outline'
      className='h-auto min-h-5 max-w-full rounded-sm [overflow-wrap:anywhere] whitespace-normal'
    >
      {label}
    </Badge>
  )
}

export function ProxyControls(props: {
  proxy: Proxy
  pending: boolean
  testing: boolean
  testingCurrent?: boolean
  pendingAction?: 'status' | 'delete'
  onTest: () => void
  onStatus: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const reasonId = useId()
  const allowed = props.proxy.can_update_status === true
  let reason = t('supplier.proxyOwnershipUnknown')
  if (props.proxy.status_update_reason === 'proxy_not_owned') {
    reason = t('supplier.proxyNotOwned')
  }
  if (props.proxy.status_update_reason === 'proxy_ownership_conflict') {
    reason = t('supplier.proxyOwnershipConflict')
  }
  return (
    <div className='supplier-portal grid min-w-0 gap-2'>
      <div className='flex flex-wrap items-center gap-3'>
        <label className='flex items-center gap-2 text-sm'>
          <Switch
            checked={proxyEnabled(props.proxy.status)}
            disabled={!allowed || props.pending}
            aria-label={t('supplier.proxyStatus')}
            aria-describedby={!allowed ? reasonId : undefined}
            aria-busy={props.pendingAction === 'status'}
            onCheckedChange={props.onStatus}
          />
          {t(
            proxyEnabled(props.proxy.status)
              ? 'supplier.enabled'
              : 'supplier.disabled'
          )}
          <span className='inline-flex size-4 shrink-0' aria-hidden='true'>
            {props.pendingAction === 'status' && (
              <LoaderCircle className='size-4 animate-spin motion-reduce:animate-none' />
            )}
          </span>
        </label>
        <Button
          variant='ghost'
          size='icon'
          disabled={props.testing}
          aria-busy={props.testingCurrent}
          aria-label={t('supplier.test')}
          title={t('supplier.test')}
          onClick={props.onTest}
        >
          {props.testingCurrent ? (
            <LoaderCircle
              className='animate-spin motion-reduce:animate-none'
              aria-hidden='true'
            />
          ) : (
            <Activity aria-hidden='true' />
          )}
        </Button>
        <Button
          variant='ghost'
          size='icon'
          className='text-destructive'
          disabled={props.pending}
          aria-busy={props.pendingAction === 'delete'}
          aria-label={t('supplier.delete')}
          title={t('supplier.delete')}
          onClick={props.onDelete}
        >
          {props.pendingAction === 'delete' ? (
            <LoaderCircle
              className='animate-spin motion-reduce:animate-none'
              aria-hidden='true'
            />
          ) : (
            <Trash2 aria-hidden='true' />
          )}
        </Button>
      </div>
      {!allowed && (
        <p
          id={reasonId}
          className='text-muted-foreground max-w-64 text-xs break-words whitespace-normal'
        >
          {reason}
        </p>
      )}
      <span className='sr-only' role='status'>
        {(props.testingCurrent || props.pendingAction) && t('supplier.loading')}
      </span>
    </div>
  )
}
