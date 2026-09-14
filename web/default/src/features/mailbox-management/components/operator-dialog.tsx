import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { mailboxApi } from '../api'
import { useMailboxMutation } from '../hooks'
import { operatorSchema, passwordSchema } from '../lib/schemas'
import type { Operator } from '../types'
import { Confirm, CopyButton, Field, Modal } from './common'

export function OperatorDialog(props: {
  operator?: Operator
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [generated, setGenerated] = useState('')
  const [confirmation, setConfirmation] = useState(false)
  const form = useForm<z.infer<typeof operatorSchema>>({
    resolver: zodResolver(operatorSchema),
    defaultValues: {
      username: props.operator?.username ?? '',
      display_name: props.operator?.display_name ?? '',
      enabled: props.operator?.enabled ?? true,
      version: props.operator?.version ?? 0,
      password: '',
    },
  })
  const mutation = useMailboxMutation(async () => {
    const value = form.getValues()
    const result = await mailboxApi.saveOperator(
      {
        username: value.username,
        display_name: value.display_name,
        enabled: value.enabled,
        version: value.version,
        ...(props.operator ? {} : { password: value.password }),
      },
      props.operator?.id
    )
    form.reset({ ...value, password: '' })
    setConfirmation(false)
    if (result.generated_password) setGenerated(result.generated_password)
    else props.onClose()
  })
  const submit = form.handleSubmit(() => {
    if (props.operator?.enabled && !form.getValues('enabled')) {
      setConfirmation(true)
    } else mutation.submit(undefined)
  })
  return (
    <>
      <Modal
        title={t(
          props.operator
            ? 'mailbox.admin.editOperator'
            : 'mailbox.admin.createOperator'
        )}
        dirty={form.formState.isDirty}
        pending={mutation.isPending}
        onClose={props.onClose}
        footer={
          !generated && (
            <Button
              type='submit'
              form='mailbox-operator-form'
              disabled={mutation.isPending}
            >
              {t('mailbox.admin.save')}
            </Button>
          )
        }
      >
        {generated ? (
          <div className='space-y-3'>
            <p>{t('mailbox.admin.generatedPassword')}</p>
            <div className='flex items-start gap-2'>
              <output className='min-w-0 flex-1 rounded border p-3 font-mono break-all'>
                {generated}
              </output>
              <CopyButton value={generated} />
            </div>
          </div>
        ) : (
          <form
            id='mailbox-operator-form'
            className='grid gap-4'
            onSubmit={submit}
          >
            <Field
              id='mailbox-username'
              label={t('mailbox.admin.username')}
              error={
                form.formState.errors.username && 'mailbox.admin.invalidInput'
              }
            >
              <Input
                id='mailbox-username'
                autoComplete='off'
                {...form.register('username')}
                readOnly={!!props.operator}
                disabled={mutation.isPending}
              />
            </Field>
            <Field
              id='mailbox-display-name'
              label={t('mailbox.admin.displayName')}
              error={
                form.formState.errors.display_name &&
                'mailbox.admin.invalidInput'
              }
            >
              <Input
                id='mailbox-display-name'
                {...form.register('display_name')}
                disabled={mutation.isPending}
              />
            </Field>
            {!props.operator && (
              <Field
                id='mailbox-operator-password'
                label={t('mailbox.admin.optionalPassword')}
                error={form.formState.errors.password?.message}
              >
                <Input
                  id='mailbox-operator-password'
                  type='password'
                  autoComplete='new-password'
                  {...form.register('password')}
                  disabled={mutation.isPending}
                />
              </Field>
            )}
            <label className='flex items-center gap-2 text-sm'>
              <input
                type='checkbox'
                {...form.register('enabled')}
                disabled={mutation.isPending}
              />
              {t('mailbox.admin.enabled')}
            </label>
          </form>
        )}
      </Modal>
      {confirmation && (
        <Confirm
          title={t('mailbox.admin.disableOperator')}
          description={t('mailbox.admin.disableConfirm')}
          pending={mutation.isPending}
          onClose={() => setConfirmation(false)}
          onConfirm={() => mutation.submit(undefined)}
        />
      )}
    </>
  )
}
export function ResetPasswordDialog(props: {
  operator: Operator
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [generated, setGenerated] = useState('')
  const [confirm, setConfirm] = useState(false)
  const form = useForm<z.infer<typeof passwordSchema>>({
    resolver: zodResolver(passwordSchema),
    defaultValues: { password: '' },
  })
  const mutation = useMailboxMutation(async () => {
    const result = await mailboxApi.password(
      props.operator.id,
      form.getValues('password')
    )
    form.reset({ password: '' })
    setConfirm(false)
    if (result.password) setGenerated(result.password)
    else props.onClose()
  })
  return (
    <>
      <Modal
        title={t('mailbox.admin.resetPasswordFor', {
          name: props.operator.display_name,
        })}
        dirty={form.formState.isDirty}
        pending={mutation.isPending}
        onClose={props.onClose}
        footer={
          !generated && (
            <Button
              type='submit'
              form='mailbox-reset-password'
              disabled={mutation.isPending}
            >
              {t('mailbox.admin.resetPassword')}
            </Button>
          )
        }
      >
        {generated ? (
          <div className='space-y-3'>
            <p>{t('mailbox.admin.generatedPassword')}</p>
            <div className='flex items-start gap-2'>
              <output className='min-w-0 flex-1 rounded border p-3 font-mono break-all'>
                {generated}
              </output>
              <CopyButton value={generated} />
            </div>
          </div>
        ) : (
          <form
            id='mailbox-reset-password'
            onSubmit={form.handleSubmit(() => setConfirm(true))}
          >
            <Field
              id='mailbox-new-password'
              label={t('mailbox.admin.optionalPassword')}
              error={form.formState.errors.password?.message}
            >
              <Input
                id='mailbox-new-password'
                type='password'
                autoComplete='new-password'
                {...form.register('password')}
                disabled={mutation.isPending}
              />
            </Field>
          </form>
        )}
      </Modal>
      {confirm && (
        <Confirm
          title={t('mailbox.admin.resetPassword')}
          description={t('mailbox.admin.revokeConfirm')}
          pending={mutation.isPending}
          onClose={() => setConfirm(false)}
          onConfirm={() => mutation.submit(undefined)}
        />
      )}
    </>
  )
}
