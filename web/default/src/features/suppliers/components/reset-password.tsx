import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

import { adminApi } from '../admin-api'
import { useAdminMutation } from '../hooks/use-admin-mutation'
import { Confirm, Field } from './common'
import { PasswordGenerator } from './password-generator'

const schema = z
  .object({ password: z.string().min(8), confirm: z.string().min(8) })
  .refine((data) => data.password === data.confirm, { path: ['confirm'] })
export function ResetPassword(props: {
  supplierId: number
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [confirm, setConfirm] = useState(false)
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: { password: '', confirm: '' },
  })
  const mutation = useAdminMutation(
    (password: string) => adminApi.password(props.supplierId, password),
    () => {
      form.reset()
      toast.success(t('supplier.saved'))
      props.onClose()
    }
  )
  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) props.onClose()
        }}
      >
        <DialogContent>
          <DialogTitle>{t('supplier.resetPassword')}</DialogTitle>
          <form
            className='grid gap-4'
            onSubmit={form.handleSubmit(() => setConfirm(true))}
          >
            <Field
              id='reset-password'
              label={t('supplier.newPassword')}
              error={!!form.formState.errors.password}
            >
              <Input
                id='reset-password'
                type='password'
                autoComplete='new-password'
                {...form.register('password')}
              />
            </Field>
            <Field
              id='reset-confirm'
              label={t('supplier.confirmPassword')}
              error={!!form.formState.errors.confirm}
            >
              <Input
                id='reset-confirm'
                type='password'
                autoComplete='new-password'
                {...form.register('confirm')}
              />
            </Field>
            <Button type='submit' disabled={mutation.isPending}>
              {t('supplier.save')}
            </Button>
            <PasswordGenerator
              value={form.watch('password')}
              disabled={mutation.isPending}
              onGenerate={(value) => {
                form.setValue('password', value, { shouldValidate: true })
                form.setValue('confirm', value, { shouldValidate: true })
              }}
            />
          </form>
        </DialogContent>
      </Dialog>
      <Confirm
        open={confirm}
        title={t('supplier.resetPassword')}
        description={t('supplier.resetConfirm')}
        pending={mutation.isPending}
        onClose={() => setConfirm(false)}
        onConfirm={() => mutation.mutate(form.getValues('password'))}
      />
    </>
  )
}
