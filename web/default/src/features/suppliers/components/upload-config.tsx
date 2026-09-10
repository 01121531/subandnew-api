import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { Input } from '@/components/ui/input'
import { NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { useSupplierUploadPreferences } from '@/stores/supplier-upload-preferences'

import { parseSessionKeys, uploadFormSchema } from '../lib/import-input'
import { policyValues } from '../lib/policy'
import { uploadDefaults } from '../lib/upload-defaults'
import type {
  AccountImportCredentials,
  UploadInput,
  UploadMethod,
  UploadOptions,
} from '../types'
import { Field, SelectField } from './common'

export function UploadConfig(props: {
  bindingId: number
  supplierId: number
  options: UploadOptions
  pending: boolean
  method: UploadMethod
  initial?: UploadInput
  onMethodChange: (method: UploadMethod) => void
  onDirtyChange: (dirty: boolean) => void
  onSubmit: (data: UploadInput, credentials: AccountImportCredentials) => void
}) {
  const { t } = useTranslation()
  const schema = uploadFormSchema(props.method)
  const form = useForm<
    z.input<typeof schema>,
    unknown,
    z.output<typeof schema>
  >({
    resolver: zodResolver(schema),
    defaultValues: {
      ...uploadDefaults(
        props.bindingId,
        props.options,
        useSupplierUploadPreferences.getState().choices?.[
          `${props.supplierId}:${props.bindingId}`
        ]
      ),
      ...props.initial,
      refresh_token: '',
      access_token: '',
      session_keys_text: '',
    },
  })
  const dirty = form.formState.isDirty
  const onDirtyChange = props.onDirtyChange
  useEffect(() => onDirtyChange(dirty), [dirty, onDirtyChange])
  const mode = form.watch('outbound_proxy_mode')
  const groups = form.watch('group_ids')
  return (
    <form
      id='supplier-upload-config'
      onSubmit={form.handleSubmit(
        ({ refresh_token, access_token, session_keys_text, ...data }) => {
          let credentials: AccountImportCredentials = {}
          if (props.method === 'rt') {
            credentials = {
              refresh_token: refresh_token.trim(),
              ...(access_token.trim()
                ? { access_token: access_token.trim() }
                : {}),
            }
          } else if (props.method === 'sk') {
            credentials = { session_keys: parseSessionKeys(session_keys_text) }
          }
          form.resetField('refresh_token')
          form.resetField('access_token')
          form.resetField('session_keys_text')
          props.onSubmit(data, credentials)
        }
      )}
      className='grid min-w-0 gap-5'
    >
      <fieldset disabled={props.pending} className='grid min-w-0 gap-5'>
        <SelectField
          id='upload-method'
          label={t('supplier.addMethod')}
          value={props.method}
          onChange={(method) => {
            if (!['login', 'setup_token', 'rt', 'sk'].includes(method)) return
            form.resetField('refresh_token')
            form.resetField('access_token')
            form.resetField('session_keys_text')
            props.onMethodChange(method as UploadMethod)
          }}
        >
          {(['login', 'setup_token', 'rt', 'sk'] as const).map((method) => (
            <NativeSelectOption key={method} value={method}>
              {t(`supplier.method_${method}`)}
            </NativeSelectOption>
          ))}
        </SelectField>
        <Field
          id='upload-name'
          label={t(
            props.method === 'sk' ? 'supplier.namePrefix' : 'supplier.name'
          )}
          error={!!form.formState.errors.name}
        >
          <Input id='upload-name' maxLength={64} {...form.register('name')} />
        </Field>
        {props.method === 'rt' && (
          <div className='grid gap-4 sm:grid-cols-2'>
            <Field
              id='upload-rt'
              label={t('supplier.refreshToken')}
              error={!!form.formState.errors.refresh_token}
            >
              <Input
                id='upload-rt'
                type='password'
                autoComplete='off'
                spellCheck={false}
                {...form.register('refresh_token')}
              />
            </Field>
            <Field
              id='upload-at'
              label={t('supplier.accessTokenOptional')}
              error={!!form.formState.errors.access_token}
            >
              <Input
                id='upload-at'
                type='password'
                autoComplete='off'
                spellCheck={false}
                {...form.register('access_token')}
              />
            </Field>
          </div>
        )}
        {props.method === 'sk' && (
          <Field id='upload-sk' label={t('supplier.sessionKeys')}>
            <Textarea
              id='upload-sk'
              autoComplete='off'
              spellCheck={false}
              rows={4}
              className='max-h-48 resize-y font-mono text-xs'
              {...form.register('session_keys_text')}
            />
            {form.formState.errors.session_keys_text && (
              <p role='alert' className='text-destructive text-xs'>
                {t('supplier.sessionKeysInvalid')}
              </p>
            )}
          </Field>
        )}
        <div className='grid gap-4 sm:grid-cols-2'>
          <SelectField
            id='upload-mode'
            label={t('supplier.proxyMode')}
            value={mode}
            onChange={(value) => {
              if (
                value === 'direct' ||
                value === 'manual' ||
                value === 'auto'
              ) {
                form.setValue('outbound_proxy_mode', value, {
                  shouldDirty: true,
                })
              }
              form.setValue(
                'outbound_proxy_id',
                value === 'manual' ? (props.options.proxies[0]?.id ?? '') : '',
                { shouldDirty: true }
              )
            }}
          >
            <NativeSelectOption value='direct'>
              {t('supplier.direct')}
            </NativeSelectOption>
            <NativeSelectOption value='manual'>
              {t('supplier.proxy')}
            </NativeSelectOption>
            <NativeSelectOption value='auto'>
              {t('supplier.auto')}
            </NativeSelectOption>
          </SelectField>
          {mode === 'manual' && (
            <div>
              <SelectField
                id='upload-proxy'
                label={t('supplier.proxy')}
                value={form.watch('outbound_proxy_id')}
                onChange={(value) =>
                  form.setValue('outbound_proxy_id', value, {
                    shouldDirty: true,
                  })
                }
              >
                <NativeSelectOption value=''>
                  {t('supplier.none')}
                </NativeSelectOption>
                {props.options.proxies.map((item) => (
                  <NativeSelectOption key={item.id} value={item.id}>
                    {item.name} ({item.host}:{item.port})
                  </NativeSelectOption>
                ))}
              </SelectField>
              {!props.options.proxies.length && (
                <p role='status' className='text-muted-foreground mt-2 text-xs'>
                  {t('supplier.noBindableProxies')}
                </p>
              )}
              {form.formState.errors.outbound_proxy_id && (
                <p role='alert' className='text-destructive text-xs'>
                  {t('supplier.required')}
                </p>
              )}
            </div>
          )}
          <SelectField
            id='upload-policy'
            label={t('supplier.policy')}
            value={form.watch('policy_template_id')}
            onChange={(value) => {
              form.setValue('policy_template_id', value, { shouldDirty: true })
              const template = props.options.policies.find(
                (item) => item.id === value
              )
              if (template) {
                for (const [field, limit] of policyValues(template)) {
                  form.setValue(field, limit, {
                    shouldDirty: true,
                    shouldValidate: true,
                  })
                }
              }
            }}
          >
            <NativeSelectOption value=''>
              {t('supplier.none')}
            </NativeSelectOption>
            {props.options.policies.map((item) => (
              <NativeSelectOption key={item.id} value={item.id}>
                {item.name}
              </NativeSelectOption>
            ))}
          </SelectField>
          <SelectField
            id='upload-template'
            label={t('supplier.template')}
            value={form.watch('cc_template_id')}
            onChange={(value) =>
              form.setValue('cc_template_id', value, { shouldDirty: true })
            }
          >
            <NativeSelectOption value=''>
              {t('supplier.none')}
            </NativeSelectOption>
            {props.options.templates.map((item) => (
              <NativeSelectOption key={item.id} value={item.id}>
                {item.name}
              </NativeSelectOption>
            ))}
          </SelectField>
        </div>
        <fieldset className='grid gap-3 border-y py-4'>
          <legend className='text-sm font-medium'>
            {t('supplier.groups')}
          </legend>
          <div className='flex flex-wrap gap-4'>
            {props.options.groups.map((group) => (
              <label key={group.id} className='flex items-center gap-2 text-sm'>
                <input
                  type='checkbox'
                  checked={groups.includes(group.id)}
                  onChange={(event) =>
                    form.setValue(
                      'group_ids',
                      event.target.checked
                        ? [...groups, group.id]
                        : groups.filter((id) => id !== group.id),
                      { shouldDirty: true }
                    )
                  }
                />
                {group.name}
              </label>
            ))}
          </div>
        </fieldset>
        <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
          {(
            [
              ['max_rpm', 'maxRpm'],
              ['max_tpm', 'maxTpm'],
              ['max_concurrent', 'maxConcurrent'],
              ['max_sessions', 'maxSessions'],
            ] as const
          ).map(([key, label]) => (
            <Field
              key={key}
              id={`upload-${key}`}
              label={t(`supplier.${label}`)}
              error={!!form.formState.errors[key]}
            >
              <Input
                id={`upload-${key}`}
                type='number'
                min={0}
                step={1}
                {...form.register(key, {
                  setValueAs: (value: string | number) =>
                    value === '' ? '' : Number(value),
                })}
              />
            </Field>
          ))}
        </div>
      </fieldset>
    </form>
  )
}
