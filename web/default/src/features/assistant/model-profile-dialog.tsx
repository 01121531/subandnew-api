/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useMemo } from 'react'
import { type Resolver, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
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
import { Switch } from '@/components/ui/switch'

import type { AssistantModelProfile, AssistantModelProfileInput } from './types'

type ModelProfileDialogProps = {
  open: boolean
  profile: AssistantModelProfile | null
  pending: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (input: AssistantModelProfileInput) => void
}

const defaultValues: AssistantModelProfileInput = {
  name: '',
  provider: 'openai_compatible',
  base_url: 'https://api.openai.com/v1',
  model: '',
  api_key: '',
  timeout_seconds: 30,
  max_output_tokens: 2048,
  enabled: true,
  is_primary: false,
}

export function ModelProfileDialog(props: ModelProfileDialogProps) {
  const { t } = useTranslation()
  const schema = useMemo(
    () =>
      z.object({
        name: z.string().trim().min(1, t('Profile name is required')).max(100),
        provider: z.literal('openai_compatible'),
        base_url: z.url(t('Enter a valid API base URL')),
        model: z.string().trim().min(1, t('Model name is required')).max(160),
        api_key: z.string(),
        timeout_seconds: z.number().int().min(1).max(120),
        max_output_tokens: z.number().int().min(1).max(32768),
        enabled: z.boolean(),
        is_primary: z.boolean(),
      }),
    [t]
  )
  const form = useForm<AssistantModelProfileInput>({
    resolver: zodResolver(schema) as Resolver<AssistantModelProfileInput>,
    defaultValues,
  })

  useEffect(() => {
    if (!props.open) return
    if (!props.profile) {
      form.reset(defaultValues)
      return
    }
    form.reset({
      name: props.profile.name,
      provider: props.profile.provider,
      base_url: props.profile.base_url,
      model: props.profile.model,
      api_key: '',
      timeout_seconds: props.profile.timeout_seconds,
      max_output_tokens: props.profile.max_output_tokens,
      enabled: props.profile.enabled,
      is_primary: props.profile.is_primary,
    })
  }, [form, props.open, props.profile])

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={props.profile ? t('Edit model profile') : t('Add model profile')}
      description={t(
        'Configure an OpenAI-compatible endpoint. Secrets are encrypted on the server.'
      )}
      contentHeight='min(34rem, calc(100vh - 14rem))'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            className='min-h-11'
            disabled={props.pending}
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='assistant-model-profile-form'
            className='min-h-11'
            disabled={props.pending}
          >
            {props.pending ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id='assistant-model-profile-form'
          className='space-y-5'
          onSubmit={form.handleSubmit(props.onSubmit)}
        >
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='name'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Profile name')}</FormLabel>
                  <FormControl>
                    <Input
                      className='min-h-11'
                      placeholder={t('Production model')}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='model'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Model')}</FormLabel>
                  <FormControl>
                    <Input
                      className='min-h-11'
                      placeholder='gpt-4.1-mini'
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='base_url'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('API base URL')}</FormLabel>
                <FormControl>
                  <Input className='min-h-11' inputMode='url' {...field} />
                </FormControl>
                <FormDescription>
                  {t('The endpoint must use HTTP or HTTPS.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='api_key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('API key')}</FormLabel>
                <FormControl>
                  <Input
                    className='min-h-11'
                    type='password'
                    autoComplete='new-password'
                    placeholder={
                      props.profile
                        ? t('Leave blank to keep the current key')
                        : t('Optional when the endpoint does not require a key')
                    }
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='timeout_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Timeout (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      className='min-h-11'
                      type='number'
                      min={1}
                      max={120}
                      value={field.value}
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
              name='max_output_tokens'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max output tokens')}</FormLabel>
                  <FormControl>
                    <Input
                      className='min-h-11'
                      type='number'
                      min={1}
                      max={32768}
                      value={field.value}
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

          <div className='grid gap-3 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <FormItem className='bg-muted/40 flex min-h-14 items-center justify-between gap-4 rounded-lg border p-3'>
                  <div>
                    <FormLabel>{t('Enabled')}</FormLabel>
                    <FormDescription>
                      {t('Allow the assistant to use this endpoint.')}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='is_primary'
              render={({ field }) => (
                <FormItem className='bg-muted/40 flex min-h-14 items-center justify-between gap-4 rounded-lg border p-3'>
                  <div>
                    <FormLabel>{t('Primary model')}</FormLabel>
                    <FormDescription>
                      {t('Use this profile by default.')}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />
          </div>
        </form>
      </Form>
    </Dialog>
  )
}
