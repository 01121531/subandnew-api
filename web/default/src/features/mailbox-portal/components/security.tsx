import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { mailboxApi } from '../api'
import { passwordSchema } from '../lib/schemas'
import { clearMailboxSession } from '../session'
import { ErrorMessage, Field } from './common'

export function Security(props: { csrf: string }) {
  const { t } = useTranslation()
  const lock = useRef(false)
  const form = useForm({
    resolver: zodResolver(passwordSchema),
    defaultValues: { current_password: '', password: '', confirm: '' },
  })
  const mutation = useMutation({
    mutationFn: () => {
      const values = form.getValues()
      return mailboxApi.password(props.csrf, {
        current_password: values.current_password,
        password: values.password,
      })
    },
    onSuccess: clearMailboxSession,
    onSettled: () => {
      form.reset()
      lock.current = false
    },
  })
  return (
    <section className='max-w-md py-6'>
      <h2 className='mb-6 text-lg font-semibold'>
        {t('mailboxPortal.changePassword')}
      </h2>
      <form
        noValidate
        onSubmit={form.handleSubmit(() => {
          if (!lock.current) {
            lock.current = true
            mutation.mutate()
          }
        })}
      >
        <fieldset disabled={mutation.isPending} className='grid min-w-0 gap-5'>
          {(['current_password', 'password', 'confirm'] as const).map(
            (name) => (
              <Field
                key={name}
                id={`mailbox-${name}`}
                label={t(`mailboxPortal.field_${name}`)}
                error={
                  form.formState.errors[name] &&
                  (name === 'current_password'
                    ? 'mailboxPortal.required'
                    : form.formState.errors[name]?.message)
                }
              >
                <Input
                  id={`mailbox-${name}`}
                  type='password'
                  autoComplete={
                    name === 'current_password'
                      ? 'current-password'
                      : 'new-password'
                  }
                  aria-invalid={!!form.formState.errors[name]}
                  aria-describedby={
                    form.formState.errors[name]
                      ? `mailbox-${name}-error`
                      : undefined
                  }
                  {...form.register(name)}
                />
              </Field>
            )
          )}
          <ErrorMessage error={mutation.error} />
          <Button type='submit' className='h-10'>
            <KeyRound />
            {t(
              mutation.isPending
                ? 'mailboxPortal.loading'
                : 'mailboxPortal.changePassword'
            )}
          </Button>
        </fieldset>
      </form>
    </section>
  )
}
