import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'

import { createManagedInstance, updateManagedInstance } from '../api'
import {
  managedInstanceFormSchema,
  type ManagedInstanceFormValues,
} from '../form-schema'
import { MANAGED_INSTANCE_KINDS } from '../lib'
import type { ManagedInstance, ManagedInstanceInput } from '../types'

type InstanceFormSheetProps = {
  open: boolean
  instance: ManagedInstance | null
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}

const defaultValues: ManagedInstanceFormValues = {
  name: '',
  kind: 'new_api',
  base_url: '',
  environment: 'production',
  management_mode: 'observe',
  tls_verify: true,
  request_timeout_seconds: 10,
  check_interval_seconds: 60,
  labels: '',
  auth_type: 'bearer_pat',
  secret: '',
  user_id: '',
}

function labelsToText(labels: Record<string, string>): string {
  return Object.entries(labels)
    .map(([key, value]) => `${key}=${value}`)
    .join(', ')
}

function textToLabels(value: string): Record<string, string> {
  const labels: Record<string, string> = {}
  for (const item of value.split(',')) {
    const [key, ...rest] = item.split('=')
    if (key?.trim() && rest.length > 0) {
      labels[key.trim()] = rest.join('=').trim()
    }
  }
  return labels
}

function toInput(values: ManagedInstanceFormValues): ManagedInstanceInput {
  const input: ManagedInstanceInput = {
    name: values.name.trim(),
    kind: values.kind,
    base_url: values.base_url.trim(),
    environment: values.environment,
    labels: textToLabels(values.labels),
    management_mode: values.management_mode,
    tls_verify: values.tls_verify,
    request_timeout_seconds: values.request_timeout_seconds,
    check_interval_seconds: values.check_interval_seconds,
  }
  if (values.secret.trim()) {
    input.credential = {
      auth_type: values.auth_type,
      secret: values.secret,
      user_id: values.user_id.trim(),
      expires_at: 0,
    }
  }
  return input
}

export function InstanceFormSheet(props: InstanceFormSheetProps) {
  const { t } = useTranslation()
  const [submitting, setSubmitting] = useState(false)
  const form = useForm<ManagedInstanceFormValues>({
    resolver: zodResolver(managedInstanceFormSchema),
    defaultValues,
  })

  useEffect(() => {
    if (!props.open) return
    if (!props.instance) {
      form.reset(defaultValues)
      return
    }
    form.reset({
      name: props.instance.name,
      kind: props.instance.kind,
      base_url: props.instance.base_url,
      environment: props.instance
        .environment as ManagedInstanceFormValues['environment'],
      management_mode: props.instance.management_mode,
      tls_verify: props.instance.tls_verify,
      request_timeout_seconds: props.instance.request_timeout_seconds,
      check_interval_seconds: props.instance.check_interval_seconds,
      labels: labelsToText(props.instance.labels),
      auth_type: props.instance.credential?.auth_type || 'bearer_pat',
      secret: '',
      user_id: '',
    })
  }, [form, props.instance, props.open])

  const onSubmit = async (values: ManagedInstanceFormValues) => {
    setSubmitting(true)
    try {
      const input = toInput(values)
      const response = props.instance
        ? await updateManagedInstance(props.instance.id, input)
        : await createManagedInstance(input)
      if (!response.success) return
      form.setValue('secret', '')
      toast.success(t(props.instance ? 'Instance updated' : 'Instance added'))
      props.onOpenChange(false)
      props.onSaved()
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[560px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {t(props.instance ? 'Edit instance' : 'Add instance')}
          </SheetTitle>
          <SheetDescription>
            {t('Configure the control-plane connection and probe policy.')}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='managed-instance-form'
            className={sideDrawerFormClassName()}
            onSubmit={form.handleSubmit(onSubmit)}
          >
            <SideDrawerSection>
              <h3 className='text-sm font-medium'>{t('Connection')}</h3>
              <FormField
                control={form.control}
                name='name'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Name')}</FormLabel>
                    <FormControl>
                      <Input {...field} autoComplete='off' />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <div className='grid gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='kind'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Product')}</FormLabel>
                      <NativeSelect className='w-full' {...field}>
                        {MANAGED_INSTANCE_KINDS.map((kind) => (
                          <NativeSelectOption
                            key={kind.value}
                            value={kind.value}
                          >
                            {kind.label}
                          </NativeSelectOption>
                        ))}
                      </NativeSelect>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='environment'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Environment')}</FormLabel>
                      <NativeSelect className='w-full' {...field}>
                        <NativeSelectOption value='production'>
                          {t('Production')}
                        </NativeSelectOption>
                        <NativeSelectOption value='staging'>
                          {t('Staging')}
                        </NativeSelectOption>
                        <NativeSelectOption value='development'>
                          {t('Development')}
                        </NativeSelectOption>
                      </NativeSelect>
                    </FormItem>
                  )}
                />
              </div>
              <FormField
                control={form.control}
                name='base_url'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Base URL')}</FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        type='url'
                        placeholder='https://api.example.com'
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='tls_verify'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between rounded-lg border p-3'>
                    <FormLabel className='mb-0'>
                      {t('Verify TLS certificates')}
                    </FormLabel>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </SideDrawerSection>

            <SideDrawerSection>
              <h3 className='text-sm font-medium'>{t('Probe policy')}</h3>
              <div className='grid gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='request_timeout_seconds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Timeout (seconds)')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          min={1}
                          max={120}
                          onChange={(event) =>
                            field.onChange(event.target.valueAsNumber)
                          }
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='check_interval_seconds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Interval (seconds)')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          min={10}
                          max={86400}
                          onChange={(event) =>
                            field.onChange(event.target.valueAsNumber)
                          }
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
              <FormField
                control={form.control}
                name='labels'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Labels')}</FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        placeholder='region=cn-east, team=platform'
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SideDrawerSection>

            {!props.instance && (
              <SideDrawerSection>
                <h3 className='text-sm font-medium'>
                  {t('Management credential')}
                </h3>
                <div className='grid gap-4 sm:grid-cols-2'>
                  <FormField
                    control={form.control}
                    name='auth_type'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Authentication')}</FormLabel>
                        <NativeSelect className='w-full' {...field}>
                          <NativeSelectOption value='bearer_pat'>
                            Bearer PAT
                          </NativeSelectOption>
                          <NativeSelectOption value='admin_token'>
                            {t('Admin token')}
                          </NativeSelectOption>
                          <NativeSelectOption value='legacy_access_token'>
                            {t('Legacy access token')}
                          </NativeSelectOption>
                        </NativeSelect>
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='user_id'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Legacy user ID')}</FormLabel>
                        <FormControl>
                          <Input {...field} autoComplete='off' />
                        </FormControl>
                      </FormItem>
                    )}
                  />
                </div>
                <FormField
                  control={form.control}
                  name='secret'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Secret')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='password'
                          autoComplete='new-password'
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </SideDrawerSection>
            )}
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose
            render={<Button variant='outline' />}
            disabled={submitting}
          >
            {t('Cancel')}
          </SheetClose>
          <Button
            form='managed-instance-form'
            type='submit'
            disabled={submitting}
          >
            {submitting ? t('Saving...') : t('Save')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
