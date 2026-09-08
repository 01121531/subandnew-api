import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelectOption } from '@/components/ui/native-select'

import { uploadSchema } from '../lib/schemas'
import type { UploadInput, UploadOptions } from '../types'
import { Field, SelectField } from './common'

export function UploadConfig(props: {
  bindingId: number
  options: UploadOptions
  pending: boolean
  onSubmit: (data: UploadInput) => void
}) {
  const { t } = useTranslation()
  const form = useForm<UploadInput>({
    resolver: zodResolver(uploadSchema),
    defaultValues: {
      binding_id: props.bindingId,
      name: '',
      outbound_proxy_mode: 'direct',
      outbound_proxy_id: '',
      group_ids: [],
      policy_template_id: '',
      cc_template_id: '',
      max_rpm: 0,
      max_tpm: 0,
      max_concurrent: 0,
      max_sessions: 0,
    },
  })
  const mode = form.watch('outbound_proxy_mode')
  const groups = form.watch('group_ids')
  return (
    <form
      onSubmit={form.handleSubmit(props.onSubmit)}
      className='grid max-w-3xl gap-5'
    >
      <Field
        id='upload-name'
        label={t('supplier.name')}
        error={!!form.formState.errors.name}
      >
        <Input id='upload-name' maxLength={64} {...form.register('name')} />
      </Field>
      <div className='grid gap-4 sm:grid-cols-2'>
        <SelectField
          id='upload-mode'
          label={t('supplier.proxyMode')}
          value={mode}
          onChange={(value) => {
            if (value === 'direct' || value === 'manual' || value === 'auto') {
              form.setValue('outbound_proxy_mode', value)
            }
            form.setValue('outbound_proxy_id', '')
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
              onChange={(value) => form.setValue('outbound_proxy_id', value)}
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
          onChange={(value) => form.setValue('policy_template_id', value)}
        >
          <NativeSelectOption value=''>{t('supplier.none')}</NativeSelectOption>
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
          onChange={(value) => form.setValue('cc_template_id', value)}
        >
          <NativeSelectOption value=''>{t('supplier.none')}</NativeSelectOption>
          {props.options.templates.map((item) => (
            <NativeSelectOption key={item.id} value={item.id}>
              {item.name}
            </NativeSelectOption>
          ))}
        </SelectField>
      </div>
      <fieldset className='grid gap-3 border-y py-4'>
        <legend className='text-sm font-medium'>{t('supplier.groups')}</legend>
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
                      : groups.filter((id) => id !== group.id)
                  )
                }
              />
              {group.name}
            </label>
          ))}
        </div>
      </fieldset>
      <div className='grid grid-cols-2 gap-4 sm:grid-cols-4'>
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
              {...form.register(key, { valueAsNumber: true })}
            />
          </Field>
        ))}
      </div>
      <Button
        className='justify-self-start'
        type='submit'
        disabled={props.pending}
      >
        {t('supplier.authorize')}
      </Button>
    </form>
  )
}
