import { useId, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import type { AccountType } from '../types'

export function PoolScope(props: {
  children: (accountType: AccountType) => ReactNode
}) {
  const { t } = useTranslation()
  const name = useId()
  const [accountType, setAccountType] = useState<AccountType>('refund')
  return (
    <div className='min-w-0'>
      <fieldset className='min-w-0 pt-4'>
        <legend className='sr-only'>{t('mailbox.admin.accountType')}</legend>
        <div className='flex flex-wrap gap-2'>
          {(['refund', 'opening'] as const).map((value) => (
            <label key={value} className='relative min-w-0 cursor-pointer'>
              <input
                className='peer sr-only'
                type='radio'
                name={name}
                value={value}
                checked={accountType === value}
                onChange={() => setAccountType(value)}
              />
              <span className='border-input bg-background text-muted-foreground peer-checked:border-primary peer-checked:bg-primary/10 peer-checked:text-foreground peer-focus-visible:ring-ring flex min-h-10 items-center justify-center rounded-md border px-4 py-2 text-center text-sm font-medium break-words peer-focus-visible:ring-2'>
                {t(`mailbox.admin.pools.${value}`)}
              </span>
            </label>
          ))}
        </div>
      </fieldset>
      {props.children(accountType)}
    </div>
  )
}
