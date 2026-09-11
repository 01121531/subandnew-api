import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { useSupplierUploadPreferences } from '@/stores/supplier-upload-preferences'

import { parseSessionKeys, uploadFormSchema } from '../lib/import-input'
import { emptyNaming, uploadName, uploadNameError } from '../lib/naming'
import { policyValues } from '../lib/policy'
import { uploadDefaults } from '../lib/upload-defaults'
import type {
  AccountImportCredentials,
  UploadInput,
  UploadMethod,
  UploadOptions,
} from '../types'
import { Field } from './common'
import { UploadChoiceField } from './upload-choice-field'

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
  const naming = props.options.effective_naming ?? emptyNaming
  const skBlocked = !!naming.suffix
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
          const error = uploadNameError(naming, data.name, props.method)
          if (error) {
            form.setError('name', { message: t(error) }, { shouldFocus: true })
            return
          }
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
          props.onSubmit(
            {
              ...data,
              naming_revision: props.options.effective_naming?.version,
            },
            credentials
          )
        }
      )}
      className='grid min-w-0 gap-5'
    >
      <fieldset disabled={props.pending} className='grid min-w-0 gap-5'>
        <UploadChoiceField
          id='upload-method'
          label={t('supplier.addMethod')}
          layout='methods'
          options={(['login', 'setup_token', 'rt', 'sk'] as const).map(
            (method) => ({
              value: method,
              label: t(`supplier.method_${method}`),
              disabled: method === 'sk' && skBlocked,
            })
          )}
          value={props.method}
          disabled={props.pending}
          onChange={(method) => {
            if (method === 'sk' && skBlocked) return
            form.resetField('refresh_token')
            form.resetField('access_token')
            form.resetField('session_keys_text')
            props.onMethodChange(method as UploadMethod)
          }}
        />
        {skBlocked && (
          <p role='status' className='text-muted-foreground text-xs'>
            {t('supplier.namingSkUnavailable')}
          </p>
        )}
        <fieldset className='supplier-form-section'>
          <legend>{t('supplier.ui_accountInfo')}</legend>
          <Field
            id='upload-name'
            label={t(
              props.method === 'sk' ? 'supplier.namePrefix' : 'supplier.name'
            )}
            error={
              form.formState.errors.name &&
              (form.formState.errors.name.type === 'manual'
                ? form.formState.errors.name.message
                : t('supplier.ui_nameInvalid'))
            }
          >
            <Input id='upload-name' {...form.register('name')} />
          </Field>
          {(naming.prefix || naming.suffix) && (
            <div className='bg-muted/50 grid min-w-0 gap-2 rounded-lg p-3'>
              <div className='grid gap-2 text-xs sm:grid-cols-2'>
                <div>
                  {t('supplier.naming_prefix')}
                  <span className='ml-2 font-mono [overflow-wrap:anywhere]'>
                    {naming.prefix || t('supplier.none')}
                  </span>
                </div>
                <div>
                  {t('supplier.naming_suffix')}
                  <span className='ml-2 font-mono [overflow-wrap:anywhere]'>
                    {naming.suffix || t('supplier.none')}
                  </span>
                </div>
              </div>
              <span className='text-muted-foreground text-xs'>
                {t(
                  props.method === 'sk'
                    ? 'supplier.namingBatchPreview'
                    : 'supplier.namingPreview'
                )}
              </span>
              <output
                aria-label={t('supplier.namingPreview')}
                className='font-mono text-sm [overflow-wrap:anywhere]'
              >
                {uploadName(naming, form.watch('name'))}
              </output>
              {[...uploadName(naming, form.watch('name'))].length > 64 && (
                <p role='alert' className='text-destructive text-xs'>
                  {t('supplier.namingNameTooLong')}
                </p>
              )}
            </div>
          )}
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
          <div className='grid min-w-0 gap-5'>
            <UploadChoiceField
              id='upload-mode'
              label={t('supplier.proxyMode')}
              layout='modes'
              disabled={props.pending}
              options={[
                { value: 'direct', label: t('supplier.direct') },
                { value: 'manual', label: t('supplier.proxy') },
                { value: 'auto', label: t('supplier.auto') },
              ]}
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
            />
            {mode === 'manual' && (
              <div>
                <UploadChoiceField
                  id='upload-proxy'
                  label={t('supplier.proxy')}
                  disabled={props.pending}
                  emptyLabel={t('supplier.ui_proxyUnselected')}
                  options={props.options.proxies.map((item) => ({
                    value: item.id,
                    label: `${item.name} (${item.host}:${item.port})`,
                  }))}
                  inputRef={form.register('outbound_proxy_id').ref}
                  error={
                    form.formState.errors.outbound_proxy_id &&
                    t('supplier.ui_proxyRequired')
                  }
                  value={form.watch('outbound_proxy_id')}
                  onChange={(value) =>
                    form.setValue('outbound_proxy_id', value, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                />
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
          <div className='grid min-w-0 gap-5'>
            <UploadChoiceField
              id='upload-policy'
              label={t('supplier.policy')}
              disabled={props.pending}
              emptyLabel={t('supplier.none')}
              options={props.options.policies.map((item) => ({
                value: item.id,
                label: item.name,
              }))}
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
            />
            <UploadChoiceField
              id='upload-template'
              label={t('supplier.template')}
              disabled={props.pending}
              emptyLabel={t('supplier.none')}
              options={props.options.templates.map((item) => ({
                value: item.id,
                label: item.name,
              }))}
              value={form.watch('cc_template_id')}
              onChange={(value) =>
                form.setValue('cc_template_id', value, { shouldDirty: true })
              }
            />
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
