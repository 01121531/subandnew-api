/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleAlert, ServerCog, UserRoundCog } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'

import {
  getAssistantDefaultInstanceSetting,
  listAssistantInstanceOptions,
  listMyAssistantIdentities,
  updateAssistantDefaultInstanceSetting,
  updateMyAssistantIdentityDefault,
} from './api'
import type {
  AssistantDefaultSource,
  AssistantIdentity,
  AssistantInstanceOption,
} from './types'

const assistantDefaultKeys = {
  global: ['assistant', 'default-instance'] as const,
  options: ['assistant', 'instance-options'] as const,
  mine: ['assistant', 'me', 'identities'] as const,
}

function sourceLabel(
  source: AssistantDefaultSource,
  t: (key: string) => string
) {
  if (source === 'personal') return t('Personal default')
  if (source === 'global') return t('Global default')
  return t('All permitted instances')
}

function instanceOptionLabel(option: AssistantInstanceOption) {
  return `${option.name} · ${option.kind} · ${option.status} · #${option.id}`
}

export function EffectiveDefault({
  identity,
}: {
  identity: AssistantIdentity
}) {
  const { t } = useTranslation()
  const effective = identity.effective_default_instance_id
    ? `${identity.effective_default_instance_name} · #${identity.effective_default_instance_id}`
    : t('All permitted instances')

  return (
    <div className='bg-muted/30 rounded-lg border px-3 py-2 text-sm'>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='text-muted-foreground'>{t('Effective scope')}</span>
        <span className='font-medium'>{effective}</span>
        <Badge variant='outline'>
          {sourceLabel(identity.default_source, t)}
        </Badge>
      </div>
      {identity.default_fallback && (
        <p className='text-warning mt-1 flex items-start gap-1.5 text-xs'>
          <CircleAlert
            className='mt-0.5 size-3.5 shrink-0'
            aria-hidden='true'
          />
          {t(
            'The configured default is no longer available. The fallback shown above is being used.'
          )}
        </p>
      )}
    </div>
  )
}

export function GlobalDefaultInstanceSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const settingQuery = useQuery({
    queryKey: assistantDefaultKeys.global,
    queryFn: getAssistantDefaultInstanceSetting,
  })
  const optionsQuery = useQuery({
    queryKey: assistantDefaultKeys.options,
    queryFn: listAssistantInstanceOptions,
  })
  const updateMutation = useMutation({
    mutationFn: updateAssistantDefaultInstanceSetting,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: assistantDefaultKeys.global,
        }),
        queryClient.invalidateQueries({ queryKey: assistantDefaultKeys.mine }),
        queryClient.invalidateQueries({
          queryKey: ['assistant', 'identities'],
        }),
      ])
      toast.success(t('Global default instance updated'))
    },
  })
  const currentID = settingQuery.data?.default_instance_id ?? null
  const currentIsMissing =
    currentID != null &&
    optionsQuery.isSuccess &&
    !optionsQuery.data.some((option) => option.id === currentID)

  return (
    <TitledCard
      title={t('Global default instance')}
      description={t(
        'Used when a bound identity has no personal default and the message does not name an instance.'
      )}
      icon={<ServerCog />}
      iconTone='info'
    >
      {(settingQuery.isLoading || optionsQuery.isLoading) && (
        <Skeleton className='h-20' aria-label={t('Loading...')} />
      )}
      {(settingQuery.isError || optionsQuery.isError) && (
        <ErrorState
          className='min-h-40'
          title={t('Could not load default instance settings')}
          description={t('Check the server connection and try again.')}
          onRetry={() => {
            void settingQuery.refetch()
            void optionsQuery.refetch()
          }}
        />
      )}
      {settingQuery.isSuccess && optionsQuery.isSuccess && (
        <div className='max-w-xl space-y-3'>
          <div className='space-y-1.5'>
            <Label htmlFor='assistant-global-default-instance'>
              {t('Default instance')}
            </Label>
            <NativeSelect
              id='assistant-global-default-instance'
              className='w-full'
              value={currentID == null ? 'none' : String(currentID)}
              disabled={updateMutation.isPending}
              onChange={(event) => {
                const value = event.target.value
                updateMutation.mutate(value === 'none' ? null : Number(value))
              }}
            >
              <NativeSelectOption value='none'>
                {t('Not set')}
              </NativeSelectOption>
              {currentIsMissing && (
                <NativeSelectOption value={String(currentID)} disabled>
                  {t('Unavailable instance #{{id}}', { id: currentID })}
                </NativeSelectOption>
              )}
              {optionsQuery.data.map((option) => (
                <NativeSelectOption key={option.id} value={String(option.id)}>
                  {instanceOptionLabel(option)}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          {currentIsMissing && (
            <Alert variant='destructive'>
              <CircleAlert />
              <AlertTitle>{t('Default instance unavailable')}</AlertTitle>
              <AlertDescription>
                {t('Choose another instance or clear the global default.')}
              </AlertDescription>
            </Alert>
          )}
          {currentID == null && (
            <p className='text-muted-foreground text-sm'>
              {t(
                'Without a global or personal default, the assistant queries all permitted instances.'
              )}
            </p>
          )}
        </div>
      )}
    </TitledCard>
  )
}

export function MyDefaultInstanceSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const identitiesQuery = useQuery({
    queryKey: assistantDefaultKeys.mine,
    queryFn: listMyAssistantIdentities,
  })
  const updateMutation = useMutation({
    mutationFn: (input: { identityID: number; instanceID: number | null }) =>
      updateMyAssistantIdentityDefault(input.identityID, input.instanceID),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: assistantDefaultKeys.mine,
      })
      toast.success(t('Personal default instance updated'))
    },
  })

  return (
    <TitledCard
      title={t('My default instance')}
      description={t(
        'Choose the instance the assistant uses when your message does not specify one.'
      )}
      icon={<UserRoundCog />}
      iconTone='success'
    >
      {identitiesQuery.isLoading && (
        <Skeleton className='h-32' aria-label={t('Loading...')} />
      )}
      {identitiesQuery.isError && (
        <ErrorState
          className='min-h-44'
          title={t('Could not load your assistant bindings')}
          description={t('Check the server connection and try again.')}
          onRetry={() => identitiesQuery.refetch()}
        />
      )}
      {identitiesQuery.isSuccess && identitiesQuery.data.length === 0 && (
        <Empty className='min-h-44 border'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <UserRoundCog />
            </EmptyMedia>
            <EmptyTitle>{t('No active WeChat binding')}</EmptyTitle>
            <EmptyDescription>
              {t(
                'Bind your web account in WeChat before choosing a personal default.'
              )}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {identitiesQuery.isSuccess && identitiesQuery.data.length > 0 && (
        <ul className='grid gap-3 lg:grid-cols-2'>
          {identitiesQuery.data.map((identity) => {
            const options = identity.instance_options ?? []
            const configuredID = identity.default_instance_id
            const configuredIsMissing =
              configuredID != null &&
              !options.some((option) => option.id === configuredID)
            return (
              <li
                key={identity.id}
                className='bg-muted/20 space-y-3 rounded-xl border p-4'
              >
                <div>
                  <p className='font-medium'>
                    {t('WeChat {{external}}', {
                      external: identity.external_user,
                    })}
                  </p>
                  <p className='text-muted-foreground text-xs'>
                    {t('Channel #{{id}}', { id: identity.channel_id })}
                  </p>
                </div>
                <div className='space-y-1.5'>
                  <Label htmlFor={`assistant-my-default-${identity.id}`}>
                    {t('Personal default')}
                  </Label>
                  <NativeSelect
                    id={`assistant-my-default-${identity.id}`}
                    className='w-full'
                    value={
                      configuredID == null ? 'inherit' : String(configuredID)
                    }
                    disabled={
                      updateMutation.isPending &&
                      updateMutation.variables?.identityID === identity.id
                    }
                    onChange={(event) => {
                      const value = event.target.value
                      updateMutation.mutate({
                        identityID: identity.id,
                        instanceID: value === 'inherit' ? null : Number(value),
                      })
                    }}
                  >
                    <NativeSelectOption value='inherit'>
                      {t('Inherit global default')}
                    </NativeSelectOption>
                    {configuredIsMissing && (
                      <NativeSelectOption value={String(configuredID)} disabled>
                        {t('Unavailable instance #{{id}}', {
                          id: configuredID,
                        })}
                      </NativeSelectOption>
                    )}
                    {options.map((option) => (
                      <NativeSelectOption
                        key={option.id}
                        value={String(option.id)}
                      >
                        {instanceOptionLabel(option)}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </div>
                <EffectiveDefault identity={identity} />
              </li>
            )
          })}
        </ul>
      )}
    </TitledCard>
  )
}
