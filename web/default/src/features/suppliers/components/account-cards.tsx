import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import { formatNumber } from '../lib/display'
import { formatCost } from '../lib/usage'
import type { Account } from '../types'
import { Time } from './common'

export function AccountCards(props: { accounts: Account[] }) {
  const { t } = useTranslation()
  return (
    <div className='grid gap-3 md:hidden'>
      {props.accounts.map((account) => (
        <details key={account.id} className='group min-w-0 rounded-md border'>
          <summary className='cursor-pointer list-none p-3 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-600'>
            <div className='flex items-start gap-2'>
              <h3 className='min-w-0 flex-1 font-semibold break-words'>
                {account.name || '--'}
              </h3>
              <Badge variant='outline'>
                {t(`supplier.${account.status}`, {
                  defaultValue: account.status ?? '--',
                })}
              </Badge>
              <ChevronDown
                className='size-4 shrink-0 transition-transform group-open:rotate-180'
                aria-hidden='true'
              />
            </div>
            <p className='text-muted-foreground mt-1 font-mono text-xs break-all'>
              UUID: {account.id}
            </p>
            <dl className='mt-4 grid grid-cols-2 gap-3 text-sm'>
              <div className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.todayCost')}
                </dt>
                <dd className='mt-1 font-medium break-words tabular-nums'>
                  {formatCost(account.today_cost)}
                </dd>
              </div>
              <div className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.totalCost')}
                </dt>
                <dd className='mt-1 font-medium break-words tabular-nums'>
                  {formatCost(account.total_cost)}
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
              <dd className='break-words'>{account.group_name || '--'}</dd>
            </div>
            <div className='grid grid-cols-2 gap-3'>
              <div>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.requests')}
                </dt>
                <dd className='break-words tabular-nums'>
                  {formatNumber(account.total_requests)}
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.tokens')}
                </dt>
                <dd className='break-words tabular-nums'>
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
