import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { Save } from 'lucide-react'
import { useState, type Ref } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { adminApi } from '../admin-api'
import { useAdminMutation } from '../hooks/use-admin-mutation'
import {
  useFormLeaveGuard,
  type FormLeaveGuard,
} from '../hooks/use-form-leave-guard'
import { emptyNaming, validNamingRule } from '../lib/naming'
import { bindingSchema } from '../lib/schemas'
import { SupplierRequestError } from '../portal-api'
import type {
  Binding,
  BindingInput,
  PolicyOverrides,
  NamingRule,
} from '../types'
import { Field, QueryState } from './common'
import { NamingEditor } from './naming-editor'
import { PolicyEditor } from './policy-editor'

export function BindingForm(props: {
  supplierId: number
  binding?: Binding
  guardRef: Ref<FormLeaveGuard>
  onPendingChange: (pending: boolean) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [naming, setNaming] = useState<NamingRule | null>(
    props.binding?.naming_override ?? null
  )
  const supplier = useQuery({
    queryKey: ['supplier-admin', 'supplier', props.supplierId],
    queryFn: () => adminApi.get(props.supplierId),
  })
  const [overrides, setOverrides] = useState<PolicyOverrides>(
    props.binding?.policy_overrides ?? {}
  )
  const instances = useQuery({
    queryKey: ['supplier-admin', 'instances'],
    queryFn: adminApi.instances,
  })
  const schema = bindingSchema
    .extend({
      instance_id: z.number().int().positive(t('supplier.ui_instanceRequired')),
      identifier: z
        .string()
        .trim()
        .refine(
          (value) => new TextEncoder().encode(value).length <= 256,
          t('supplier.ui_identifierInvalid')
        ),
      password: z
        .string()
        .refine(
          (value) =>
            new TextEncoder().encode(value).length <= 1024 &&
            value === value.trim(),
          t('supplier.ui_bindingPasswordInvalid')
        ),
    })
    .superRefine((data, context) => {
      const keep =
        !!props.binding &&
        props.binding.instance_id === data.instance_id &&
        !data.identifier &&
        !data.password
      if (keep) return
      if (!data.identifier) {
        context.addIssue({
          code: 'custom',
          path: ['identifier'],
          message: t('supplier.ui_credentialsRequired'),
        })
      }
      if (!data.password) {
        context.addIssue({
          code: 'custom',
          path: ['password'],
          message: t('supplier.ui_credentialsRequired'),
        })
      }
    })
  const form = useForm<BindingInput>({
    resolver: zodResolver(schema),
    defaultValues: {
      display_name: props.binding?.display_name ?? '',
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
  const guard = useFormLeaveGuard({
    dirty:
      form.formState.isDirty ||
      JSON.stringify(naming) !==
        JSON.stringify(props.binding?.naming_override ?? null) ||
      JSON.stringify(overrides) !==
        JSON.stringify(props.binding?.policy_overrides ?? {}),
    pending: mutation.isPending,
    guardRef: props.guardRef,
    onPendingChange: props.onPendingChange,
  })
  const submit = (data: BindingInput) => {
    if (mutation.isPending || !supplier.data?.effective_policy) return
    if (!validNamingRule(naming)) {
      document
        .querySelector<HTMLInputElement>('#binding-naming-prefix')
        ?.focus()
      return
    }
    mutation.mutate(
      {
        ...data,
        policy_overrides: overrides,
        policy_revision: props.binding?.effective_policy?.version,
        naming_override: naming,
        naming_revision: props.binding?.effective_naming?.version,
      },
      {
        onError: (error) => {
          if (
            error instanceof SupplierRequestError &&
            error.code === 'supplier_binding_conflict'
          ) {
            form.setError(
              'instance_id',
              { message: t('supplier.bindingConflict') },
              { shouldFocus: true }
            )
          }
        },
      }
    )
  }
  const options =
    instances.data?.filter((instance) => instance.kind === 'claude_gateway') ??
    []
  return (
    <>
      <form
        className='flex min-h-0 flex-1 flex-col'
        noValidate
        onSubmit={form.handleSubmit(submit)}
        aria-busy={mutation.isPending}
      >
        <div className='min-h-0 flex-1 overflow-y-auto p-4'>
          <h3 className='mb-4 text-sm font-semibold'>
            {props.binding
              ? t('supplier.editBinding')
              : t('supplier.newBinding')}
          </h3>
          <QueryState
            pending={instances.isPending}
            error={instances.error}
            hasData={instances.data !== undefined}
            retry={() => void instances.refetch()}
          >
            <fieldset
              disabled={mutation.isPending}
              className='grid min-w-0 gap-4'
            >
              <Field
                id='binding-instance'
                label={t('supplier.instance')}
                error={form.formState.errors.instance_id?.message}
              >
                <NativeSelect
                  id='binding-instance'
                  className='w-full'
                  aria-invalid={!!form.formState.errors.instance_id}
                  {...form.register('instance_id', { valueAsNumber: true })}
                >
                  <NativeSelectOption value={0}>
                    {t('supplier.none')}
                  </NativeSelectOption>
                  {props.binding &&
                    !options.some(
                      (instance) => instance.id === props.binding?.instance_id
                    ) && (
                      <NativeSelectOption value={props.binding.instance_id}>
                        {props.binding.instance_name}
                      </NativeSelectOption>
                    )}
                  {options.map((instance) => (
                    <NativeSelectOption key={instance.id} value={instance.id}>
                      {instance.name}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
              <Field
                id='binding-display-name'
                label={t('supplier.bindingDisplayName')}
                error={
                  form.formState.errors.display_name
                    ? t('supplier.portalTextInvalid')
                    : undefined
                }
              >
                <Input
                  id='binding-display-name'
                  aria-invalid={!!form.formState.errors.display_name}
                  {...form.register('display_name')}
                />
                <p className='text-muted-foreground text-xs'>
                  {t('supplier.bindingDisplayNameHint')}
                </p>
              </Field>
              <Field
                id='binding-identifier'
                label={t('supplier.identifier')}
                error={form.formState.errors.identifier?.message}
              >
                <Input
                  id='binding-identifier'
                  autoComplete='off'
                  aria-invalid={!!form.formState.errors.identifier}
                  placeholder={
                    props.binding ? t('supplier.keepCredentials') : undefined
                  }
                  {...form.register('identifier')}
                />
              </Field>
              <Field
                id='binding-password'
                label={t('supplier.password')}
                error={form.formState.errors.password?.message}
              >
                <Input
                  id='binding-password'
                  type='password'
                  autoComplete='new-password'
                  aria-invalid={!!form.formState.errors.password}
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
              <QueryState
                pending={supplier.isPending}
                error={supplier.error}
                retry={() => void supplier.refetch()}
              >
                {supplier.data?.effective_policy && (
                  <PolicyEditor
                    value={overrides}
                    parent={supplier.data.effective_policy}
                    level='binding'
                    disabled={mutation.isPending}
                    onChange={setOverrides}
                  />
                )}
              </QueryState>
              <NamingEditor
                id='binding-naming'
                value={naming}
                parent={supplier.data?.naming_rule ?? emptyNaming}
                disabled={mutation.isPending || !supplier.data}
                onChange={setNaming}
              />
            </fieldset>
          </QueryState>
        </div>
        <footer className='flex shrink-0 justify-end gap-2 border-t p-4'>
          <Button
            type='button'
            variant='outline'
            disabled={mutation.isPending}
            onClick={() => guard.requestLeave(props.onClose)}
          >
            {t('supplier.cancel')}
          </Button>
          <Button
            type='submit'
            disabled={
              mutation.isPending ||
              instances.data === undefined ||
              !supplier.data?.effective_policy
            }
          >
            <Save />
            {t('supplier.save')}
          </Button>
        </footer>
      </form>
      {guard.discardConfirmation}
    </>
  )
}
