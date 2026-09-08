import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { NativeSelectOption } from '@/components/ui/native-select'

import { adminApi } from '../admin-api'
import { useAdminMutation } from '../hooks/use-admin-mutation'
import { bindingSchema } from '../lib/schemas'
import type { Binding, BindingInput } from '../types'
import { Field, QueryState, SelectField } from './common'

export function BindingForm(props: {
  supplierId: number
  binding?: Binding
  onClose: () => void
}) {
  const { t } = useTranslation()
  const instances = useQuery({
    queryKey: ['supplier-admin', 'instances'],
    queryFn: adminApi.instances,
  })
  const form = useForm<BindingInput>({
    resolver: zodResolver(bindingSchema),
    defaultValues: {
      instance_id: props.binding?.instance_id ?? 0,
      identifier: '',
      password: '',
      enabled: props.binding?.enabled ?? true,
    },
  })
  const mutation = useAdminMutation(
    (data: BindingInput) =>
      adminApi.saveBinding(props.supplierId, data, props.binding?.id),
    () => {
      form.reset()
      toast.success(t('supplier.saved'))
      props.onClose()
    }
  )
  const submit = (data: BindingInput) => {
    const keep =
      !!props.binding &&
      props.binding.instance_id === data.instance_id &&
      !data.identifier &&
      !data.password
    if (!keep && (!data.identifier || !data.password)) {
      if (!data.identifier) form.setError('identifier', { type: 'required' })
      if (!data.password) form.setError('password', { type: 'required' })
      return
    }
    mutation.mutate(data)
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) props.onClose()
      }}
    >
      <DialogContent>
        <DialogTitle>
          {props.binding ? t('supplier.edit') : t('supplier.newBinding')}
        </DialogTitle>
        <QueryState
          pending={instances.isPending}
          error={instances.error}
          retry={() => void instances.refetch()}
        >
          <form className='grid gap-4' onSubmit={form.handleSubmit(submit)}>
            <SelectField
              id='binding-instance'
              label={t('supplier.instance')}
              value={form.watch('instance_id')}
              onChange={(value) => form.setValue('instance_id', Number(value))}
            >
              <NativeSelectOption value={0}>
                {t('supplier.none')}
              </NativeSelectOption>
              {instances.data
                ?.filter((instance) => instance.kind === 'claude_gateway')
                .map((instance) => (
                  <NativeSelectOption key={instance.id} value={instance.id}>
                    {instance.name}
                  </NativeSelectOption>
                ))}
            </SelectField>
            {form.formState.errors.instance_id && (
              <p role='alert' className='text-destructive text-xs'>
                {t('supplier.required')}
              </p>
            )}
            <Field
              id='binding-identifier'
              label={t('supplier.identifier')}
              error={!!form.formState.errors.identifier}
            >
              <Input
                id='binding-identifier'
                autoComplete='off'
                placeholder={
                  props.binding ? t('supplier.keepCredentials') : undefined
                }
                {...form.register('identifier')}
              />
            </Field>
            <Field
              id='binding-password'
              label={t('supplier.password')}
              error={!!form.formState.errors.password}
            >
              <Input
                id='binding-password'
                type='password'
                autoComplete='new-password'
                placeholder={
                  props.binding ? t('supplier.keepCredentials') : undefined
                }
                {...form.register('password')}
              />
            </Field>
            <label className='flex items-center gap-2 text-sm'>
              <input type='checkbox' {...form.register('enabled')} />
              {t('supplier.enabled')}
            </label>
            <Button type='submit' disabled={mutation.isPending}>
              {t('supplier.save')}
            </Button>
          </form>
        </QueryState>
      </DialogContent>
    </Dialog>
  )
}
