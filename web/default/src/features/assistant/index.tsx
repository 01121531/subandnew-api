/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Bot,
  CheckCircle2,
  KeyRound,
  Link2,
  MessageCircle,
  Pencil,
  Plus,
  RefreshCw,
  ShieldCheck,
  Trash2,
  Unplug,
  Wifi,
  X,
} from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import {
  cancelAssistantChannelLogin,
  checkAssistantChannelLogin,
  createAssistantBindingCode,
  createAssistantModelProfile,
  deleteAssistantModelProfile,
  listAssistantChannels,
  listAssistantModelProfiles,
  removeAssistantChannelCredential,
  startAssistantChannelLogin,
  testAssistantModelProfile,
  updateAssistantModelProfile,
} from './api'
import { AssistantAuditSection } from './audit-section'
import {
  GlobalDefaultInstanceSection,
  MyDefaultInstanceSection,
} from './default-instance-section'
import { AssistantIdentitySection } from './identity-section'
import { ModelProfileDialog } from './model-profile-dialog'
import type {
  AssistantChannel,
  AssistantChannelStatus,
  AssistantLoginState,
  AssistantLoginView,
  AssistantModelProfile,
  AssistantModelProfileInput,
} from './types'

const assistantQueryKeys = {
  profiles: ['assistant', 'model-profiles'] as const,
  channels: ['assistant', 'channels'] as const,
  login: (channelId: number) => ['assistant', 'login', channelId] as const,
}

function ModelProfilesSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingProfile, setEditingProfile] =
    useState<AssistantModelProfile | null>(null)
  const [deletingProfile, setDeletingProfile] =
    useState<AssistantModelProfile | null>(null)
  const [testLatencies, setTestLatencies] = useState<Record<number, number>>({})
  const profilesQuery = useQuery({
    queryKey: assistantQueryKeys.profiles,
    queryFn: listAssistantModelProfiles,
  })
  const saveMutation = useMutation({
    mutationFn: (variables: {
      profile: AssistantModelProfile | null
      input: AssistantModelProfileInput
    }) =>
      variables.profile
        ? updateAssistantModelProfile(variables.profile.id, variables.input)
        : createAssistantModelProfile(variables.input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: assistantQueryKeys.profiles,
      })
      toast.success(
        editingProfile ? t('Model profile updated') : t('Model profile created')
      )
      setDialogOpen(false)
      setEditingProfile(null)
    },
  })
  const deleteMutation = useMutation({
    mutationFn: deleteAssistantModelProfile,
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: assistantQueryKeys.profiles,
      })
      toast.success(t('Model profile deleted'))
      setDeletingProfile(null)
    },
  })
  const testMutation = useMutation({
    mutationFn: testAssistantModelProfile,
    onSuccess: (data, profileId) => {
      setTestLatencies((current) => ({
        ...current,
        [profileId]: data.latency_ms,
      }))
      toast.success(
        t('Connection succeeded in {{latency}} ms', {
          latency: data.latency_ms,
        })
      )
    },
    onError: () => {
      toast.error(t('Model connection test failed'))
    },
  })

  function openCreateDialog() {
    setEditingProfile(null)
    setDialogOpen(true)
  }

  function openEditDialog(profile: AssistantModelProfile) {
    setEditingProfile(profile)
    setDialogOpen(true)
  }

  return (
    <>
      <TitledCard
        title={t('Model configuration')}
        description={t(
          'Manage the OpenAI-compatible models used by the assistant.'
        )}
        icon={<Bot />}
        iconTone='info'
        action={
          <Button
            className='min-h-11 w-full sm:w-auto'
            onClick={openCreateDialog}
          >
            <Plus />
            {t('Add model')}
          </Button>
        }
      >
        {profilesQuery.isLoading && (
          <div
            className='grid gap-3 lg:grid-cols-2'
            aria-label={t('Loading...')}
          >
            <Skeleton className='h-36' />
            <Skeleton className='h-36' />
          </div>
        )}
        {profilesQuery.isError && (
          <ErrorState
            className='min-h-52'
            title={t('Could not load model profiles')}
            description={t('Check the server connection and try again.')}
            onRetry={() => profilesQuery.refetch()}
          />
        )}
        {profilesQuery.isSuccess && profilesQuery.data.length === 0 && (
          <Empty className='min-h-52 border'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Bot />
              </EmptyMedia>
              <EmptyTitle>{t('No model profiles yet')}</EmptyTitle>
              <EmptyDescription>
                {t('Add a model endpoint before connecting the assistant.')}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button className='min-h-11' onClick={openCreateDialog}>
                <Plus />
                {t('Add model')}
              </Button>
            </EmptyContent>
          </Empty>
        )}
        {profilesQuery.isSuccess && profilesQuery.data.length > 0 && (
          <ul className='grid gap-3 lg:grid-cols-2'>
            {profilesQuery.data.map((profile) => (
              <li
                key={profile.id}
                className='bg-muted/25 rounded-xl border p-4'
              >
                <div className='flex items-start justify-between gap-3'>
                  <div className='min-w-0'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <h3 className='truncate font-medium'>{profile.name}</h3>
                      {profile.is_primary && <Badge>{t('Primary')}</Badge>}
                      <Badge
                        variant={profile.enabled ? 'secondary' : 'outline'}
                      >
                        {profile.enabled ? t('Enabled') : t('Disabled')}
                      </Badge>
                    </div>
                    <p className='text-muted-foreground mt-1 truncate text-sm'>
                      {profile.model}
                    </p>
                  </div>
                  <div className='flex shrink-0 gap-1'>
                    <Button
                      variant='ghost'
                      size='icon'
                      className='size-11'
                      aria-label={t('Edit {{name}}', { name: profile.name })}
                      onClick={() => openEditDialog(profile)}
                    >
                      <Pencil />
                    </Button>
                    <Button
                      variant='ghost'
                      size='icon'
                      className='text-destructive size-11'
                      aria-label={t('Delete {{name}}', { name: profile.name })}
                      onClick={() => setDeletingProfile(profile)}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </div>
                <dl className='mt-4 grid gap-2 text-sm sm:grid-cols-2'>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('API base URL')}
                    </dt>
                    <dd className='truncate font-mono text-xs'>
                      {profile.base_url}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>{t('API key')}</dt>
                    <dd className='truncate font-mono text-xs'>
                      {profile.api_key_fingerprint
                        ? t('Encrypted · fingerprint {{value}}', {
                            value: profile.api_key_fingerprint.slice(0, 10),
                          })
                        : t('Not configured')}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>{t('Timeout')}</dt>
                    <dd>
                      {t('{{count}} seconds', {
                        count: profile.timeout_seconds,
                      })}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Max output tokens')}
                    </dt>
                    <dd>{profile.max_output_tokens.toLocaleString()}</dd>
                  </div>
                </dl>
                <div className='mt-4 flex flex-col gap-2 border-t pt-3 sm:flex-row sm:items-center sm:justify-between'>
                  <Button
                    variant='outline'
                    className='min-h-11 w-full sm:w-auto'
                    disabled={testMutation.isPending}
                    onClick={() => testMutation.mutate(profile.id)}
                  >
                    <Wifi />
                    {testMutation.isPending &&
                    testMutation.variables === profile.id
                      ? t('Testing...')
                      : t('Test connection')}
                  </Button>
                  {testLatencies[profile.id] != null && (
                    <p className='text-success text-sm' role='status'>
                      {t('Reachable · {{latency}} ms', {
                        latency: testLatencies[profile.id],
                      })}
                    </p>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </TitledCard>

      <ModelProfileDialog
        open={dialogOpen}
        profile={editingProfile}
        pending={saveMutation.isPending}
        onOpenChange={(open) => {
          setDialogOpen(open)
          if (!open) setEditingProfile(null)
        }}
        onSubmit={(input) =>
          saveMutation.mutate({ profile: editingProfile, input })
        }
      />
      <ConfirmDialog
        open={deletingProfile != null}
        onOpenChange={(open) => {
          if (!open) setDeletingProfile(null)
        }}
        title={t('Delete model profile')}
        desc={t('This removes {{name}} and its encrypted API key.', {
          name: deletingProfile?.name ?? '',
        })}
        confirmText={t('Delete')}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() => {
          if (deletingProfile) deleteMutation.mutate(deletingProfile.id)
        }}
      />
    </>
  )
}

function loginStateLabel(
  state: AssistantLoginState,
  t: (key: string) => string
) {
  const labels: Record<AssistantLoginState, string> = {
    pending: t('Waiting for scan'),
    scanned: t('Scanned, confirm on WeChat'),
    verify_required: t('Verification code required'),
    connected: t('Connected'),
    expired: t('QR code expired'),
  }
  return labels[state]
}

function channelStatusLabel(
  status: AssistantChannelStatus,
  t: (key: string) => string
) {
  const labels: Record<AssistantChannelStatus, string> = {
    unbound: t('Unbound'),
    qr_issued: t('Waiting for scan'),
    scanned: t('Scanned'),
    verify_required: t('Verification required'),
    connected: t('Connected'),
    degraded: t('Degraded'),
    reauth_required: t('Sign-in required'),
  }
  return labels[status]
}

function maskedBotId(value: string) {
  const normalized = value.trim()
  if (normalized.length <= 16) return normalized
  return `${normalized.slice(0, 7)}...${normalized.slice(-7)}`
}

function WeChatChannelSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [login, setLogin] = useState<AssistantLoginView | null>(null)
  const [verifyCode, setVerifyCode] = useState('')
  const [disconnectingChannel, setDisconnectingChannel] =
    useState<AssistantChannel | null>(null)
  const channelsQuery = useQuery({
    queryKey: assistantQueryKeys.channels,
    queryFn: listAssistantChannels,
    retry: false,
  })
  const startLoginMutation = useMutation({
    mutationFn: startAssistantChannelLogin,
    onSuccess: async (data) => {
      setLogin(data)
      setVerifyCode('')
      await queryClient.invalidateQueries({
        queryKey: assistantQueryKeys.channels,
      })
    },
  })
  const statusQuery = useQuery({
    queryKey: assistantQueryKeys.login(login?.channel_id ?? 0),
    queryFn: ({ signal }) =>
      checkAssistantChannelLogin(login?.channel_id ?? 0, '', signal),
    enabled:
      login != null && (login.state === 'pending' || login.state === 'scanned'),
    retry: 2,
    retryDelay: (attempt) => Math.min(2000, 500 * 2 ** attempt),
    refetchInterval: (query) => {
      const state = query.state.data?.state
      return state == null || state === 'pending' || state === 'scanned'
        ? 2000
        : false
    },
  })
  const verifyMutation = useMutation({
    mutationFn: () =>
      checkAssistantChannelLogin(login?.channel_id ?? 0, verifyCode),
    onSuccess: (data) => {
      setLogin((current) => (current ? { ...current, ...data } : data))
      setVerifyCode('')
    },
  })
  const disconnectMutation = useMutation({
    mutationFn: removeAssistantChannelCredential,
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: assistantQueryKeys.channels,
      })
      if (login?.channel_id === disconnectingChannel?.id) setLogin(null)
      toast.success(t('WeChat channel disconnected'))
      setDisconnectingChannel(null)
    },
  })
  const cancelLoginMutation = useMutation({
    mutationFn: cancelAssistantChannelLogin,
    onMutate: async (channelId) => {
      await queryClient.cancelQueries({
        queryKey: assistantQueryKeys.login(channelId),
      })
      queryClient.removeQueries({
        queryKey: assistantQueryKeys.login(channelId),
      })
    },
    onSuccess: async () => {
      setLogin(null)
      setVerifyCode('')
      await queryClient.invalidateQueries({
        queryKey: assistantQueryKeys.channels,
      })
      toast.success(t('WeChat channel disconnected'))
    },
  })
  const currentLogin = useMemo(
    () => (login ? { ...login, ...statusQuery.data } : null),
    [login, statusQuery.data]
  )

  useEffect(() => {
    if (!currentLogin || currentLogin.state !== 'connected') return
    void queryClient.invalidateQueries({
      queryKey: assistantQueryKeys.channels,
    })
  }, [currentLogin, queryClient])

  return (
    <>
      <TitledCard
        title={t('WeChat connection')}
        description={t(
          'Scan with WeChat to connect an OpenBot assistant account.'
        )}
        icon={<MessageCircle />}
        iconTone='success'
        action={
          <Button
            className='min-h-11 w-full sm:w-auto'
            disabled={startLoginMutation.isPending}
            onClick={() => startLoginMutation.mutate()}
          >
            <Link2 />
            {startLoginMutation.isPending
              ? t('Creating QR code...')
              : t('Connect WeChat')}
          </Button>
        }
      >
        {currentLogin && (
          <div className='mb-4 rounded-xl border p-4' aria-live='polite'>
            <div className='flex flex-col gap-5 sm:flex-row sm:items-center'>
              {currentLogin.qr_image && currentLogin.state !== 'connected' && (
                <div className='mx-auto flex size-52 shrink-0 items-center justify-center rounded-xl bg-white p-3 ring-1 ring-black/10 sm:mx-0'>
                  <QRCodeSVG
                    value={currentLogin.qr_image}
                    size={184}
                    level='M'
                    title={t('WeChat login QR code')}
                  />
                </div>
              )}
              <div className='min-w-0 flex-1 space-y-3'>
                <div>
                  <Badge
                    variant={
                      currentLogin.state === 'expired'
                        ? 'destructive'
                        : 'secondary'
                    }
                  >
                    {loginStateLabel(currentLogin.state, t)}
                  </Badge>
                  <p className='text-muted-foreground mt-2 text-sm'>
                    {currentLogin.state === 'connected'
                      ? t('WeChat is connected and ready to receive messages.')
                      : t(
                          'Open WeChat, scan the code, then confirm the sign-in on your phone.'
                        )}
                  </p>
                </div>
                {(currentLogin.state === 'pending' ||
                  currentLogin.state === 'scanned') && (
                  <p className='text-muted-foreground flex items-center gap-2 text-sm'>
                    <RefreshCw
                      className='size-4 animate-spin'
                      aria-hidden='true'
                    />
                    {t('Checking connection status automatically...')}
                  </p>
                )}
                {currentLogin.state === 'verify_required' && (
                  <form
                    className='flex flex-col gap-2 sm:flex-row sm:items-end'
                    onSubmit={(event) => {
                      event.preventDefault()
                      if (verifyCode.trim()) verifyMutation.mutate()
                    }}
                  >
                    <div className='min-w-0 flex-1 space-y-1.5'>
                      <Label htmlFor='assistant-wechat-verify-code'>
                        {t('WeChat verification code')}
                      </Label>
                      <Input
                        id='assistant-wechat-verify-code'
                        className='min-h-11 text-base'
                        inputMode='numeric'
                        autoComplete='one-time-code'
                        value={verifyCode}
                        onChange={(event) => setVerifyCode(event.target.value)}
                      />
                    </div>
                    <Button
                      type='submit'
                      className='min-h-11'
                      disabled={!verifyCode.trim() || verifyMutation.isPending}
                    >
                      {t('Verify')}
                    </Button>
                  </form>
                )}
                {currentLogin.state === 'expired' && (
                  <Button
                    variant='outline'
                    className='min-h-11'
                    onClick={() => startLoginMutation.mutate()}
                  >
                    {t('Generate a new QR code')}
                  </Button>
                )}
                {currentLogin.state !== 'connected' &&
                  currentLogin.state !== 'expired' && (
                    <Button
                      variant='outline'
                      className='min-h-11'
                      disabled={cancelLoginMutation.isPending}
                      onClick={() =>
                        cancelLoginMutation.mutate(currentLogin.channel_id)
                      }
                    >
                      <X />
                      {t('Cancel')}
                    </Button>
                  )}
                {statusQuery.isError && (
                  <Alert variant='destructive'>
                    <AlertTitle>{t('Status check failed')}</AlertTitle>
                    <AlertDescription className='flex flex-col items-start gap-2'>
                      {t(
                        'The QR code may have expired. Retry the status check or create a new code.'
                      )}
                      <Button
                        variant='outline'
                        className='min-h-11'
                        onClick={() => statusQuery.refetch()}
                      >
                        {t('Retry')}
                      </Button>
                    </AlertDescription>
                  </Alert>
                )}
              </div>
            </div>
          </div>
        )}

        {channelsQuery.isLoading && (
          <Skeleton className='h-24' aria-label={t('Loading...')} />
        )}
        {channelsQuery.isError && (
          <ErrorState
            className='min-h-44'
            title={t('Could not load WeChat channels')}
            description={t('Check the server connection and try again.')}
            onRetry={() => channelsQuery.refetch()}
          />
        )}
        {channelsQuery.isSuccess &&
          channelsQuery.data.length === 0 &&
          !currentLogin && (
            <Empty className='min-h-44 border'>
              <EmptyHeader>
                <EmptyMedia variant='icon'>
                  <MessageCircle />
                </EmptyMedia>
                <EmptyTitle>{t('No WeChat account connected')}</EmptyTitle>
                <EmptyDescription>
                  {t(
                    'Create a QR code and scan it with the WeChat account that will host the assistant.'
                  )}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        {channelsQuery.isSuccess && channelsQuery.data.length > 0 && (
          <ul className='grid gap-3 sm:grid-cols-2'>
            {channelsQuery.data.map((channel) => {
              let badgeVariant: 'outline' | 'secondary' | 'destructive' =
                'outline'
              if (channel.status === 'connected') badgeVariant = 'secondary'
              if (
                channel.status === 'degraded' ||
                channel.status === 'reauth_required'
              ) {
                badgeVariant = 'destructive'
              }

              return (
                <li
                  key={channel.id}
                  className='bg-muted/25 rounded-xl border p-4'
                >
                  <div className='flex items-center justify-between gap-3'>
                    <div className='min-w-0'>
                      <p className='truncate font-medium'>
                        {t('WeChat ClawBot')}
                      </p>
                      <div className='text-muted-foreground mt-1 flex min-w-0 items-center gap-1 text-xs'>
                        <span className='truncate font-mono'>
                          {maskedBotId(channel.account_id)}
                        </span>
                        <CopyButton
                          value={channel.account_id}
                          className='size-7'
                          iconClassName='size-3.5'
                          tooltip={t('Account ID')}
                          aria-label={t('Account ID')}
                        />
                        <span className='shrink-0'>
                          {t('Channel #{{id}}', { id: channel.id })}
                        </span>
                      </div>
                    </div>
                    <Badge variant={badgeVariant}>
                      {channelStatusLabel(channel.status, t)}
                    </Badge>
                  </div>
                  {channel.status !== 'unbound' && (
                    <div className='mt-4 border-t pt-3'>
                      <Button
                        variant='destructive'
                        className='min-h-11 w-full sm:w-auto'
                        onClick={() => setDisconnectingChannel(channel)}
                      >
                        <Unplug />
                        {t('Disconnect channel')}
                      </Button>
                    </div>
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </TitledCard>
      <ConfirmDialog
        open={disconnectingChannel != null}
        onOpenChange={(open) => {
          if (!open) setDisconnectingChannel(null)
        }}
        title={t('Disconnect WeChat channel')}
        desc={t(
          'This deletes the encrypted login credential and stops message polling. The channel audit record is retained.'
        )}
        confirmText={t('Disconnect')}
        destructive
        isLoading={disconnectMutation.isPending}
        handleConfirm={() => {
          if (disconnectingChannel) {
            disconnectMutation.mutate(disconnectingChannel.id)
          }
        }}
      />
    </>
  )
}

function BindingCodeSection() {
  const { t } = useTranslation()
  const [code, setCode] = useState<Awaited<
    ReturnType<typeof createAssistantBindingCode>
  > | null>(null)
  const mutation = useMutation({
    mutationFn: createAssistantBindingCode,
    onSuccess: (data) => {
      setCode(data)
      toast.success(t('Binding code created'))
    },
  })

  return (
    <TitledCard
      title={t('Account binding')}
      description={t(
        'Generate a short-lived code to link your web account in WeChat.'
      )}
      icon={<KeyRound />}
      iconTone='warning'
      action={
        <Button
          className='min-h-11 w-full sm:w-auto'
          disabled={mutation.isPending}
          onClick={() => mutation.mutate()}
        >
          <KeyRound />
          {mutation.isPending ? t('Generating...') : t('Generate binding code')}
        </Button>
      }
    >
      <Alert>
        <ShieldCheck />
        <AlertTitle>{t('Scoped access')}</AlertTitle>
        <AlertDescription>
          {t(
            'This code grants your WeChat identity access to all instances you are allowed to view. It expires in five minutes and can be used once.'
          )}
        </AlertDescription>
      </Alert>

      {code && (
        <div
          className='bg-muted/35 mt-4 rounded-xl border p-4 sm:p-5'
          aria-live='polite'
        >
          <div className='flex items-center gap-2 text-sm font-medium'>
            <CheckCircle2 className='text-success size-5' aria-hidden='true' />
            {t('Binding code ready')}
          </div>
          <div className='mt-4 flex items-center gap-2'>
            <code className='bg-background min-w-0 flex-1 rounded-lg border px-3 py-3 text-center text-xl font-bold tracking-[0.18em] break-all sm:text-2xl'>
              {code.code}
            </code>
            <CopyButton
              value={code.code}
              className='size-11'
              tooltip={t('Copy binding code')}
              aria-label={t('Copy binding code')}
            />
          </div>
          <div className='mt-3 flex items-center gap-2'>
            <code className='bg-background min-w-0 flex-1 rounded-lg border px-3 py-2 text-sm break-all'>
              {code.command}
            </code>
            <CopyButton
              value={code.command}
              className='size-11'
              tooltip={t('Copy WeChat command')}
              aria-label={t('Copy WeChat command')}
            />
          </div>
          <p className='text-muted-foreground mt-3 text-xs'>
            {t('Expires at {{time}}', {
              time: new Date(code.expires_at * 1000).toLocaleTimeString(),
            })}
          </p>
        </div>
      )}
    </TitledCard>
  )
}

export function AssistantManagement() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canAccess = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.ASSISTANT,
    ADMIN_PERMISSION_ACTIONS.ACCESS
  )
  const canManage = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.ASSISTANT,
    ADMIN_PERMISSION_ACTIONS.MANAGE
  )
  const canAudit = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.ASSISTANT,
    ADMIN_PERMISSION_ACTIONS.AUDIT
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('AI Assistant')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto max-w-6xl space-y-4 pb-4'>
          {canManage && <GlobalDefaultInstanceSection />}
          {canAccess && <MyDefaultInstanceSection />}
          {canManage && <ModelProfilesSection />}
          {canManage && <WeChatChannelSection />}
          {canAccess && <BindingCodeSection />}
          {canManage && <AssistantIdentitySection />}
          {canAudit && <AssistantAuditSection />}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
