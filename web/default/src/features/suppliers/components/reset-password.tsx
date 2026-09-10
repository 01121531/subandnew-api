import { zodResolver } from '@hookform/resolvers/zod'
import { Save } from 'lucide-react'
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
import { useFormLeaveGuard } from '../hooks/use-form-leave-guard'
import { Confirm, Field } from './common'
import { PasswordGenerator } from './password-generator'

export function ResetPassword(props: {
  supplierId: number
  onClose: () => void
}) {
  const { t } = useTranslation()
  const schema = z
    .object({
      password: z.string().refine((value) => {
        const bytes = new TextEncoder().encode(value).length
        return value.trim().length > 0 && bytes >= 8 && bytes <= 72
      }, t('supplier.ui_passwordInvalid')),
      confirm: z.string(),
    })
    .refine((data) => data.password === data.confirm, {
      path: ['confirm'],
      message: t('supplier.ui_passwordMismatch'),
    })
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
  const locked = mutation.isPending || confirm
  const guard = useFormLeaveGuard({
    dirty: form.formState.isDirty,
    pending: locked,
  })
  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) guard.requestLeave(props.onClose)
        }}
      >
        <DialogContent
          showCloseButton={!locked}
          className='supplier-portal flex max-h-[90dvh] flex-col gap-0 overflow-hidden rounded-lg p-0 sm:max-w-lg'
        >
          <DialogTitle className='shrink-0 border-b p-4 pr-12'>
            {t('supplier.resetPassword')}
          </DialogTitle>
          <form
            className='flex min-h-0 flex-col'
            noValidate
            aria-busy={mutation.isPending}
            onSubmit={form.handleSubmit(() => {
              if (!locked) setConfirm(true)
            })}
          >
            <fieldset
              disabled={locked}
              className='grid min-h-0 gap-4 overflow-y-auto p-4'
            >
              <Field
                id='reset-password'
                label={t('supplier.newPassword')}
                error={form.formState.errors.password?.message}
              >
                <Input
                  id='reset-password'
                  type='password'
                  autoComplete='new-password'
                  aria-invalid={!!form.formState.errors.password}
                  {...form.register('password')}
                />
              </Field>
              <Field
                id='reset-confirm'
                label={t('supplier.confirmPassword')}
                error={form.formState.errors.confirm?.message}
              >
                <Input
                  id='reset-confirm'
                  type='password'
                  autoComplete='new-password'
                  aria-invalid={!!form.formState.errors.confirm}
                  {...form.register('confirm')}
                />
              </Field>
              <PasswordGenerator
                value={form.watch('password')}
                disabled={locked}
                onGenerate={(value) => {
                  form.setValue('password', value, {
                    shouldValidate: true,
                    shouldDirty: true,
                  })
                  form.setValue('confirm', value, {
                    shouldValidate: true,
                    shouldDirty: true,
                  })
                }}
              />
            </fieldset>
            <footer className='flex shrink-0 justify-end gap-2 border-t p-4'>
              <Button
                type='button'
                variant='outline'
                disabled={locked}
                onClick={() => guard.requestLeave(props.onClose)}
              >
                {t('supplier.cancel')}
              </Button>
              <Button type='submit' disabled={locked}>
                <Save />
                {t('supplier.save')}
              </Button>
            </footer>
          </form>
        </DialogContent>
      </Dialog>
      <Confirm
        open={confirm}
        title={t('supplier.resetPassword')}
        description={t('supplier.resetConfirm')}
        pending={mutation.isPending}
        onClose={() => setConfirm(false)}
        onConfirm={() => {
          if (!mutation.isPending) {
            mutation.mutate(form.getValues('password'), {
              onError: () => setConfirm(false),
            })
          }
        }}
      />
      {guard.discardConfirmation}
    </>
  )
}
