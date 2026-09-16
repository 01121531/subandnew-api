import { useTranslation } from 'react-i18next'

import { formatNumber } from '../lib/display'
import { useSupplierPolicy } from '../lib/permissions'
import type { Account } from '../types'

export function AccountRuntime(props: { account: Account }) {
  const { t } = useTranslation()
  const allowed = useSupplierPolicy()
  const metrics = [
    ['rpm', 'max_rpm', 'accountRpm'],
    ['tpm', 'max_tpm', 'accountTpm'],
    ['concurrent', 'max_concurrent', 'accountConcurrent'],
    ['active_sessions', 'max_sessions', 'accountSessions'],
  ] as const
  return (
    <dl className='grid min-w-0 grid-cols-2 gap-x-4 gap-y-2 text-xs'>
      {metrics
        .filter(([field]) => allowed(`account.${field}`))
        .map(([field, limit, label]) => (
          <div key={field} className='min-w-0'>
            <dt className='text-muted-foreground'>{t(`supplier.${label}`)}</dt>
            <dd className='mt-1 font-medium [overflow-wrap:anywhere] tabular-nums'>
              {formatNumber(props.account[field])}
              <span className='text-muted-foreground mt-0.5 block font-normal'>
                {t('supplier.accountMetricLimit', {
                  value: formatNumber(props.account[limit]),
                })}
              </span>
            </dd>
          </div>
        ))}
    </dl>
  )
}
