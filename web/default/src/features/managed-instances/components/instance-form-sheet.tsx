/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { LoaderCircle, ScanSearch } from 'lucide-react'
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
  FormDescription,
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
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

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
  kind: 'generic',
  base_url: '',
  environment: 'production',
  management_mode: 'observe',
  tls_verify: true,
  request_timeout_seconds: 10,
  check_interval_seconds: 60,
  alert_failure_threshold: 0,
  labels: '',
  auth_type: 'account_password',
  access_scope: 'admin',
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

function normalizedBaseURL(value: string) {
  const trimmed = value.trim()
  return /^https?:\/\//i.test(trimmed) ? trimmed : `https://${trimmed}`
}

function managedInstanceEnvironment(
  values: ManagedInstanceFormValues,
  baseURL: string,
  autoDetect: boolean
) {
  if (!autoDetect) return values.environment
  return baseURL.startsWith('http://') ? 'development' : 'production'
}

function submitLabel(editing: boolean, submitting: boolean) {
  if (submitting) return editing ? 'Saving...' : 'Detecting...'
  return editing ? 'Save' : 'Detect and add'
}

function createInstanceErrorMessage(error: unknown) {
  const message = (error as { response?: { data?: { message?: string } } })
    .response?.data?.message
  switch (message) {
    case 'managed instance already exists':
      return 'This instance already exists. Refresh the instance list.'
    case 'target_blocked':
      return 'The target address is blocked by the outbound security policy.'
    case 'authentication_failed':
      return 'Invalid account or password.'
    case 'permission_denied':
      return 'The account can sign in but does not have administrator permissions. Select regular account data or use an administrator account.'
    case 'collection_failed':
      return 'Connection detection failed. Check the site address, network, and TLS certificate.'
    case 'tls_verification_failed':
      return 'The target TLS certificate is not trusted. Install a valid certificate before connecting.'
    case 'tls_failed':
      return 'The site URL protocol does not match the target service.'
    case 'dns_failed':
      return 'The target hostname could not be resolved by the control-plane server.'
    case 'network_failed':
      return 'The control-plane server could not reach the target host or port.'
    case 'two_factor_required':
      return 'This administrator account requires two-factor authentication.'
    case 'invalid_response':
      return 'The target did not return a supported New API or Sub2API response.'
    default:
      return message || 'Failed to add instance'
  }
}

function toInput(
  values: ManagedInstanceFormValues,
  autoDetect = false
): ManagedInstanceInput {
  const baseURL = normalizedBaseURL(values.base_url)
  const input: ManagedInstanceInput = {
    name: values.name.trim(),
    kind: autoDetect ? 'generic' : values.kind,
    base_url: baseURL,
    environment: managedInstanceEnvironment(values, baseURL, autoDetect),
    labels: autoDetect ? {} : textToLabels(values.labels),
    management_mode: autoDetect ? 'observe' : values.management_mode,
    tls_verify: autoDetect ? true : values.tls_verify,
    request_timeout_seconds: autoDetect ? 10 : values.request_timeout_seconds,
    check_interval_seconds: autoDetect ? 60 : values.check_interval_seconds,
    alert_failure_threshold: autoDetect ? 0 : values.alert_failure_threshold,
  }
  if (values.secret.trim()) {
    input.credential = {
      auth_type: autoDetect ? 'account_password' : values.auth_type,
      access_scope: values.access_scope,
      secret: values.secret,
      user_id: values.user_id.trim(),
      expires_at: 0,
    }
  }
  return input
}

export function InstanceFormSheet(props: InstanceFormSheetProps) {
  const { t } = useTranslation()
  const isRoot =
    useAuthStore((state) => state.auth.user?.role) === ROLE.SUPER_ADMIN
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
      alert_failure_threshold: props.instance.alert_failure_threshold,
      labels: labelsToText(props.instance.labels),
      auth_type: props.instance.credential?.auth_type || 'bearer_pat',
      access_scope: props.instance.credential?.access_scope || 'admin',
      secret: '',
      user_id: '',
    })
  }, [form, props.instance, props.open])

  const onSubmit = async (values: ManagedInstanceFormValues) => {
    if (!props.instance) {
      if (!values.user_id.trim()) {
        form.setError('user_id', { message: t('Account is required') })
        return
      }
      if (!values.secret) {
        form.setError('secret', { message: t('Password is required') })
        return
      }
    }
    setSubmitting(true)
    try {
      const input = toInput(values, !props.instance)
      const response = props.instance
        ? await updateManagedInstance(props.instance.id, input)
        : await createManagedInstance(input)
      if (!response.success) return
      toast.success(t(props.instance ? 'Instance updated' : 'Instance added'))
      props.onOpenChange(false)
      props.onSaved()
    } catch (error) {
      if (!props.instance) toast.error(t(createInstanceErrorMessage(error)))
    } finally {
      form.setValue('secret', '', { shouldDirty: false })
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
            {t(
              props.instance
                ? 'Configure the control-plane connection and probe policy.'
                : 'Enter the site address and administrator account. The system type and capabilities will be detected automatically.'
            )}
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
              {props.instance && (
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
              )}
              {props.instance && (
                <FormField
                  control={form.control}
                  name='management_mode'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Management mode')}</FormLabel>
                      <NativeSelect
                        className='w-full'
                        {...field}
                        disabled={!isRoot}
                      >
                        <NativeSelectOption value='observe'>
                          {t('Observe')}
                        </NativeSelectOption>
                        {isRoot && (
                          <>
                            <NativeSelectOption value='operate'>
                              {t('Operate')}
                            </NativeSelectOption>
                            <NativeSelectOption value='enforce'>
                              {t('Enforce')}
                            </NativeSelectOption>
                          </>
                        )}
                      </NativeSelect>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
              <FormField
                control={form.control}
                name='base_url'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Site URL')}</FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        type='text'
                        inputMode='url'
                        placeholder='api.example.com'
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              {props.instance && (
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
              )}
            </SideDrawerSection>

            {props.instance && (
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
                  name='alert_failure_threshold'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Alert after consecutive failures')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          min={0}
                          max={100}
                          onChange={(event) =>
                            field.onChange(event.target.valueAsNumber)
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Use 0 to inherit the global email setting.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
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
            )}

            {!props.instance && (
              <SideDrawerSection>
                <h3 className='text-sm font-medium'>{t('Site account')}</h3>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Use an administrator or regular account. Credentials are encrypted and only used by the control plane.'
                  )}
                </p>
                <FormField
                  control={form.control}
                  name='access_scope'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Account permissions')}</FormLabel>
                      <FormControl>
                        <NativeSelect className='w-full' {...field}>
                          <NativeSelectOption value='admin'>
                            {t('Administrator account (site-wide data)')}
                          </NativeSelectOption>
                          <NativeSelectOption value='user'>
                            {t('Regular account (own data only)')}
                          </NativeSelectOption>
                        </NativeSelect>
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='user_id'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Account')}</FormLabel>
                      <FormControl>
                        <Input {...field} autoComplete='username' />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='secret'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Password')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='password'
                          autoComplete='current-password'
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
            {submitting && <LoaderCircle className='animate-spin' />}
            {!submitting && !props.instance && <ScanSearch />}
            {t(submitLabel(Boolean(props.instance), submitting))}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
