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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Database,
  FlaskConical,
  LoaderCircle,
  Power,
  RefreshCw,
  ShieldCheck,
  TriangleAlert,
  type LucideIcon,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  executeManagedInstanceOperation,
  getManagedInstanceOperation,
  planManagedInstanceOperation,
} from '../api'
import { formatTimestamp } from '../lib'
import type {
  ApiResponse,
  ManagedInstance,
  ManagedInstanceOperation,
  ManagedInstanceOperationAction,
  ManagedInstanceOperationParameters,
  ManagedInstanceOperationStatus,
} from '../types'

type ControlledOperationsPanelProps = {
  instance: ManagedInstance
}

type ActionDefinition = {
  action: ManagedInstanceOperationAction
  capability: string
  icon: LucideIcon
  title: string
  description: string
  writesRemote: boolean
}

type PlannedOperation = {
  operation: ManagedInstanceOperation
  idempotencyKey: string
}

const activeOperationStatuses = new Set<ManagedInstanceOperationStatus>([
  'planned',
  'queued',
  'running',
])

export function ControlledOperationsPanel({
  instance,
}: ControlledOperationsPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [resourceIdsText, setResourceIdsText] = useState('')
  const [resourceIdText, setResourceIdText] = useState('')
  const [resourceEnabled, setResourceEnabled] = useState(true)
  const [planned, setPlanned] = useState<PlannedOperation | null>(null)
  const [confirmed, setConfirmed] = useState(false)
  const [operationId, setOperationId] = useState<string | null>(null)
  const notifiedTerminalOperation = useRef('')

  const definitions = useMemo(() => operationDefinitions(instance), [instance])
  const resourceIds = useMemo(
    () => parseResourceIds(resourceIdsText),
    [resourceIdsText]
  )
  const resourceId = parsePositiveInteger(resourceIdText)

  const planMutation = useMutation({
    mutationFn: (input: {
      action: ManagedInstanceOperationAction
      parameters: ManagedInstanceOperationParameters
      idempotencyKey: string
    }) =>
      planManagedInstanceOperation(instance.id, {
        action: input.action,
        parameters: input.parameters,
        idempotency_key: input.idempotencyKey,
      }),
    onSuccess: (response, variables) => {
      if (!response.success) return
      setConfirmed(false)
      setPlanned({
        operation: response.data,
        idempotencyKey: variables.idempotencyKey,
      })
    },
  })

  const executeMutation = useMutation({
    mutationFn: (input: PlannedOperation) =>
      executeManagedInstanceOperation(instance.id, {
        operation_id: input.operation.operation_id,
        idempotency_key: input.idempotencyKey,
      }),
    onSuccess: (response) => {
      if (!response.success) return
      const operation = response.data.operation
      setOperationId(operation.operation_id)
      setPlanned(null)
      setConfirmed(false)
      queryClient.setQueryData<ApiResponse<ManagedInstanceOperation>>(
        ['managed-instance-operation', instance.id, operation.operation_id],
        { success: true, message: '', data: operation }
      )
      toast.success(t('Controlled operation queued'))
    },
  })

  const operationQuery = useQuery({
    queryKey: ['managed-instance-operation', instance.id, operationId],
    queryFn: () => getManagedInstanceOperation(instance.id, operationId ?? ''),
    enabled: Boolean(operationId),
    retry: 1,
    refetchInterval: (query) => {
      if (query.state.status === 'error') return false
      const response = query.state.data as
        | ApiResponse<ManagedInstanceOperation>
        | undefined
      return !response || activeOperationStatuses.has(response.data.status)
        ? 1_500
        : false
    },
  })

  const operation =
    operationQuery.data?.data ?? executeMutation.data?.data.operation
  const operationBusy = Boolean(
    executeMutation.isPending ||
    (operation && activeOperationStatuses.has(operation.status))
  )

  useEffect(() => {
    if (!operation || activeOperationStatuses.has(operation.status)) return
    if (notifiedTerminalOperation.current === operation.operation_id) return
    notifiedTerminalOperation.current = operation.operation_id
    if (operation.status === 'succeeded') {
      toast.success(t('Controlled operation completed'))
    } else if (operation.status === 'unknown') {
      toast.warning(
        t(
          'Operation outcome is unknown. Verify remote state manually; it will not be retried automatically.'
        )
      )
    } else {
      toast.error(
        t('Controlled operation failed: {{code}}', {
          code: operation.error_code || 'operation_failed',
        })
      )
    }
    void queryClient.invalidateQueries({
      queryKey: ['managed-instance', instance.id],
    })
    void queryClient.invalidateQueries({
      queryKey: ['managed-instance-audits', instance.id],
    })
    void queryClient.invalidateQueries({ queryKey: ['managed-instances'] })
    void queryClient.invalidateQueries({
      queryKey: ['managed-instance-inventory', instance.id],
    })
    void queryClient.invalidateQueries({
      queryKey: ['managed-instance-metrics', instance.id],
    })
    void queryClient.invalidateQueries({
      queryKey: ['managed-instance-alerts', instance.id],
    })
  }, [instance.id, operation, queryClient, t])

  if (definitions.length === 0) return null

  const startPlan = (
    action: ManagedInstanceOperationAction,
    parameters: ManagedInstanceOperationParameters
  ) => {
    planMutation.mutate({
      action,
      parameters,
      idempotencyKey: createIdempotencyKey(),
    })
  }

  return (
    <>
      <section className='min-w-0 rounded-lg border'>
        <header className='border-b px-4 py-3'>
          <h2 className='text-sm font-semibold'>
            {t('Controlled operations')}
          </h2>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {t(
              'Every action is planned and reviewed before asynchronous execution.'
            )}
          </p>
        </header>
        <div className='divide-y px-4'>
          {definitions.map((definition) => {
            const planning = planMutation.isPending
            const observeBlocked =
              definition.writesRemote && instance.management_mode === 'observe'
            let toggleValidationMessage: string | undefined
            if (observeBlocked) {
              toggleValidationMessage = t(
                'Observe mode blocks remote state changes. Change the management mode before toggling a resource.'
              )
            } else if (resourceIdText && resourceId == null) {
              toggleValidationMessage = t('Enter a positive resource ID')
            }

            if (definition.action === 'refresh_inventory') {
              return (
                <OperationRow
                  key={definition.action}
                  definition={definition}
                  planning={planning}
                  disabled={operationBusy}
                  onPlan={() => startPlan(definition.action, {})}
                />
              )
            }

            if (definition.action === 'test_resources') {
              return (
                <OperationRow
                  key={definition.action}
                  definition={definition}
                  planning={planning}
                  disabled={operationBusy || !resourceIds.value}
                  validationMessage={
                    resourceIdsText && !resourceIds.value
                      ? t(resourceIds.error || 'Invalid resource IDs')
                      : undefined
                  }
                  onPlan={() =>
                    resourceIds.value &&
                    startPlan(definition.action, {
                      resource_ids: resourceIds.value,
                    })
                  }
                >
                  <div className='grid gap-1.5'>
                    <Label htmlFor='managed-operation-resource-ids'>
                      {t('Resource IDs')}
                    </Label>
                    <Input
                      id='managed-operation-resource-ids'
                      value={resourceIdsText}
                      onChange={(event) =>
                        setResourceIdsText(event.target.value)
                      }
                      placeholder={t('For example: 12, 18, 27')}
                      autoComplete='off'
                      aria-invalid={Boolean(
                        resourceIdsText && !resourceIds.value
                      )}
                    />
                  </div>
                </OperationRow>
              )
            }

            return (
              <OperationRow
                key={definition.action}
                definition={definition}
                planning={planning}
                disabled={operationBusy || observeBlocked || resourceId == null}
                validationMessage={toggleValidationMessage}
                warning={observeBlocked}
                onPlan={() =>
                  resourceId != null &&
                  !observeBlocked &&
                  startPlan(definition.action, {
                    resource_id: resourceId,
                    enabled: resourceEnabled,
                  })
                }
              >
                <div className='grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end'>
                  <div className='grid gap-1.5'>
                    <Label htmlFor='managed-operation-resource-id'>
                      {t('Resource ID')}
                    </Label>
                    <Input
                      id='managed-operation-resource-id'
                      type='number'
                      min={1}
                      step={1}
                      value={resourceIdText}
                      onChange={(event) =>
                        setResourceIdText(event.target.value)
                      }
                      placeholder={t('Enter resource ID')}
                      autoComplete='off'
                      aria-invalid={Boolean(
                        resourceIdText && resourceId == null
                      )}
                    />
                  </div>
                  <div className='flex h-8 items-center gap-2'>
                    <Switch
                      id='managed-operation-resource-enabled'
                      checked={resourceEnabled}
                      onCheckedChange={setResourceEnabled}
                      disabled={observeBlocked}
                    />
                    <Label htmlFor='managed-operation-resource-enabled'>
                      {resourceEnabled ? t('Enable') : t('Disable')}
                    </Label>
                  </div>
                </div>
              </OperationRow>
            )
          })}
        </div>

        {(operation || operationQuery.isError) && (
          <div className='border-t p-4'>
            <OperationResult
              operation={operation}
              loading={operationQuery.isFetching}
              error={operationQuery.isError}
              onRetry={() => void operationQuery.refetch()}
            />
          </div>
        )}
      </section>

      <PlanConfirmationDialog
        planned={planned}
        confirmed={confirmed}
        executing={executeMutation.isPending}
        onConfirmedChange={setConfirmed}
        onOpenChange={(open) => {
          if (!open && !executeMutation.isPending) {
            setPlanned(null)
            setConfirmed(false)
          }
        }}
        onExecute={() => planned && executeMutation.mutate(planned)}
      />
    </>
  )
}

function OperationRow(props: {
  definition: ActionDefinition
  planning: boolean
  disabled?: boolean
  validationMessage?: string
  warning?: boolean
  children?: React.ReactNode
  onPlan: () => void
}) {
  const { t } = useTranslation()
  const Icon = props.definition.icon
  return (
    <div className='grid min-w-0 gap-3 py-4 lg:grid-cols-[minmax(220px,0.65fr)_minmax(260px,1fr)_auto] lg:items-center'>
      <div className='flex min-w-0 items-start gap-3'>
        <div className='bg-muted text-muted-foreground grid size-8 shrink-0 place-items-center rounded-md'>
          <Icon className='size-4' />
        </div>
        <div className='min-w-0'>
          <div className='text-sm font-medium'>{t(props.definition.title)}</div>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {t(props.definition.description)}
          </p>
          <Badge variant='outline' className='mt-2 font-mono text-[11px]'>
            {props.definition.capability}
          </Badge>
        </div>
      </div>
      <div className='min-w-0'>
        {props.children}
        {props.validationMessage && (
          <p
            role='alert'
            className={cn(
              'mt-1.5 flex items-start gap-1 text-xs',
              props.warning
                ? 'text-amber-700 dark:text-amber-400'
                : 'text-destructive'
            )}
          >
            {props.warning && (
              <TriangleAlert className='mt-0.5 size-3 shrink-0' />
            )}
            <span>{props.validationMessage}</span>
          </p>
        )}
      </div>
      <Button
        variant='outline'
        size='sm'
        disabled={props.disabled || props.planning}
        onClick={props.onPlan}
      >
        {props.planning ? (
          <LoaderCircle className='animate-spin' />
        ) : (
          <ShieldCheck />
        )}
        {t('Plan action')}
      </Button>
    </div>
  )
}

function PlanConfirmationDialog(props: {
  planned: PlannedOperation | null
  confirmed: boolean
  executing: boolean
  onConfirmedChange: (confirmed: boolean) => void
  onOpenChange: (open: boolean) => void
  onExecute: () => void
}) {
  const { t } = useTranslation()
  const operation = props.planned?.operation
  const plan = operation?.plan
  return (
    <AlertDialog open={Boolean(operation)} onOpenChange={props.onOpenChange}>
      <AlertDialogContent className='max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg'>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <ShieldCheck />
          </AlertDialogMedia>
          <AlertDialogTitle>{t('Review operation plan')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(
              'Review the server-generated plan, then explicitly confirm execution.'
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>

        {operation && plan && (
          <div className='grid min-w-0 gap-3 rounded-md border p-3'>
            <PlanField
              label={t('Action')}
              value={t(actionTitle(operation.action))}
            />
            <PlanField
              label={t('Summary')}
              value={localizedPlanSummary(operation, t)}
            />
            <div className='grid gap-3 sm:grid-cols-2'>
              <PlanField label={t('Risk level')}>
                <Badge variant='secondary'>{t('Low')}</Badge>
              </PlanField>
              <PlanField
                label={t('Remote write')}
                value={plan.writes_remote ? t('Yes') : t('No')}
              />
              <PlanField
                label={t('Target')}
                value={operationTarget(operation, {
                  all: t('All remote resources'),
                  enable: t('Enable'),
                  disable: t('Disable'),
                })}
              />
              <PlanField
                label={t('Required capability')}
                value={plan.required_capability}
                mono
              />
            </div>
            <PlanField
              label={t('Operation ID')}
              value={operation.operation_id}
              mono
            />
          </div>
        )}

        <div className='flex items-start gap-2 rounded-md border p-3'>
          <Checkbox
            id='confirm-managed-operation'
            checked={props.confirmed}
            onCheckedChange={(checked) =>
              props.onConfirmedChange(checked === true)
            }
            disabled={props.executing}
            className='mt-0.5'
          />
          <Label
            htmlFor='confirm-managed-operation'
            className='items-start text-xs leading-5 font-normal'
          >
            {t(
              'I reviewed the target, parameters, capability and remote-write impact.'
            )}
          </Label>
        </div>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.executing}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={!props.confirmed || props.executing}
            onClick={props.onExecute}
          >
            {props.executing && <LoaderCircle className='animate-spin' />}
            {t('Confirm and execute')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function PlanField(props: {
  label: string
  value?: string
  mono?: boolean
  children?: React.ReactNode
}) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div
        className={cn(
          'mt-0.5 min-w-0 break-all text-sm',
          props.mono && 'font-mono text-xs'
        )}
      >
        {props.children ?? props.value ?? '-'}
      </div>
    </div>
  )
}

function OperationResult(props: {
  operation?: ManagedInstanceOperation
  loading: boolean
  error: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()
  if (props.error) {
    return (
      <div
        role='alert'
        className='flex flex-wrap items-center justify-between gap-3'
      >
        <div>
          <div className='text-sm font-medium'>{t('Result unavailable')}</div>
          <p className='text-muted-foreground text-xs'>
            {t('The operation result could not be refreshed.')}
          </p>
        </div>
        <Button variant='outline' size='sm' onClick={props.onRetry}>
          <RefreshCw />
          {t('Retry')}
        </Button>
      </div>
    )
  }
  if (!props.operation) return null

  const result = props.operation.result
  return (
    <div className='grid min-w-0 gap-3' role='status' aria-live='polite'>
      <div className='flex min-w-0 flex-wrap items-center justify-between gap-2'>
        <div>
          <h3 className='text-sm font-semibold'>{t('Latest operation')}</h3>
          <p className='text-muted-foreground font-mono text-xs break-all'>
            {props.operation.operation_id}
          </p>
        </div>
        <OperationStatusBadge
          status={props.operation.status}
          loading={props.loading}
        />
      </div>

      <dl className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
        <PlanField
          label={t('Action')}
          value={t(actionTitle(props.operation.action))}
        />
        <PlanField
          label={t('Started')}
          value={formatTimestamp(props.operation.executed_at)}
        />
        <PlanField
          label={t('Finished')}
          value={formatTimestamp(props.operation.finished_at)}
        />
        <PlanField
          label={t('Result count')}
          value={result?.count == null ? '-' : String(result.count)}
        />
      </dl>

      {props.operation.error_code && (
        <div className='border-destructive/30 bg-destructive/5 text-destructive rounded-md border px-3 py-2 font-mono text-xs break-all'>
          {props.operation.error_code}
        </div>
      )}

      {result?.items && result.items.length > 0 && (
        <div className='overflow-x-auto rounded-md border'>
          <table className='w-full min-w-96 text-left text-xs'>
            <thead className='bg-muted/50 text-muted-foreground'>
              <tr>
                <th className='px-3 py-2 font-medium'>{t('Resource ID')}</th>
                <th className='px-3 py-2 font-medium'>{t('Outcome')}</th>
                <th className='px-3 py-2 font-medium'>{t('Enabled state')}</th>
              </tr>
            </thead>
            <tbody className='divide-y'>
              {result.items.map((item) => (
                <tr key={item.resource_id}>
                  <td className='px-3 py-2 font-mono'>{item.resource_id}</td>
                  <td className='px-3 py-2'>
                    {t(item.succeeded ? 'Succeeded' : 'Failed')}
                  </td>
                  <td className='px-3 py-2'>
                    {item.enabled == null
                      ? '-'
                      : t(item.enabled ? 'Enabled' : 'Disabled')}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function OperationStatusBadge(props: {
  status: ManagedInstanceOperationStatus
  loading: boolean
}) {
  const { t } = useTranslation()
  const active = activeOperationStatuses.has(props.status)
  return (
    <Badge
      variant='outline'
      className={cn(
        props.status === 'succeeded' &&
          'border-emerald-600/20 bg-emerald-600/10 text-emerald-700 dark:text-emerald-400',
        props.status === 'failed' &&
          'border-red-600/20 bg-red-600/10 text-red-700 dark:text-red-400',
        props.status === 'unknown' &&
          'border-amber-600/30 bg-amber-600/10 text-amber-800 dark:text-amber-300',
        active &&
          'border-blue-600/20 bg-blue-600/10 text-blue-700 dark:text-blue-400'
      )}
    >
      {(active || props.loading) && <LoaderCircle className='animate-spin' />}
      {t(titleCase(props.status))}
    </Badge>
  )
}

function operationDefinitions(instance: ManagedInstance): ActionDefinition[] {
  let prefix: 'accounts' | 'channels' | null = null
  if (instance.kind === 'sub2api') {
    prefix = 'accounts'
  } else if (instance.kind === 'new_api' || instance.kind === 'huichuan') {
    prefix = 'channels'
  }
  if (!prefix) return []

  const definitions: ActionDefinition[] = [
    {
      action: 'refresh_inventory',
      capability: `${prefix}.list`,
      icon: Database,
      title: 'Refresh inventory',
      description: 'Read the remote resource inventory and return its count.',
      writesRemote: false,
    },
    {
      action: 'test_resources',
      capability: `${prefix}.test`,
      icon: FlaskConical,
      title: 'Test resources',
      description: 'Run connectivity tests for 1 to 20 remote resource IDs.',
      writesRemote: false,
    },
    {
      action: 'toggle_resource',
      capability: `${prefix}.toggle`,
      icon: Power,
      title: 'Toggle resource',
      description: 'Enable or disable one remote resource.',
      writesRemote: true,
    },
  ]
  const capabilities = new Set(instance.capabilities)
  return definitions.filter((definition) =>
    capabilities.has(definition.capability)
  )
}

function operationTarget(
  operation: ManagedInstanceOperation,
  labels: { all: string; enable: string; disable: string }
): string {
  if (operation.action === 'refresh_inventory') return labels.all
  if (operation.action === 'test_resources') {
    return operation.parameters.resource_ids?.join(', ') || '-'
  }
  const enabled = operation.parameters.enabled ? labels.enable : labels.disable
  return `#${operation.parameters.resource_id ?? '-'} (${enabled})`
}

function localizedPlanSummary(
  operation: ManagedInstanceOperation,
  translate: (key: string, options?: { count: number }) => string
): string {
  if (operation.action === 'refresh_inventory') {
    return translate('Refresh the remote resource inventory summary')
  }
  if (operation.action === 'test_resources') {
    return translate('Test {{count}} remote resources', {
      count: operation.plan.target_count,
    })
  }
  return translate('Change one remote resource enabled state')
}

function parseResourceIds(value: string): {
  value?: number[]
  error?: string
} {
  const parts = value
    .trim()
    .split(/[\s,]+/)
    .filter(Boolean)
  if (parts.length === 0) return {}
  if (parts.length > 20) return { error: 'Use no more than 20 resource IDs' }
  const ids = parts.map((part) => Number(part))
  if (ids.some((id) => !Number.isSafeInteger(id) || id <= 0)) {
    return { error: 'Resource IDs must be positive integers' }
  }
  if (new Set(ids).size !== ids.length) {
    return { error: 'Resource IDs must be unique' }
  }
  return { value: ids }
}

function parsePositiveInteger(value: string): number | null {
  if (!value.trim()) return null
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null
}

function createIdempotencyKey(): string {
  const random = globalThis.crypto?.randomUUID?.()
  if (random) return `managed-operation-${random}`
  return `managed-operation-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function actionTitle(action: ManagedInstanceOperationAction): string {
  return (
    {
      refresh_inventory: 'Refresh inventory',
      test_resources: 'Test resources',
      toggle_resource: 'Toggle resource',
      apply_config: 'Apply configuration',
    } as const
  )[action]
}

function titleCase(value: string): string {
  return value
    .split('_')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}
