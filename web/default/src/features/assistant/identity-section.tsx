/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { ShieldOff, UserRoundCheck, UsersRound } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
  listAssistantIdentities,
  listAssistantInstanceOptions,
  revokeAssistantIdentity,
  updateAssistantIdentityDefault,
} from './api'
import { EffectiveDefault } from './default-instance-section'
import type {
  AssistantIdentity,
  AssistantIdentityStatus,
  AssistantInstanceScope,
} from './types'

function identityStatusLabel(status: AssistantIdentityStatus, t: TFunction) {
  const labels: Record<AssistantIdentityStatus, string> = {
    pending: t('Pending'),
    active: t('Active'),
    disabled: t('Disabled'),
    revoked: t('Revoked'),
  }
  return labels[status]
}

function scopeLabel(
  scope: AssistantInstanceScope,
  count: number,
  t: TFunction
) {
  if (scope === 'all') return t('All permitted instances')
  if (scope === 'selected') {
    return t('{{count}} selected instances', { count })
  }
  return t('No instance access')
}

export function AssistantIdentitySection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [revokingIdentity, setRevokingIdentity] =
    useState<AssistantIdentity | null>(null)
  const identitiesQuery = useQuery({
    queryKey: ['assistant', 'identities'],
    queryFn: listAssistantIdentities,
  })
  const optionsQuery = useQuery({
    queryKey: ['assistant', 'instance-options'],
    queryFn: listAssistantInstanceOptions,
  })
  const revokeMutation = useMutation({
    mutationFn: revokeAssistantIdentity,
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['assistant', 'identities'],
      })
      toast.success(t('User binding revoked'))
      setRevokingIdentity(null)
    },
  })
  const defaultMutation = useMutation({
    mutationFn: (input: { identityID: number; instanceID: number | null }) =>
      updateAssistantIdentityDefault(input.identityID, input.instanceID),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['assistant', 'identities'],
      })
      toast.success(t('Personal default instance updated'))
    },
  })

  return (
    <>
      <TitledCard
        title={t('User bindings')}
        description={t(
          'Review WeChat identities and the instance scope granted to each user.'
        )}
        icon={<UsersRound />}
        iconTone='primary'
      >
        {identitiesQuery.isLoading && (
          <div
            className='grid gap-3 lg:grid-cols-2'
            aria-label={t('Loading...')}
          >
            <Skeleton className='h-40' />
            <Skeleton className='h-40' />
          </div>
        )}
        {identitiesQuery.isError && (
          <ErrorState
            className='min-h-48'
            title={t('Could not load user bindings')}
            description={t('Check the server connection and try again.')}
            onRetry={() => identitiesQuery.refetch()}
          />
        )}
        {optionsQuery.isError && identitiesQuery.isSuccess && (
          <p className='text-destructive mb-3 text-sm' role='alert'>
            {t('Could not load default instance settings')}
          </p>
        )}
        {identitiesQuery.isSuccess && identitiesQuery.data.length === 0 && (
          <Empty className='min-h-48 border'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <UserRoundCheck />
              </EmptyMedia>
              <EmptyTitle>{t('No users are bound')}</EmptyTitle>
              <EmptyDescription>
                {t(
                  'A user appears here after sending a valid binding command in WeChat.'
                )}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {identitiesQuery.isSuccess && identitiesQuery.data.length > 0 && (
          <ul className='grid gap-3 lg:grid-cols-2'>
            {identitiesQuery.data.map((identity) => {
              const instanceOptions =
                identity.allowed_instance_scope === 'all'
                  ? (optionsQuery.data ?? [])
                  : (optionsQuery.data ?? []).filter((option) =>
                      identity.instance_ids.includes(option.id)
                    )
              return (
                <li
                  key={identity.id}
                  className='bg-muted/20 rounded-xl border p-4'
                >
                  <div className='flex items-start justify-between gap-3'>
                    <div className='flex min-w-0 items-center gap-3'>
                      <span className='bg-background flex size-10 shrink-0 items-center justify-center rounded-xl border'>
                        <UserRoundCheck className='size-5' aria-hidden='true' />
                      </span>
                      <div className='min-w-0'>
                        <p className='truncate font-medium'>
                          {identity.username || t('Unknown user')}
                        </p>
                        <p className='text-muted-foreground truncate text-xs'>
                          {t('WeChat {{external}} · channel #{{channel}}', {
                            external: identity.external_user,
                            channel: identity.channel_id,
                          })}
                        </p>
                      </div>
                    </div>
                    <Badge
                      variant={
                        identity.status === 'active' ? 'secondary' : 'outline'
                      }
                    >
                      {identityStatusLabel(identity.status, t)}
                    </Badge>
                  </div>

                  <dl className='mt-4 grid gap-3 text-sm sm:grid-cols-2'>
                    <div>
                      <dt className='text-muted-foreground text-xs'>
                        {t('Instance scope')}
                      </dt>
                      <dd className='mt-1 font-medium'>
                        {scopeLabel(
                          identity.allowed_instance_scope,
                          identity.instance_ids.length,
                          t
                        )}
                      </dd>
                    </div>
                    <div>
                      <dt className='text-muted-foreground text-xs'>
                        {t('Bound at')}
                      </dt>
                      <dd className='mt-1 text-xs'>
                        {identity.bound_at > 0
                          ? new Date(identity.bound_at * 1000).toLocaleString()
                          : t('Unknown')}
                      </dd>
                    </div>
                  </dl>

                  {identity.allowed_instance_scope === 'selected' &&
                    identity.instance_ids.length > 0 && (
                      <div
                        className='mt-3 flex flex-wrap gap-1.5'
                        aria-label={t('Selected instance IDs')}
                      >
                        {identity.instance_ids.map((instanceId) => (
                          <Badge key={instanceId} variant='outline'>
                            #{instanceId}
                          </Badge>
                        ))}
                      </div>
                    )}

                  {identity.status === 'active' && (
                    <div className='mt-4 space-y-3 border-t pt-3'>
                      <div className='space-y-1.5'>
                        <Label
                          htmlFor={`assistant-identity-default-${identity.id}`}
                        >
                          {t('Personal default')}
                        </Label>
                        <NativeSelect
                          id={`assistant-identity-default-${identity.id}`}
                          className='w-full'
                          value={
                            identity.default_instance_id == null
                              ? 'inherit'
                              : String(identity.default_instance_id)
                          }
                          disabled={
                            !optionsQuery.isSuccess ||
                            (defaultMutation.isPending &&
                              defaultMutation.variables?.identityID ===
                                identity.id)
                          }
                          onChange={(event) => {
                            const value = event.target.value
                            defaultMutation.mutate({
                              identityID: identity.id,
                              instanceID:
                                value === 'inherit' ? null : Number(value),
                            })
                          }}
                        >
                          <NativeSelectOption value='inherit'>
                            {t('Inherit global default')}
                          </NativeSelectOption>
                          {identity.default_instance_id != null &&
                            !instanceOptions.some(
                              (option) =>
                                option.id === identity.default_instance_id
                            ) && (
                              <NativeSelectOption
                                value={String(identity.default_instance_id)}
                                disabled
                              >
                                {t('Unavailable instance #{{id}}', {
                                  id: identity.default_instance_id,
                                })}
                              </NativeSelectOption>
                            )}
                          {instanceOptions.map((option) => (
                            <NativeSelectOption
                              key={option.id}
                              value={String(option.id)}
                            >
                              {option.name} · {option.kind} · {option.status} ·
                              #{option.id}
                            </NativeSelectOption>
                          ))}
                        </NativeSelect>
                      </div>
                      <EffectiveDefault identity={identity} />
                    </div>
                  )}

                  <div className='mt-4 border-t pt-3'>
                    <Button
                      variant='destructive'
                      className='min-h-11 w-full sm:w-auto'
                      disabled={identity.status === 'revoked'}
                      onClick={() => setRevokingIdentity(identity)}
                    >
                      <ShieldOff />
                      {identity.status === 'revoked'
                        ? t('Binding revoked')
                        : t('Revoke binding')}
                    </Button>
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </TitledCard>

      <ConfirmDialog
        open={revokingIdentity != null}
        onOpenChange={(open) => {
          if (!open) setRevokingIdentity(null)
        }}
        title={t('Revoke user binding')}
        desc={t(
          'This immediately removes the WeChat identity access and its selected instance scope. The user must bind again to continue.'
        )}
        confirmText={t('Revoke binding')}
        destructive
        isLoading={revokeMutation.isPending}
        handleConfirm={() => {
          if (revokingIdentity) revokeMutation.mutate(revokingIdentity.id)
        }}
      />
    </>
  )
}
