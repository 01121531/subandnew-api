import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

import { adminApi } from '../admin-api'
import { useAdminMutation } from '../hooks/use-admin-mutation'
import { supplierDefaults, supplierSchema } from '../lib/schemas'
import type { Supplier, SupplierInput } from '../types'
import { Confirm, Field } from './common'
import { PasswordGenerator } from './password-generator'

export function SupplierForm(props: {
  supplier?: Supplier
  onClose: () => void
}) {
  const { t } = useTranslation()
  const form = useForm({
    resolver: zodResolver(supplierSchema),
    defaultValues: props.supplier
      ? { ...props.supplier, password: '' }
      : supplierDefaults,
  })
  const [confirmation, setConfirmation] = useState<SupplierInput | null>(null)
  const mutation = useAdminMutation(
    (data: SupplierInput) => adminApi.save(data, props.supplier?.id),
    () => {
      toast.success(t('supplier.saved'))
      form.reset()
      props.onClose()
    }
  )
  const submit = (data: typeof supplierDefaults) => {
    if (!props.supplier && data.password.length < 8) {
      form.setError('password', { message: t('supplier.passwordMin') })
      return
    }
    const input = { ...data, password: data.password || undefined }
    const addsWrite =
      (data.manage_proxies && !props.supplier?.manage_proxies) ||
      (data.upload_accounts && !props.supplier?.upload_accounts)
    if (addsWrite || (props.supplier?.enabled && !data.enabled)) {
      setConfirmation(input)
    } else mutation.mutate(input)
  }
  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) props.onClose()
        }}
      >
        <DialogContent className='max-h-[90dvh] overflow-y-auto sm:max-w-lg'>
          <DialogTitle>
            {props.supplier ? t('supplier.edit') : t('supplier.create')}
          </DialogTitle>
          <form className='grid gap-4' onSubmit={form.handleSubmit(submit)}>
            <Field
              id='supplier-name'
              label={t('supplier.name')}
              error={!!form.formState.errors.name}
            >
              <Input id='supplier-name' {...form.register('name')} />
            </Field>
            <Field
              id='supplier-user'
              label={t('supplier.username')}
              error={!!form.formState.errors.username}
            >
              <Input
                id='supplier-user'
                autoComplete='off'
                {...form.register('username')}
              />
            </Field>
            {!props.supplier && (
              <Field
                id='supplier-new-password'
                label={t('supplier.password')}
                error={!!form.formState.errors.password}
              >
                <Input
                  id='supplier-new-password'
                  type='password'
                  autoComplete='new-password'
                  {...form.register('password')}
                />
                <PasswordGenerator
                  value={form.watch('password')}
                  disabled={mutation.isPending}
                  onGenerate={(value) =>
                    form.setValue('password', value, { shouldValidate: true })
                  }
                />
              </Field>
            )}
            <label className='flex items-center gap-2 text-sm'>
              <input type='checkbox' {...form.register('enabled')} />
              {t('supplier.enabled')}
            </label>
            <fieldset className='grid gap-3 border-y py-4'>
              <legend className='text-sm font-medium'>
                {t('supplier.permissions')}
              </legend>
              {(
                [
                  ['view_accounts', 'readAccounts'],
                  ['view_usage', 'readUsage'],
                  ['manage_proxies', 'manageProxies'],
                  ['upload_accounts', 'uploadAccounts'],
                ] as const
              ).map(([key, label]) => (
                <label key={key} className='flex items-center gap-2 text-sm'>
                  <input type='checkbox' {...form.register(key)} />
                  {t(`supplier.${label}`)}
                </label>
              ))}
            </fieldset>
            <Button type='submit' disabled={mutation.isPending}>
              {t('supplier.save')}
            </Button>
          </form>
        </DialogContent>
      </Dialog>
      <Confirm
        open={!!confirmation}
        title={t('supplier.permissions')}
        description={
          confirmation?.enabled === false
            ? t('supplier.disableConfirm')
            : t('supplier.permissionConfirm')
        }
        pending={mutation.isPending}
        onClose={() => setConfirmation(null)}
        onConfirm={() => {
          if (confirmation) mutation.mutate(confirmation)
        }}
      />
    </>
  )
}
