import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import { formatNumber } from '../lib/display'
import type { Account } from '../types'
import { Time } from './common'
import { CostValue } from './cost-value'

export function AccountCards(props: { accounts: Account[] }) {
  const { t } = useTranslation()
  return (
    <div className='supplier-portal grid min-w-0 gap-3 md:hidden'>
      {props.accounts.map((account) => (
        <details
          key={account.id}
          className='supplier-data-item group min-w-0 rounded-md border'
        >
          <summary className='cursor-pointer list-none p-3 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-600'>
            <div className='grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2'>
              <h3 className='min-w-0 font-semibold [overflow-wrap:anywhere]'>
                {account.name || '--'}
              </h3>
              <ChevronDown
                className='size-4 shrink-0 transition-transform group-open:rotate-180'
                aria-hidden='true'
              />
              <Badge
                variant='outline'
                className='col-span-2 h-auto min-h-5 max-w-full rounded-sm [overflow-wrap:anywhere] whitespace-normal'
              >
                <span className='sr-only'>{t('supplier.status')}: </span>
                {t(`supplier.${account.status}`, {
                  defaultValue: account.status ?? '--',
                })}
              </Badge>
            </div>
            <p className='text-muted-foreground mt-1 font-mono text-xs break-all'>
              UUID: {account.id}
            </p>
            <dl className='mt-3 grid grid-cols-2 gap-3 text-sm'>
              <div className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.todayCost')}
                </dt>
                <dd className='mt-1 font-medium [overflow-wrap:anywhere] tabular-nums'>
                  <CostValue value={account.today_cost} />
                </dd>
              </div>
              <div className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.totalCost')}
                </dt>
                <dd className='mt-1 font-medium [overflow-wrap:anywhere] tabular-nums'>
                  <CostValue value={account.total_cost} />
                </dd>
              </div>
            </dl>
          </summary>
          <dl className='grid gap-3 border-t p-3 text-sm'>
            <div>
              <dt className='text-muted-foreground text-xs'>
                {t('supplier.email')}
              </dt>
              <dd className='break-all'>{account.email || '--'}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground text-xs'>
                {t('supplier.group')}
              </dt>
              <dd className='[overflow-wrap:anywhere]'>
                {account.group_name || '--'}
              </dd>
            </div>
            <div className='grid grid-cols-2 gap-3'>
              <div className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.requests')}
                </dt>
                <dd className='[overflow-wrap:anywhere] tabular-nums'>
                  {formatNumber(account.total_requests)}
                </dd>
              </div>
              <div className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.tokens')}
                </dt>
                <dd className='[overflow-wrap:anywhere] tabular-nums'>
                  {formatNumber(account.total_tokens)}
                </dd>
              </div>
            </div>
            <div>
              <dt className='text-muted-foreground text-xs'>
                {t('supplier.createdAt')}
              </dt>
              <dd>
                <Time value={account.created_at} />
              </dd>
            </div>
          </dl>
        </details>
      ))}
    </div>
  )
}
