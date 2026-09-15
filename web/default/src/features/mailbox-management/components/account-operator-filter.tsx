import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'

import { mailboxApi } from '../api'
import { useMailboxQuery } from '../hooks'
import { errorKey } from '../lib/errors'
import type { AccountType } from '../types'

export function AccountOperatorFilter(props: {
  accountType: AccountType
  value: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const options = useMailboxQuery(
    ['account-operators', props.accountType],
    (signal) => mailboxApi.accountOperators(signal, props.accountType)
  )
  return (
    <div className='flex w-full min-w-0 flex-wrap items-center gap-2 sm:w-auto'>
      <NativeSelect
        aria-label={t('mailbox.admin.operatorFilter')}
        className='w-full min-w-0 sm:w-64 [&_select]:truncate'
        value={props.value}
        disabled={options.isPending || options.isError}
        onChange={(event) => props.onChange(event.target.value)}
      >
        <option value=''>
          {t(
            options.isPending
              ? 'mailbox.admin.loading'
              : 'mailbox.admin.allOperators'
          )}
        </option>
        {(options.data ?? []).map((operator) => (
          <option key={operator.id} value={operator.id}>
            {operator.display_name || operator.username} (#{operator.id})
          </option>
        ))}
      </NativeSelect>
      {options.isError && (
        <div
          role='alert'
          className='text-destructive flex min-w-0 items-center gap-2 text-sm'
        >
          <span className='break-words'>
            {t(errorKey(options.error), {
              defaultValue: t('mailbox.errors.mailbox_request_failed'),
            })}
          </span>
          <Button
            variant='outline'
            size='icon'
            title={t('mailbox.admin.retry')}
            aria-label={t('mailbox.admin.retry')}
            disabled={options.isFetching}
            onClick={() => void options.refetch()}
          >
            <RefreshCw />
          </Button>
        </div>
      )}
    </div>
  )
}
