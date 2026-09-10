import { zodResolver } from '@hookform/resolvers/zod'
import { Save } from 'lucide-react'
import { useState, type Ref } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

import { adminApi } from '../admin-api'
import { useAdminMutation } from '../hooks/use-admin-mutation'
import {
  useFormLeaveGuard,
  type FormLeaveGuard,
} from '../hooks/use-form-leave-guard'
import { supplierDefaults, supplierSchema } from '../lib/schemas'
import { SupplierRequestError } from '../portal-api'
import type { Supplier, SupplierInput } from '../types'
import { Confirm, Field } from './common'
import { PasswordGenerator } from './password-generator'

export function SupplierForm(props: {
  supplier?: Supplier
  embedded?: boolean
  guardRef?: Ref<FormLeaveGuard>
  onPendingChange?: (pending: boolean) => void
  onSaved?: (supplier: Supplier) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const schema = supplierSchema.extend({
    name: z
      .string()
      .trim()
      .refine(
        (value) => value.length > 0 && [...value].length <= 96,
        t('supplier.ui_supplierNameInvalid')
      ),
    username: z
      .string()
      .trim()
      .toLowerCase()
      .regex(/^[a-z0-9][a-z0-9@._+-]{2,95}$/, t('supplier.ui_usernameInvalid')),
    password: z.string().refine((value) => {
      if (props.supplier) return value === ''
      const bytes = new TextEncoder().encode(value).length
      return value.trim().length > 0 && bytes >= 8 && bytes <= 72
    }, t('supplier.ui_passwordInvalid')),
  })
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: props.supplier
      ? { ...props.supplier, password: '' }
      : supplierDefaults,
  })
  const [confirmation, setConfirmation] = useState<SupplierInput | null>(null)
  const mutation = useAdminMutation(
    (data: SupplierInput) => adminApi.save(data, props.supplier?.id),
    (supplier) => {
      toast.success(t('supplier.saved'))
      form.reset({ ...supplier, password: '' })
      if (props.onSaved) props.onSaved(supplier)
      else props.onClose()
    }
  )
  const locked = mutation.isPending || !!confirmation
  const guard = useFormLeaveGuard({
    dirty: form.formState.isDirty,
    pending: locked,
    guardRef: props.guardRef,
    onPendingChange: props.onPendingChange,
  })
  const save = (data: SupplierInput) => {
    if (mutation.isPending) return
    mutation.mutate(data, {
      onError: (error) => {
        setConfirmation(null)
        if (
          error instanceof SupplierRequestError &&
          error.code === 'supplier_username_conflict'
        ) {
          form.setError(
            'username',
            { message: t('supplier.usernameConflict') },
            { shouldFocus: true }
          )
        }
      },
    })
  }
  const submit = (data: typeof supplierDefaults) => {
    if (locked) return
    const input = { ...data, password: data.password || undefined }
    const addsWrite =
      (data.manage_proxies && !props.supplier?.manage_proxies) ||
      (data.upload_accounts && !props.supplier?.upload_accounts)
    if (addsWrite || (props.supplier?.enabled && !data.enabled)) {
      setConfirmation(input)
    } else save(input)
  }
  const content = (
    <form
      className='flex min-h-0 flex-1 flex-col'
      noValidate
      onSubmit={form.handleSubmit(submit)}
      aria-busy={mutation.isPending}
    >
      <fieldset
        disabled={locked}
        className='grid min-h-0 min-w-0 flex-1 gap-4 overflow-y-auto p-4'
      >
        <Field
          id='supplier-name'
          label={t('supplier.name')}
          error={form.formState.errors.name?.message}
        >
          <Input
            id='supplier-name'
            aria-invalid={!!form.formState.errors.name}
            {...form.register('name')}
          />
        </Field>
        <Field
          id='supplier-user'
          label={t('supplier.username')}
          error={form.formState.errors.username?.message}
        >
          <Input
            id='supplier-user'
            autoComplete='off'
            autoCapitalize='none'
            spellCheck={false}
            aria-invalid={!!form.formState.errors.username}
            {...form.register('username')}
          />
        </Field>
        {!props.supplier && (
          <Field
            id='supplier-new-password'
            label={t('supplier.password')}
            error={form.formState.errors.password?.message}
          >
            <Input
              id='supplier-new-password'
              type='password'
              autoComplete='new-password'
              aria-invalid={!!form.formState.errors.password}
              {...form.register('password')}
            />
            <PasswordGenerator
              value={form.watch('password')}
              disabled={locked}
              onGenerate={(value) =>
                form.setValue('password', value, {
                  shouldValidate: true,
                  shouldDirty: true,
                })
              }
            />
          </Field>
        )}
        <label className='flex items-center gap-2 text-sm'>
          <input type='checkbox' {...form.register('enabled')} />
          {t('supplier.enabled')}
        </label>
        <fieldset className='grid gap-3 border-t pt-3'>
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
  )
  return (
    <>
      {props.embedded ? (
        content
      ) : (
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
              {props.supplier ? t('supplier.edit') : t('supplier.create')}
            </DialogTitle>
            {content}
          </DialogContent>
        </Dialog>
      )}
      <Confirm
        open={!!confirmation}
        title={t('supplier.permissions')}
        description={[
          props.supplier?.enabled && confirmation?.enabled === false
            ? t('supplier.disableConfirm')
            : '',
          (confirmation?.manage_proxies && !props.supplier?.manage_proxies) ||
          (confirmation?.upload_accounts && !props.supplier?.upload_accounts)
            ? t('supplier.permissionConfirm')
            : '',
        ]
          .filter(Boolean)
          .join(' ')}
        pending={mutation.isPending}
        onClose={() => setConfirmation(null)}
        onConfirm={() => {
          if (confirmation) save(confirmation)
        }}
      />
      {guard.discardConfirmation}
    </>
  )
}
