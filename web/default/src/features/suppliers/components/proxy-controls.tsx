import { Activity, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

import { proxyEnabled } from '../lib/display'
import type { Proxy } from '../types'

export function ProxyOwnership(props: { proxy: Proxy }) {
  const { t } = useTranslation()
  let label = t('supplier.ownershipUnknown')
  if (props.proxy.is_owner === true) label = t('supplier.owner')
  if (props.proxy.is_owner === false) label = t('supplier.shared')
  return <Badge variant='outline'>{label}</Badge>
}

export function ProxyControls(props: {
  proxy: Proxy
  pending: boolean
  testing: boolean
  onTest: () => void
  onStatus: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const allowed = props.proxy.can_update_status === true
  const reason =
    props.proxy.status_update_reason === 'proxy_not_owned'
      ? t('supplier.proxyNotOwned')
      : t('supplier.proxyOwnershipUnknown')
  return (
    <div className='grid min-w-0 gap-2'>
      <div className='flex flex-wrap items-center gap-3'>
        <label className='flex items-center gap-2 text-sm'>
          <Switch
            checked={proxyEnabled(props.proxy.status)}
            disabled={!allowed || props.pending}
            aria-label={t('supplier.proxyStatus')}
            onCheckedChange={props.onStatus}
          />
          {t(
            proxyEnabled(props.proxy.status)
              ? 'supplier.enabled'
              : 'supplier.disabled'
          )}
        </label>
        <Button
          variant='ghost'
          size='icon'
          disabled={props.testing}
          aria-label={t('supplier.test')}
          title={t('supplier.test')}
          onClick={props.onTest}
        >
          <Activity />
        </Button>
        <Button
          variant='ghost'
          size='icon'
          className='text-destructive'
          disabled={props.pending}
          aria-label={t('supplier.delete')}
          title={t('supplier.delete')}
          onClick={props.onDelete}
        >
          <Trash2 />
        </Button>
      </div>
      {!allowed && (
        <p className='text-muted-foreground max-w-64 text-xs break-words whitespace-normal'>
          {reason}
        </p>
      )}
    </div>
  )
}
