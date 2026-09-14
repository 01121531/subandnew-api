import { useTranslation } from 'react-i18next'

import { AdminDataField } from '@/components/admin-data-field'
import type { ManagedInstanceInventoryItem } from '@/features/managed-instances/types'
import { formatAdminDataValue } from '@/lib/admin-data-policy-view'

export function AccountLoad(props: {
  item: ManagedInstanceInventoryItem
  isConductor: boolean
  isClaudeGateway: boolean
}) {
  const { t } = useTranslation()
  const item = props.item
  const metric = formatAdminDataValue
  return (
    <div className='space-y-1 text-sm tabular-nums'>
      {props.isConductor ? (
        <>
          <AdminDataField fields={['rpm']}>
            <p>{metric(item.rpm)} RPM</p>
          </AdminDataField>
          <AdminDataField fields={['concurrency']}>
            <p>
              {t('adminData.fields.concurrency')}:{' '}
              {metric(item.active_sessions)}
            </p>
          </AdminDataField>
          <AdminDataField fields={['rates']}>
            <p>
              {t('adminData.utilization5h')}:{' '}
              {item.utilization_5h == null
                ? '--'
                : `${(item.utilization_5h * 100).toFixed(1)}%`}
            </p>
          </AdminDataField>
        </>
      ) : (
        <>
          <AdminDataField fields={['amount']}>
            <p>
              {t('Amount')}: {metric(item.cost)} {item.cost_unit}
            </p>
            {item.balance != null && (
              <p>
                {t('Balance')}: {metric(item.balance)}
              </p>
            )}
          </AdminDataField>
          <AdminDataField fields={['requests']}>
            <p>
              {t('Requests')}:{' '}
              {metric(
                props.isClaudeGateway ? item.requests_24h : item.requests
              )}
              {props.isClaudeGateway && ' / 24h'}
            </p>
          </AdminDataField>
          <AdminDataField fields={['tokens']}>
            <p>
              {t('Tokens')}: {metric(item.tokens)}
            </p>
          </AdminDataField>
          {props.isClaudeGateway && (
            <AdminDataField fields={['rates', 'requests']}>
              <p>
                {t('Success rate')}:{' '}
                {item.requests_24h && item.successful_requests_24h != null
                  ? `${((100 * item.successful_requests_24h) / item.requests_24h).toFixed(2)}%`
                  : '--'}
              </p>
              {item.limited_requests_24h != null && (
                <p>
                  {t('Rate limited')}: {metric(item.limited_requests_24h)}
                </p>
              )}
            </AdminDataField>
          )}
        </>
      )}
    </div>
  )
}
