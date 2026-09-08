import { zodResolver } from '@hookform/resolvers/zod'
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
      className='grid max-w-sm gap-4'
      onSubmit={form.handleSubmit((data) =>
        mutation.mutate({
          current_password: data.current_password,
          password: data.password,
        })
      )}
    >
      <h2 className='text-lg font-semibold'>{t('supplier.changePassword')}</h2>
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
        error={!!form.formState.errors.password}
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
        error={!!form.formState.errors.confirm}
      >
        <Input
          id='confirm-password'
          type='password'
          autoComplete='new-password'
          {...form.register('confirm')}
        />
      </Field>
      <Button type='submit' disabled={mutation.isPending}>
        {t('supplier.save')}
      </Button>
    </form>
  )
}
