import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { Save } from 'lucide-react'
import { useRef, type Ref } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import { adminApi } from '../admin-api'
import { useAdminMutation } from '../hooks/use-admin-mutation'
import {
  useFormLeaveGuard,
  type FormLeaveGuard,
} from '../hooks/use-form-leave-guard'
import { portalSettingsSchema, uploadMethodOrder } from '../lib/portal-settings'
import type { PortalSettings } from '../types'
import { Field, QueryState } from './common'

export function PortalSettingsDialog(props: {
  canManage: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['supplier-admin', 'portal-settings'],
    queryFn: adminApi.portalSettings,
  })
  const guardRef = useRef<FormLeaveGuard>(null)
  const close = () => {
    if (guardRef.current) guardRef.current.requestLeave(props.onClose)
    else props.onClose()
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close()
      }}
    >
      <DialogContent
        showCloseButton={false}
        aria-describedby={undefined}
        className='supplier-portal flex max-h-[90dvh] flex-col gap-0 overflow-hidden p-0 sm:max-w-lg'
      >
        <DialogTitle className='shrink-0 border-b p-4'>
          {t('supplier.portalSettings')}
        </DialogTitle>
        <QueryState
          pending={query.isPending}
          error={query.error}
          retry={() => void query.refetch()}
        >
          {query.data && (
            <PortalSettingsForm
              {...props}
              initial={query.data}
              guardRef={guardRef}
            />
          )}
        </QueryState>
        {!query.data && (
          <Button className='m-4' onClick={close}>
            {t('supplier.close')}
          </Button>
        )}
      </DialogContent>
    </Dialog>
  )
}

function PortalSettingsForm(props: {
  initial: PortalSettings
  canManage: boolean
  onClose: () => void
  guardRef: Ref<FormLeaveGuard>
}) {
  const { t } = useTranslation()
  const form = useForm<PortalSettings>({
    resolver: zodResolver(portalSettingsSchema),
    defaultValues: props.initial,
  })
  const mutation = useAdminMutation(adminApi.savePortalSettings, () => {
    toast.success(t('supplier.saved'))
    props.onClose()
  })
  const guard = useFormLeaveGuard({
    dirty: form.formState.isDirty,
    pending: mutation.isPending,
    guardRef: props.guardRef,
  })
  const disabled = !props.canManage || mutation.isPending
  return (
    <form
      className='flex min-h-0 flex-1 flex-col'
      noValidate
      onSubmit={form.handleSubmit((data) => {
        if (!disabled) mutation.mutate(data)
      })}
    >
      <fieldset
        disabled={disabled}
        className='grid min-h-0 gap-5 overflow-y-auto p-4'
      >
        <Field
          id='portal-title'
          label={t('supplier.portalTitle')}
          error={
            form.formState.errors.title
              ? t('supplier.portalTextInvalid')
              : undefined
          }
        >
          <Input
            id='portal-title'
            aria-invalid={!!form.formState.errors.title}
            {...form.register('title')}
          />
        </Field>
        <div className='grid gap-3'>
          <h3 className='text-sm font-semibold'>
            {t('supplier.uploadMethods')}
          </h3>
          {uploadMethodOrder.map((method) => (
            <label
              key={method}
              className='flex items-center justify-between gap-4 text-sm'
              htmlFor={`portal-method-${method}`}
            >
              <span>{t(`supplier.method_${method}`)}</span>
              <Switch
                id={`portal-method-${method}`}
                aria-label={t(`supplier.method_${method}`)}
                disabled={disabled}
                checked={form.watch(`upload_methods.${method}`)}
                onCheckedChange={(checked) =>
                  form.setValue(`upload_methods.${method}`, checked, {
                    shouldDirty: true,
                  })
                }
              />
            </label>
          ))}
        </div>
        <p className='text-muted-foreground text-xs'>
          {t('supplier.portalSettingsHint')}
        </p>
      </fieldset>
      <footer className='flex shrink-0 justify-end gap-2 border-t p-4'>
        <Button
          type='button'
          variant='outline'
          disabled={mutation.isPending}
          onClick={() => guard.requestLeave(props.onClose)}
        >
          {t('supplier.close')}
        </Button>
        {props.canManage && (
          <Button type='submit' disabled={mutation.isPending}>
            <Save />
            {t('supplier.save')}
          </Button>
        )}
      </footer>
      {guard.discardConfirmation}
    </form>
  )
}
