import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { mailboxApi } from '../api'
import { useMailboxMutation } from '../hooks'
import type { Account } from '../types'
import { Field, Modal } from './common'

export function TemporaryCvvDialog(props: {
  account: Account
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [cvv, setCvv] = useState('')
  const [confirmClear, setConfirmClear] = useState(false)
  const valid = /^[0-9]{3,4}$/.test(cvv)
  const mutation = useMailboxMutation(async (value: string) => {
    setCvv('')
    if (value === 'clear') {
      await mailboxApi.clearCvv(props.account.id, props.account.version)
      toast.success(t('mailbox.cardFilters.cleared'))
      return
    }
    await mailboxApi.provideTemporaryCvv(
      props.account.id,
      props.account.version,
      value
    )
    toast.success(t('mailbox.admin.cvvProvided'))
  }, props.onClose)
  useEffect(() => {
    function clear() {
      if (document.visibilityState !== 'visible') setCvv('')
    }
    document.addEventListener('visibilitychange', clear)
    return () => document.removeEventListener('visibilitychange', clear)
  }, [])
  return (
    <Modal
      title={t('mailbox.admin.provideCvv')}
      description={props.account.email}
      dirty={!!cvv}
      pending={mutation.isPending}
      onClose={props.onClose}
      footer={
        <>
          <Button
            variant='destructive'
            disabled={mutation.isPending}
            onClick={() => setConfirmClear(true)}
          >
            {t('mailbox.cardFilters.clearCvv')}
          </Button>
          <Button
            disabled={!valid || mutation.isPending}
            onClick={() => mutation.submit(cvv)}
          >
            {t('mailbox.admin.provideCvv')}
          </Button>
        </>
      }
    >
      <p className='text-muted-foreground mb-4 text-sm'>
        {t('mailbox.admin.temporaryCvvNotice')}
      </p>
      {confirmClear && (
        <div role='alert' className='mb-4 space-y-2'>
          <p>{t('mailbox.cardFilters.clearConfirm')}</p>
          <Button
            variant='destructive'
            disabled={mutation.isPending}
            onClick={() => mutation.submit('clear')}
          >
            {t('mailbox.cardFilters.clearCvv')}
          </Button>
        </div>
      )}
      <Field id='mailbox-temporary-cvv' label='CVV'>
        <Input
          id='mailbox-temporary-cvv'
          type='password'
          inputMode='numeric'
          autoComplete='off'
          maxLength={4}
          disabled={mutation.isPending}
          value={cvv}
          onChange={(event) => setCvv(event.target.value)}
          aria-invalid={!!cvv && !valid}
        />
      </Field>
    </Modal>
  )
}
