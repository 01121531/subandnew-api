import { zodResolver } from '@hookform/resolvers/zod'
import { LockKeyhole } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { useSupplierMutation } from '../hooks/use-portal-query'
import { passwordSchema } from '../lib/schemas'
import { portalApi } from '../portal-api'
import { clearSupplierSession } from '../session'
import type { AuthSession } from '../types'
import { Field } from './common'

export function PasswordForm(props: { session: AuthSession }) {
  const { t } = useTranslation()
  const form = useForm({
    resolver: zodResolver(passwordSchema),
    defaultValues: { current_password: '', password: '', confirm: '' },
  })
  const mutation = useSupplierMutation(
    (data: { current_password: string; password: string }) =>
      portalApi.password(props.session.csrf_token, data),
    () => {
      form.reset()
      toast.success(t('supplier.saved'))
      clearSupplierSession()
    }
  )
  return (
    <form
      className='grid max-w-md gap-5'
      noValidate
      onSubmit={form.handleSubmit((data) =>
        mutation.mutate({
          current_password: data.current_password,
          password: data.password,
        })
      )}
    >
      <h2 className='flex items-center gap-2 border-b pb-4 text-base font-semibold'>
        <LockKeyhole className='text-muted-foreground size-4' />
        {t('supplier.changePassword')}
      </h2>
      <fieldset disabled={mutation.isPending} className='grid min-w-0 gap-5'>
        <Field
          id='current-password'
          label={t('supplier.currentPassword')}
          error={!!form.formState.errors.current_password}
        >
          <Input
            id='current-password'
            type='password'
            autoComplete='current-password'
            {...form.register('current_password')}
          />
        </Field>
        <Field
          id='new-password'
          label={t('supplier.newPassword')}
          error={
            form.formState.errors.password && t('supplier.ui_passwordLength')
          }
        >
          <Input
            id='new-password'
            type='password'
            autoComplete='new-password'
            {...form.register('password')}
          />
        </Field>
        <Field
          id='confirm-password'
          label={t('supplier.confirmPassword')}
          error={
            form.formState.errors.confirm && t('supplier.ui_passwordMismatch')
          }
        >
          <Input
            id='confirm-password'
            type='password'
            autoComplete='new-password'
            {...form.register('confirm')}
          />
        </Field>
        <Button
          type='submit'
          className='mt-1 justify-self-start'
          disabled={mutation.isPending}
        >
          {t(mutation.isPending ? 'supplier.loading' : 'supplier.save')}
        </Button>
      </fieldset>
    </form>
  )
}
