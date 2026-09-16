import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import { useSupplierPolicy } from '../lib/permissions'
import type { Account } from '../types'

export function AccountStatus(props: { account: Account }) {
  const { t } = useTranslation()
  const allowed = useSupplierPolicy()
  if (!allowed('account.status')) return null
  const account = props.account
  const states = [...new Set([account.status, account.health_status])].filter(
    (value): value is string => !!value
  )
  const hasCooldown = account.cooldown === true
  const cooldownReason = account.cooldown !== false && account.cooldown_reason
  const abnormal = states.some((state) =>
    ['error', 'unhealthy', 'failed', 'unavailable', 'blocked'].includes(state)
  )
  return (
    <div className='grid min-w-0 gap-2 text-xs [overflow-wrap:anywhere] whitespace-normal'>
      <div className='flex min-w-0 flex-wrap gap-1'>
        {states.length === 0 && <span>--</span>}
        {states.map((state) => (
          <Badge
            key={state}
            variant='outline'
            className='h-auto max-w-full whitespace-normal'
          >
            {t(`supplier.${state === 'active' ? 'enabled' : state}`, {
              defaultValue: state,
            })}
          </Badge>
        ))}
        {hasCooldown && !states.includes('cooldown') && (
          <Badge
            variant='outline'
            className='h-auto whitespace-normal text-amber-700 dark:text-amber-400'
          >
            {t('supplier.cooldown')}
          </Badge>
        )}
      </div>
      {account.failure_kind && (
        <p>
          {t('supplier.accountFailureKind')}: {account.failure_kind}
        </p>
      )}
      {account.last_error && (
        <p className='text-destructive'>
          {t('supplier.accountLastError')}: {account.last_error}
        </p>
      )}
      {cooldownReason && cooldownReason !== account.last_error && (
        <p className='text-amber-700 dark:text-amber-400'>
          {t('supplier.accountCooldownReason')}: {cooldownReason}
        </p>
      )}
      {hasCooldown && account.cooldown_remaining_seconds != null && (
        <p className='text-muted-foreground'>
          {t('supplier.accountCooldownRemaining', {
            seconds: account.cooldown_remaining_seconds,
          })}
        </p>
      )}
      {(abnormal || hasCooldown) && !account.last_error && !cooldownReason && (
        <p className='text-muted-foreground'>
          {t('supplier.accountErrorMissing')}
        </p>
      )}
    </div>
  )
}
