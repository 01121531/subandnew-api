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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ArrowLeft, KeyRound, Pencil, RefreshCw } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  checkManagedInstance,
  getManagedInstance,
  getManagedInstanceAudits,
  getManagedInstanceTask,
} from './api'
import { ConfigurationGovernancePanel } from './components/configuration-governance-panel'
import { ControlledOperationsPanel } from './components/controlled-operations-panel'
import { CredentialSheet } from './components/credential-sheet'
import { InstanceFormSheet } from './components/instance-form-sheet'
import { ObservabilityPanel } from './components/observability-panel'
import { StatusBadge } from './components/status-badge'
import { formatTimestamp, MANAGED_INSTANCE_KINDS } from './lib'
import type {
  ApiResponse,
  ManagedInstance,
  ManagedInstanceAudit,
  ManagedInstanceTask,
  ManagedInstanceTaskStatus,
} from './types'

type ManagedInstanceDetailProps = {
  instanceId: number
}

const activeTaskStatuses = new Set<ManagedInstanceTaskStatus>([
  'pending',
  'running',
])

export function ManagedInstanceDetail({
  instanceId,
}: ManagedInstanceDetailProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const [editing, setEditing] = useState(false)
  const [rotating, setRotating] = useState(false)
  const [queueing, setQueueing] = useState(false)
  const [taskId, setTaskId] = useState(() => readRecentTaskId(instanceId))
  const validInstanceId = Number.isInteger(instanceId) && instanceId > 0
  const isRoot = user?.role === ROLE.SUPER_ADMIN
  const canUpdate =
    isRoot &&
    hasPermission(
      user,
      ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
      ADMIN_PERMISSION_ACTIONS.UPDATE
    )
  const canCheck = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
    ADMIN_PERMISSION_ACTIONS.OPERATE
  )
  const canRotate =
    isRoot &&
    hasPermission(
      user,
      ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
      ADMIN_PERMISSION_ACTIONS.SECRET_ROTATE
    )
  const canAudit = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
    ADMIN_PERMISSION_ACTIONS.AUDIT
  )
  const canViewConfig = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_TEMPLATE,
    ADMIN_PERMISSION_ACTIONS.VIEW
  )

  const instanceQuery = useQuery({
    queryKey: ['managed-instance', instanceId],
    queryFn: () => getManagedInstance(instanceId),
    enabled: validInstanceId,
    refetchInterval: 30_000,
  })
  const auditsQuery = useQuery({
    queryKey: ['managed-instance-audits', instanceId],
    queryFn: () => getManagedInstanceAudits(instanceId),
    enabled: validInstanceId && canAudit,
  })
  const taskQuery = useQuery({
    queryKey: ['managed-instance-task', instanceId, taskId],
    queryFn: () => getManagedInstanceTask(instanceId, taskId ?? ''),
    enabled: validInstanceId && Boolean(taskId),
    refetchInterval: (query) => {
      const response = query.state.data as
        | ApiResponse<ManagedInstanceTask>
        | undefined
      return !response || activeTaskStatuses.has(response.data.status)
        ? 1_500
        : false
    },
  })

  const instance = instanceQuery.data?.data
  const audits = useMemo(
    () => auditsQuery.data?.data.items ?? [],
    [auditsQuery.data]
  )
  const task = taskQuery.data?.data
  const latestCheckAudit = useMemo(
    () => audits.find((audit) => audit.action === 'check'),
    [audits]
  )

  const taskActive = task ? activeTaskStatuses.has(task.status) : false

  useEffect(() => {
    if (!task || activeTaskStatuses.has(task.status)) return
    void queryClient.invalidateQueries({
      queryKey: ['managed-instance', instanceId],
    })
    void queryClient.invalidateQueries({
      queryKey: ['managed-instance-audits', instanceId],
    })
    void queryClient.invalidateQueries({ queryKey: ['managed-instances'] })
  }, [instanceId, queryClient, task])

  const refresh = () => {
    void instanceQuery.refetch()
    if (canAudit) void auditsQuery.refetch()
    if (taskId) void taskQuery.refetch()
  }

  const checkNow = async () => {
    setQueueing(true)
    try {
      const response = await checkManagedInstance(instanceId)
      if (!response.success) return
      setTaskId(response.data.task_id)
      writeRecentTaskId(instanceId, response.data.task_id)
      toast.success(t('Probe task queued'))
      void queryClient.invalidateQueries({
        queryKey: ['managed-instance-task', instanceId],
      })
    } finally {
      setQueueing(false)
    }
  }

  const saved = () => {
    void instanceQuery.refetch()
    if (canAudit) void auditsQuery.refetch()
    void queryClient.invalidateQueries({ queryKey: ['managed-instances'] })
  }

  if (validInstanceId && instanceQuery.isPending) {
    return <DetailLoading onBack={() => void navigate({ to: '/instances' })} />
  }

  if (!validInstanceId || !instance || instanceQuery.isError) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('Instance details')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='grid min-h-64 place-items-center gap-3 rounded-lg border p-6 text-center'>
            <div>
              <div className='font-medium'>{t('Instance unavailable')}</div>
              <div className='text-muted-foreground mt-1 text-sm'>
                {t(
                  'The instance may have been removed or you may not have access.'
                )}
              </div>
            </div>
            <Button
              variant='outline'
              onClick={() => void navigate({ to: '/instances' })}
            >
              <ArrowLeft />
              {t('Back to instances')}
            </Button>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          <div className='flex min-w-0 items-center gap-2'>
            <Button
              variant='ghost'
              size='icon-sm'
              aria-label={t('Back to instances')}
              onClick={() => void navigate({ to: '/instances' })}
            >
              <ArrowLeft />
            </Button>
            <span className='truncate'>{instance.name}</span>
            <StatusBadge status={instance.status} />
          </div>
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            size='icon-sm'
            aria-label={t('Refresh')}
            onClick={refresh}
          >
            <RefreshCw
              className={cn(
                (instanceQuery.isFetching || auditsQuery.isFetching) &&
                  'animate-spin'
              )}
            />
          </Button>
          {canCheck && (
            <Button
              size='sm'
              disabled={queueing || taskActive}
              onClick={() => void checkNow()}
            >
              <RefreshCw
                className={cn((queueing || taskActive) && 'animate-spin')}
              />
              {t('Check now')}
            </Button>
          )}
          {canUpdate && (
            <Button
              variant='outline'
              size='sm'
              onClick={() => setEditing(true)}
            >
              <Pencil />
              {t('Edit')}
            </Button>
          )}
          {canRotate && (
            <Button
              variant='outline'
              size='sm'
              onClick={() => setRotating(true)}
            >
              <KeyRound />
              {t('Rotate credential')}
            </Button>
          )}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(320px,0.42fr)]'>
            <div className='grid min-w-0 content-start gap-4'>
              <OverviewSection instance={instance} />
              {isRoot && <ConnectionSection instance={instance} />}
              <CapabilitiesSection instance={instance} />
            </div>
            <div className='grid min-w-0 content-start gap-4'>
              <RecentCheckSection
                instance={instance}
                task={task}
                audit={latestCheckAudit}
                taskLoading={Boolean(taskId) && taskQuery.isPending}
              />
              {isRoot && <CredentialSection instance={instance} />}
            </div>
            <div className='min-w-0 xl:col-span-2'>
              <ObservabilityPanel instance={instance} />
            </div>
            {canViewConfig && instance.capabilities.includes('config.read') && (
              <div className='min-w-0 xl:col-span-2'>
                <ConfigurationGovernancePanel instance={instance} />
              </div>
            )}
            {canCheck && (
              <div className='min-w-0 xl:col-span-2'>
                <ControlledOperationsPanel instance={instance} />
              </div>
            )}
            {canAudit && (
              <div className='min-w-0 xl:col-span-2'>
                <AuditSection audits={audits} loading={auditsQuery.isPending} />
              </div>
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <InstanceFormSheet
        open={editing}
        instance={instance}
        onOpenChange={setEditing}
        onSaved={saved}
      />
      <CredentialSheet
        instance={rotating ? instance : null}
        onOpenChange={setRotating}
        onSaved={saved}
      />
    </>
  )
}

function DetailLoading({ onBack }: { onBack: () => void }) {
  const { t } = useTranslation()
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <div className='flex items-center gap-2'>
          <Button
            variant='ghost'
            size='icon-sm'
            aria-label={t('Back to instances')}
            onClick={onBack}
          >
            <ArrowLeft />
          </Button>
          <Skeleton className='h-7 w-48' />
        </div>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='grid gap-4 lg:grid-cols-2'>
          <Skeleton className='h-64 w-full' />
          <Skeleton className='h-64 w-full' />
          <Skeleton className='h-72 w-full lg:col-span-2' />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function DetailSection(props: {
  title: string
  description?: string
  children: React.ReactNode
}) {
  return (
    <section className='min-w-0 rounded-lg border'>
      <header className='border-b px-4 py-3'>
        <h2 className='text-sm font-semibold'>{props.title}</h2>
        {props.description && (
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {props.description}
          </p>
        )}
      </header>
      <div className='min-w-0 p-4'>{props.children}</div>
    </section>
  )
}

function OverviewSection({ instance }: { instance: ManagedInstance }) {
  const { t } = useTranslation()
  return (
    <DetailSection title={t('Overview')}>
      <dl className='grid min-w-0 gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3'>
        <Info label={t('Status')}>
          <StatusBadge status={instance.status} />
        </Info>
        <Info label={t('Version')} value={instance.version || '-'} />
        <Info
          label={t('Management mode')}
          value={t(titleCase(instance.management_mode))}
        />
        <Info
          label={t('Last seen')}
          value={formatTimestamp(instance.last_seen_at)}
        />
        <Info
          label={t('Last checked')}
          value={formatTimestamp(instance.last_checked_at)}
        />
        <Info
          label={t('Consecutive failures')}
          value={String(instance.consecutive_failures)}
        />
      </dl>
    </DetailSection>
  )
}

function ConnectionSection({ instance }: { instance: ManagedInstance }) {
  const { t } = useTranslation()
  const product =
    MANAGED_INSTANCE_KINDS.find((kind) => kind.value === instance.kind)
      ?.label || instance.kind
  return (
    <DetailSection title={t('Connection information')}>
      <dl className='grid min-w-0 gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3'>
        <Info label={t('Base URL')}>
          <div className='flex max-w-full items-start gap-1'>
            <span className='text-primary min-w-0 break-all'>
              {instance.base_url}
            </span>
            <CopyButton
              value={instance.base_url}
              className='-my-1 size-7'
              iconClassName='size-3.5'
              tooltip={t('Copy to clipboard')}
              successTooltip={t('Copied!')}
            />
          </div>
        </Info>
        <Info label={t('Product')} value={product} />
        <Info
          label={t('Environment')}
          value={t(titleCase(instance.environment))}
        />
        <Info
          label={t('TLS verification')}
          value={instance.tls_verify ? t('Enabled') : t('Disabled')}
        />
        <Info
          label={t('Request timeout')}
          value={t('{{count}} seconds', {
            count: instance.request_timeout_seconds,
          })}
        />
        <Info
          label={t('Check interval')}
          value={t('{{count}} seconds', {
            count: instance.check_interval_seconds,
          })}
        />
      </dl>
      {Object.keys(instance.labels).length > 0 && (
        <div className='mt-4 border-t pt-4'>
          <div className='text-muted-foreground mb-2 text-xs'>
            {t('Labels')}
          </div>
          <div className='flex min-w-0 flex-wrap gap-1.5'>
            {Object.entries(instance.labels).map(([key, value]) => (
              <Badge
                key={key}
                variant='outline'
                className='h-auto max-w-full py-1 break-all whitespace-normal'
              >
                {key}={value}
              </Badge>
            ))}
          </div>
        </div>
      )}
    </DetailSection>
  )
}

function CapabilitiesSection({ instance }: { instance: ManagedInstance }) {
  const { t } = useTranslation()
  return (
    <DetailSection
      title={t('Capabilities')}
      description={t(
        'Capabilities discovered during the latest successful check.'
      )}
    >
      {instance.capabilities.length > 0 ? (
        <div className='flex min-w-0 flex-wrap gap-2'>
          {instance.capabilities.map((capability) => (
            <Badge
              key={capability}
              variant='secondary'
              className='max-w-full font-mono text-xs break-all'
            >
              {capability}
            </Badge>
          ))}
        </div>
      ) : (
        <p className='text-muted-foreground text-sm'>
          {t('No capabilities discovered yet.')}
        </p>
      )}
    </DetailSection>
  )
}

function RecentCheckSection(props: {
  instance: ManagedInstance
  task?: ManagedInstanceTask
  audit?: ManagedInstanceAudit
  taskLoading: boolean
}) {
  const { t } = useTranslation()
  const auditDetails = parseDetails(props.audit?.details)
  const taskResult = asRecord(props.task?.result)
  let content: React.ReactNode
  if (props.taskLoading) {
    content = (
      <div className='grid gap-2'>
        <Skeleton className='h-5 w-24' />
        <Skeleton className='h-4 w-full' />
      </div>
    )
  } else if (props.task) {
    content = (
      <dl className='grid min-w-0 gap-y-4'>
        <Info label={t('Task status')}>
          <TaskStatusBadge status={props.task.status} />
        </Info>
        <Info label={t('Task ID')} value={props.task.task_id} mono />
        <Info
          label={t('Updated')}
          value={formatTimestamp(props.task.updated_at)}
        />
        {props.task.error && (
          <Info
            label={t('Error code')}
            value={props.task.error}
            mono
            tone='danger'
          />
        )}
        {taskResult.latency_ms != null && (
          <Info
            label={t('Latency')}
            value={`${String(taskResult.latency_ms)} ms`}
          />
        )}
      </dl>
    )
  } else if (props.audit) {
    content = (
      <dl className='grid min-w-0 gap-y-4'>
        <Info label={t('Check outcome')}>
          <OutcomeBadge outcome={props.audit.outcome} />
        </Info>
        <Info
          label={t('Checked at')}
          value={formatTimestamp(props.audit.created_at)}
        />
        {auditDetails.latency_ms != null && (
          <Info
            label={t('Latency')}
            value={`${String(auditDetails.latency_ms)} ms`}
          />
        )}
        {auditDetails.error_code != null && (
          <Info
            label={t('Error code')}
            value={String(auditDetails.error_code)}
            mono
            tone='danger'
          />
        )}
      </dl>
    )
  } else {
    content = (
      <div className='grid gap-2 text-sm'>
        <StatusBadge status={props.instance.status} />
        <span className='text-muted-foreground'>
          {t('No check record yet.')}
        </span>
      </div>
    )
  }
  return (
    <DetailSection title={t('Latest check and task')}>{content}</DetailSection>
  )
}

function CredentialSection({ instance }: { instance: ManagedInstance }) {
  const { t } = useTranslation()
  const credential = instance.credential
  return (
    <DetailSection title={t('Management credential')}>
      {credential ? (
        <dl className='grid min-w-0 gap-y-4'>
          <Info label={t('Authentication')} value={credential.auth_type} mono />
          <Info
            label={t('Fingerprint')}
            value={`••••${credential.fingerprint}`}
            mono
          />
          <Info
            label={t('Last verified')}
            value={formatTimestamp(credential.last_verified_at)}
          />
          <Info
            label={t('Rotated at')}
            value={formatTimestamp(credential.rotated_at)}
          />
          <Info
            label={t('Expires at')}
            value={formatTimestamp(credential.expires_at)}
          />
        </dl>
      ) : (
        <p className='text-muted-foreground text-sm'>
          {t('No management credential configured.')}
        </p>
      )}
    </DetailSection>
  )
}

function AuditSection({
  audits,
  loading,
}: {
  audits: ManagedInstanceAudit[]
  loading: boolean
}) {
  const { t } = useTranslation()
  let content: React.ReactNode
  if (loading) {
    content = (
      <div className='grid gap-3'>
        <Skeleton className='h-16 w-full' />
        <Skeleton className='h-16 w-full' />
      </div>
    )
  } else if (audits.length === 0) {
    content = (
      <p className='text-muted-foreground text-sm'>
        {t('No instance audit records.')}
      </p>
    )
  } else {
    content = (
      <div className='divide-y'>
        {audits.map((audit) => (
          <AuditRow key={audit.id} audit={audit} />
        ))}
      </div>
    )
  }
  return (
    <DetailSection
      title={t('Instance audit')}
      description={t(
        'Recent control-plane changes and connection checks for this instance.'
      )}
    >
      {content}
    </DetailSection>
  )
}

function AuditRow({ audit }: { audit: ManagedInstanceAudit }) {
  const { t } = useTranslation()
  const details = parseDetails(audit.details)
  return (
    <div className='grid min-w-0 gap-2 py-3 first:pt-0 last:pb-0 sm:grid-cols-[minmax(140px,0.28fr)_minmax(0,1fr)_auto] sm:items-start'>
      <div className='min-w-0'>
        <div className='font-medium'>{t(auditActionLabel(audit.action))}</div>
        <div className='text-muted-foreground text-xs'>
          {formatTimestamp(audit.created_at)}
        </div>
      </div>
      <div className='flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-xs'>
        <span className='text-muted-foreground'>
          {t('Actor')} #{audit.actor_id || t('System')}
        </span>
        {Object.entries(details).map(([key, value]) => (
          <span key={key} className='min-w-0 break-all'>
            <span className='text-muted-foreground'>{key}: </span>
            {formatDetailValue(value)}
          </span>
        ))}
      </div>
      <OutcomeBadge outcome={audit.outcome} />
    </div>
  )
}

function Info(props: {
  label: string
  value?: string
  children?: React.ReactNode
  mono?: boolean
  tone?: 'danger'
}) {
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground text-xs'>{props.label}</dt>
      <dd
        className={cn(
          'mt-1 min-w-0 break-all text-sm',
          props.mono && 'font-mono text-xs',
          props.tone === 'danger' && 'text-destructive'
        )}
      >
        {props.children ?? props.value ?? '-'}
      </dd>
    </div>
  )
}

function TaskStatusBadge({ status }: { status: ManagedInstanceTaskStatus }) {
  const { t } = useTranslation()
  return (
    <Badge
      variant='outline'
      className={cn(
        status === 'succeeded' &&
          'border-emerald-600/20 bg-emerald-600/10 text-emerald-700 dark:text-emerald-400',
        status === 'failed' &&
          'border-red-600/20 bg-red-600/10 text-red-700 dark:text-red-400',
        activeTaskStatuses.has(status) &&
          'border-blue-600/20 bg-blue-600/10 text-blue-700 dark:text-blue-400'
      )}
    >
      {t(titleCase(status))}
    </Badge>
  )
}

function OutcomeBadge({ outcome }: { outcome: string }) {
  const { t } = useTranslation()
  const succeeded = outcome === 'succeeded'
  return (
    <Badge
      variant='outline'
      className={cn(
        'w-fit',
        succeeded
          ? 'border-emerald-600/20 bg-emerald-600/10 text-emerald-700 dark:text-emerald-400'
          : 'border-red-600/20 bg-red-600/10 text-red-700 dark:text-red-400'
      )}
    >
      {t(succeeded ? 'Succeeded' : 'Failed')}
    </Badge>
  )
}

function readRecentTaskId(instanceId: number): string | null {
  if (typeof window === 'undefined') return null
  return window.sessionStorage.getItem(`managed-instance:${instanceId}:task`)
}

function writeRecentTaskId(instanceId: number, taskId: string) {
  window.sessionStorage.setItem(`managed-instance:${instanceId}:task`, taskId)
}

function parseDetails(value?: string): Record<string, unknown> {
  if (!value) return {}
  try {
    const parsed: unknown = JSON.parse(value)
    return asRecord(parsed)
  } catch {
    return { details: value }
  }
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function formatDetailValue(value: unknown): string {
  if (value == null) return '-'
  if (
    typeof value === 'string' ||
    typeof value === 'number' ||
    typeof value === 'boolean'
  ) {
    return String(value)
  }
  return JSON.stringify(value)
}

function auditActionLabel(action: string): string {
  return (
    {
      create: 'Instance created',
      update: 'Instance updated',
      credential_rotate: 'Credential rotated',
      check: 'Connection check',
      delete: 'Instance removed',
      config_binding_update: 'Configuration binding updated',
      config_drift_check: 'Configuration drift checked',
      config_apply_plan: 'Configuration apply planned',
    }[action] || action
  )
}

function titleCase(value: string): string {
  return value
    .split('_')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}
