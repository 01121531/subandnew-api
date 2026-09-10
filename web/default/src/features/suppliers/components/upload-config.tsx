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
      noValidate
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
        <fieldset className='grid min-w-0 gap-3'>
          <legend className='mb-3 text-sm font-semibold'>
            {t('supplier.addMethod')}
          </legend>
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
            {(['login', 'setup_token', 'rt', 'sk'] as const).map((method) => (
              <label key={method} className='relative min-w-0 cursor-pointer'>
                <input
                  type='radio'
                  name='upload-method'
                  value={method}
                  checked={props.method === method}
                  className='peer absolute inset-0 z-10 size-full cursor-pointer opacity-0 disabled:cursor-not-allowed'
                  onChange={() => {
                    form.resetField('refresh_token')
                    form.resetField('access_token')
                    form.resetField('session_keys_text')
                    props.onMethodChange(method)
                  }}
                />
                <span className='peer-checked:border-primary peer-checked:bg-primary/5 peer-checked:text-primary peer-focus-visible:ring-primary flex min-h-11 items-center justify-center rounded-md border px-2 py-2 text-center text-sm font-medium peer-focus-visible:ring-2 peer-focus-visible:ring-offset-2 peer-disabled:opacity-50'>
                  {t(`supplier.method_${method}`)}
                </span>
              </label>
            ))}
          </div>
        </fieldset>
        <fieldset className='supplier-form-section'>
          <legend>{t('supplier.ui_accountInfo')}</legend>
          <Field
            id='upload-name'
            label={t(
              props.method === 'sk' ? 'supplier.namePrefix' : 'supplier.name'
            )}
            error={form.formState.errors.name && t('supplier.ui_nameInvalid')}
          >
            <Input id='upload-name' maxLength={64} {...form.register('name')} />
          </Field>
          {props.method === 'rt' && (
            <div className='grid gap-4 sm:grid-cols-2'>
              <Field
                id='upload-rt'
                label={t('supplier.refreshToken')}
                error={
                  form.formState.errors.refresh_token &&
                  t('supplier.ui_tokenInvalid')
                }
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
                error={
                  form.formState.errors.access_token &&
                  t('supplier.ui_tokenInvalid')
                }
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
            <Field
              id='upload-sk'
              label={t('supplier.sessionKeys')}
              error={
                form.formState.errors.session_keys_text &&
                t('supplier.sessionKeysInvalid')
              }
            >
              <Textarea
                id='upload-sk'
                autoComplete='off'
                spellCheck={false}
                rows={4}
                className='max-h-48 resize-y font-mono text-xs'
                {...form.register('session_keys_text')}
              />
              <p aria-live='polite' className='text-muted-foreground text-xs'>
                {t('supplier.ui_keyCount', {
                  count: parseSessionKeys(form.watch('session_keys_text'))
                    .length,
                })}
              </p>
            </Field>
          )}
        </fieldset>
        <fieldset className='supplier-form-section'>
          <legend>{t('supplier.ui_connection')}</legend>
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
                  value === 'manual'
                    ? (props.options.proxies[0]?.id ?? '')
                    : '',
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
                  inputRef={form.register('outbound_proxy_id').ref}
                  error={
                    form.formState.errors.outbound_proxy_id &&
                    t('supplier.ui_proxyRequired')
                  }
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
                  <p
                    role='status'
                    className='text-muted-foreground mt-2 text-xs'
                  >
                    {t('supplier.noBindableProxies')}
                  </p>
                )}
              </div>
            )}
          </div>
        </fieldset>
        <fieldset className='supplier-form-section'>
          <legend>{t('supplier.ui_templates')}</legend>
          <div className='grid gap-4 sm:grid-cols-2'>
            <SelectField
              id='upload-policy'
              label={t('supplier.policy')}
              value={form.watch('policy_template_id')}
              onChange={(value) => {
                form.setValue('policy_template_id', value, {
                  shouldDirty: true,
                })
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
          <fieldset className='grid min-w-0 gap-3'>
            <legend className='text-sm font-medium'>
              {t('supplier.groups')}
            </legend>
            <div className='grid max-h-48 grid-cols-1 gap-2 overflow-y-auto sm:grid-cols-2'>
              {props.options.groups.map((group) => (
                <label
                  key={group.id}
                  className='hover:bg-muted/50 flex min-h-10 min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 text-sm'
                >
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
                  <span className='min-w-0 break-words'>{group.name}</span>
                </label>
              ))}
            </div>
          </fieldset>
        </fieldset>
        <fieldset className='supplier-form-section'>
          <legend>{t('supplier.ui_capacity')}</legend>
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
                error={
                  form.formState.errors[key] &&
                  t('supplier.ui_limitInvalid', { max: limitMax(key) })
                }
              >
                <Input
                  id={`upload-${key}`}
                  type='number'
                  min={0}
                  max={limitMax(key).replaceAll(',', '')}
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
      </fieldset>
    </form>
  )
}

function limitMax(key: string): string {
  if (key === 'max_rpm') return '1,000,000'
  if (key === 'max_tpm') return '1,000,000,000'
  return '100,000'
}
