import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import { mailboxApi } from '../api'
import { useMailboxMutation } from '../hooks'
import { statusTargets } from '../lib/change-status'
import { statusLabelKey } from '../lib/display'
import { errorKey } from '../lib/errors'
import type { Account, AccountType } from '../types'
import { Field, Modal } from './common'

export function ChangeStatusDialog(props: {
  accountType: AccountType
  accounts: Account[]
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const [accounts] = useState(props.accounts)
  const targets = statusTargets(accounts)
  const [status, setStatus] = useState(targets[0] ?? '')
  const [reason, setReason] = useState('')
  const mutation = useMailboxMutation(async () => {
    await mailboxApi.changeStatus(props.accountType, accounts, status, reason)
  }, props.onSuccess)
  return (
    <Modal
      title={t('mailbox.changeStatus.title')}
      description={t('mailbox.changeStatus.description', {
        count: accounts.length,
      })}
      dirty={reason.length > 0}
      pending={mutation.isPending}
      onClose={props.onClose}
      footer={
        <Button
          type='submit'
          form='mailbox-change-status'
          disabled={
            mutation.isPending || !targets.includes(status) || !reason.trim()
          }
        >
          {t('mailbox.admin.confirm')}
        </Button>
      }
    >
      <form
        id='mailbox-change-status'
        className='space-y-4'
        onSubmit={(event) => {
          event.preventDefault()
          if (targets.includes(status) && reason.trim()) {
            mutation.submit(undefined)
          }
        }}
      >
        <Field id='mailbox-target-status' label={t('mailbox.admin.status')}>
          <NativeSelect
            id='mailbox-target-status'
            value={status}
            disabled={mutation.isPending || !targets.length}
            onChange={(event) => setStatus(event.target.value)}
          >
            {targets.map((value) => (
              <option key={value} value={value}>
                {t(statusLabelKey(value))}
              </option>
            ))}
          </NativeSelect>
        </Field>
        {!targets.length && (
          <p role='alert'>{t('mailbox.changeStatus.unavailable')}</p>
        )}
        <Field
          id='mailbox-status-reason'
          label={t('mailbox.changeStatus.reason')}
        >
          <Textarea
            id='mailbox-status-reason'
            required
            maxLength={2000}
            value={reason}
            disabled={mutation.isPending}
            onChange={(event) => setReason(event.target.value)}
          />
        </Field>
        <p className='text-muted-foreground text-sm'>
          {t(
            status === 'approved'
              ? 'mailbox.changeStatus.revoke'
              : 'mailbox.changeStatus.reopen'
          )}
        </p>
        {mutation.error && (
          <p role='alert' className='text-destructive'>
            {t(errorKey(mutation.error))}
          </p>
        )}
      </form>
    </Modal>
  )
}
