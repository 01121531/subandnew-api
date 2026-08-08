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
import {
  CheckCircle2,
  CircleAlert,
  Clock3,
  Database,
  LoaderCircle,
  RefreshCw,
  XCircle,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { cn } from '@/lib/utils'

import {
  executeManagedInstanceBatch,
  getManagedInstanceBatch,
  planManagedInstanceBatch,
} from '../api'
import { formatTimestamp } from '../lib'
import type {
  ApiResponse,
  ManagedInstance,
  ManagedInstanceBatchItem,
  ManagedInstanceBatchStatus,
  ManagedInstanceBatchView,
  ManagedInstanceOperationStatus,
} from '../types'

const activeBatchStatuses = new Set<ManagedInstanceBatchStatus>([
  'planning',
  'planned',
  'partially_planned',
  'queued',
  'running',
])

type BatchRefreshSheetProps = {
  instances: BatchTarget[]
  onFinished: () => void
}

type BatchTarget = Pick<ManagedInstance, 'id' | 'name'>

export function BatchRefreshSheet(props: BatchRefreshSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [targets, setTargets] = useState<BatchTarget[]>([])
  const [idempotencyKey, setIdempotencyKey] = useState<string | null>(null)
  const [plannedBatch, setPlannedBatch] =
    useState<ManagedInstanceBatchView | null>(null)
  const [executedBatch, setExecutedBatch] =
    useState<ManagedInstanceBatchView | null>(null)
  const [batchId, setBatchId] = useState<string | null>(null)
  const [confirmed, setConfirmed] = useState(false)
  const [planning, setPlanning] = useState(false)
  const [executing, setExecuting] = useState(false)
  const [hasExecuted, setHasExecuted] = useState(false)
  const [requestError, setRequestError] = useState<string | null>(null)
  const notifiedBatch = useRef('')

  const batchQuery = useQuery({
    queryKey: ['managed-instance-batch', batchId],
    queryFn: () => getManagedInstanceBatch(batchId ?? ''),
    enabled: Boolean(batchId),
    retry: 1,
    refetchInterval: (query) => {
      if (query.state.status === 'error') return false
      const response = query.state.data as
        | ApiResponse<ManagedInstanceBatchView>
        | undefined
      return !response || activeBatchStatuses.has(response.data.status)
        ? 1_500
        : false
    },
  })

  const batch = batchQuery.data?.data ?? executedBatch ?? plannedBatch
  const executionActive = Boolean(
    hasExecuted && batch && activeBatchStatuses.has(batch.status)
  )
  const busy = planning || executing || (executionActive && !batchQuery.isError)
  const canStart = props.instances.length >= 2 && props.instances.length <= 50
  const instanceNames = useMemo(
    () => new Map(targets.map((instance) => [instance.id, instance.name])),
    [targets]
  )

  useEffect(() => {
    if (!hasExecuted || !batch || activeBatchStatuses.has(batch.status)) return
    if (notifiedBatch.current === batch.batch_id) return
    notifiedBatch.current = batch.batch_id

    if (batch.status === 'succeeded') {
      toast.success(t('Batch refresh completed'))
    } else if (batch.status === 'partially_failed') {
      toast.warning(t('Batch refresh completed with partial failures'))
    } else if (batch.status === 'needs_reconcile') {
      toast.warning(t('Manual reconciliation required'))
    } else {
      toast.error(t('Batch refresh failed'))
    }

    void queryClient.invalidateQueries({ queryKey: ['managed-instances'] })
    for (const item of batch.items) {
      void queryClient.invalidateQueries({
        queryKey: ['managed-instance-inventory', item.instance_id],
      })
      void queryClient.invalidateQueries({
        queryKey: ['managed-instance', item.instance_id],
      })
    }
  }, [batch, hasExecuted, queryClient, t])

  const reset = () => {
    setTargets([])
    setIdempotencyKey(null)
    setPlannedBatch(null)
    setExecutedBatch(null)
    setBatchId(null)
    setConfirmed(false)
    setPlanning(false)
    setExecuting(false)
    setHasExecuted(false)
    setRequestError(null)
    notifiedBatch.current = ''
  }

  const createPlan = async () => {
    if (!canStart || planning) return

    const targetSnapshot = props.instances.map(({ id, name }) => ({ id, name }))
    const key = createIdempotencyKey()
    setTargets(targetSnapshot)
    setIdempotencyKey(key)
    setOpen(true)
    setPlanning(true)
    setRequestError(null)

    try {
      const response = await planManagedInstanceBatch({
        action: 'refresh_inventory',
        idempotency_key: key,
        targets: targetSnapshot.map((instance) => ({
          instance_id: instance.id,
          parameters: {},
        })),
      })
      if (!response.success) {
        setRequestError(t('Could not create the batch plan.'))
        return
      }
      setPlannedBatch(response.data)
    } catch {
      setRequestError(t('Could not create the batch plan.'))
    } finally {
      setPlanning(false)
    }
  }

  const execute = async () => {
    if (!plannedBatch || !idempotencyKey || executing) return

    setExecuting(true)
    setRequestError(null)
    try {
      const response = await executeManagedInstanceBatch({
        batch_id: plannedBatch.batch_id,
        idempotency_key: idempotencyKey,
      })
      if (!response.success) {
        setRequestError(t('Could not execute the batch.'))
        return
      }
      setExecutedBatch(response.data)
      setBatchId(response.data.batch_id)
      setHasExecuted(true)
      setConfirmed(false)
      toast.success(t('Batch refresh queued'))
    } catch {
      setRequestError(t('Could not execute the batch.'))
    } finally {
      setExecuting(false)
    }
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && busy) return
    setOpen(nextOpen)
    if (!nextOpen) {
      if (hasExecuted) props.onFinished()
      reset()
    }
  }

  return (
    <>
      <Button size='sm' disabled={!canStart || busy} onClick={createPlan}>
        <Database />
        {t('Refresh resources')}
      </Button>

      <Sheet open={open} onOpenChange={handleOpenChange}>
        <SheetContent
          className='w-[calc(100%-1rem)] sm:max-w-2xl'
          showCloseButton={!busy}
        >
          <SheetHeader className='border-b'>
            <div className='flex min-w-0 items-start gap-3 pr-8'>
              <div className='bg-muted text-muted-foreground grid size-9 shrink-0 place-items-center rounded-md'>
                <Database className='size-4' />
              </div>
              <div className='min-w-0'>
                <SheetTitle>{t('Batch refresh resources')}</SheetTitle>
                <SheetDescription>
                  {t(
                    'Review each planned instance before explicitly confirming execution.'
                  )}
                </SheetDescription>
              </div>
            </div>
          </SheetHeader>

          <div className='min-h-0 flex-1 overflow-y-auto px-4 pb-4'>
            {planning && <PlanningState count={targets.length} />}

            {requestError && (
              <div
                role='alert'
                className='border-destructive/30 bg-destructive/5 text-destructive mt-4 flex items-start gap-2 rounded-md border p-3 text-sm'
              >
                <CircleAlert className='mt-0.5 size-4 shrink-0' />
                <span>{requestError}</span>
              </div>
            )}

            {batch && !planning && (
              <BatchContent
                batch={batch}
                instanceNames={instanceNames}
                refreshing={batchQuery.isFetching}
                pollingError={batchQuery.isError}
                onRetry={() => void batchQuery.refetch()}
              />
            )}

            {plannedBatch && !hasExecuted && !planning && (
              <div className='mt-4 flex items-start gap-2 rounded-md border p-3'>
                <Checkbox
                  id='confirm-managed-instance-batch'
                  className='mt-0.5'
                  checked={confirmed}
                  disabled={executing || plannedBatch.summary.planned === 0}
                  onCheckedChange={setConfirmed}
                />
                <Label
                  htmlFor='confirm-managed-instance-batch'
                  className='items-start text-xs leading-5 font-normal'
                >
                  {t(
                    'I reviewed every instance result and confirm execution for all successfully planned targets.'
                  )}
                </Label>
              </div>
            )}
          </div>

          <SheetFooter className='border-t sm:flex-row sm:justify-end'>
            {!hasExecuted && (
              <>
                <Button
                  variant='outline'
                  disabled={planning || executing}
                  onClick={() => handleOpenChange(false)}
                >
                  {t('Cancel')}
                </Button>
                <Button
                  disabled={
                    !plannedBatch ||
                    plannedBatch.summary.planned === 0 ||
                    !confirmed ||
                    executing
                  }
                  onClick={() => void execute()}
                >
                  {executing ? (
                    <LoaderCircle className='animate-spin' />
                  ) : (
                    <CheckCircle2 />
                  )}
                  {t('Confirm and execute')}
                </Button>
              </>
            )}
            {hasExecuted && (!executionActive || batchQuery.isError) && (
              <Button onClick={() => handleOpenChange(false)}>
                {t('Close')}
              </Button>
            )}
            {executionActive && !batchQuery.isError && (
              <div className='text-muted-foreground flex items-center gap-2 text-xs'>
                <LoaderCircle className='size-4 animate-spin' />
                {t('Waiting for all instances to finish')}
              </div>
            )}
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </>
  )
}

function PlanningState({ count }: { count: number }) {
  const { t } = useTranslation()
  return (
    <div className='grid min-h-56 place-items-center text-center' role='status'>
      <div>
        <LoaderCircle className='text-muted-foreground mx-auto size-6 animate-spin' />
        <div className='mt-3 text-sm font-medium'>
          {t('Creating batch plan')}
        </div>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t('Checking {{count}} selected instances', { count })}
        </p>
      </div>
    </div>
  )
}

function BatchContent(props: {
  batch: ManagedInstanceBatchView
  instanceNames: ReadonlyMap<number, string>
  refreshing: boolean
  pollingError: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()
  const completed =
    props.batch.summary.succeeded +
    props.batch.summary.failed +
    props.batch.summary.unknown
  const progress = props.batch.summary.total
    ? (completed / props.batch.summary.total) * 100
    : 0

  return (
    <div className='grid gap-4 pt-4' aria-live='polite'>
      <div className='flex min-w-0 flex-wrap items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='text-muted-foreground text-xs'>{t('Batch ID')}</div>
          <div className='mt-0.5 font-mono text-xs break-all'>
            {props.batch.batch_id}
          </div>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          {props.batch.idempotent_replay && (
            <Badge variant='secondary' className='rounded-md'>
              {t('Idempotent replay')}
            </Badge>
          )}
          <BatchStatusBadge
            status={props.batch.status}
            refreshing={props.refreshing}
          />
        </div>
      </div>

      <div className='grid grid-cols-2 overflow-hidden rounded-lg border sm:grid-cols-6'>
        <SummaryValue label={t('Total')} value={props.batch.summary.total} />
        <SummaryValue
          label={t('Planned')}
          value={props.batch.summary.planned}
        />
        <SummaryValue label={t('Active')} value={props.batch.summary.active} />
        <SummaryValue
          label={t('Succeeded')}
          value={props.batch.summary.succeeded}
          tone='success'
        />
        <SummaryValue
          label={t('Failed')}
          value={props.batch.summary.failed}
          tone='danger'
        />
        <SummaryValue
          label={t('Unknown')}
          value={props.batch.summary.unknown}
          tone='warning'
        />
      </div>

      {props.batch.status === 'needs_reconcile' && (
        <div
          role='alert'
          className='flex items-start gap-2 rounded-md border border-amber-600/30 bg-amber-600/5 p-3 text-sm text-amber-800 dark:text-amber-300'
        >
          <CircleAlert className='mt-0.5 size-4 shrink-0' />
          <div>
            <div className='font-medium'>
              {t('Manual reconciliation required')}
            </div>
            <p className='mt-0.5 text-xs'>
              {t(
                'One or more remote write outcomes are unknown after lease loss. Verify the remote state manually; these operations will not be retried automatically.'
              )}
            </p>
          </div>
        </div>
      )}

      {props.batch.executed_at > 0 && (
        <Progress value={progress} aria-label={t('Batch progress')} />
      )}

      {props.pollingError && (
        <div
          role='alert'
          className='flex flex-wrap items-center justify-between gap-3 rounded-md border p-3'
        >
          <div>
            <div className='text-sm font-medium'>{t('Result unavailable')}</div>
            <p className='text-muted-foreground text-xs'>
              {t('The batch result could not be refreshed.')}
            </p>
          </div>
          <Button variant='outline' size='sm' onClick={props.onRetry}>
            <RefreshCw />
            {t('Retry')}
          </Button>
        </div>
      )}

      <div className='overflow-hidden rounded-lg border'>
        <div className='bg-muted/40 text-muted-foreground grid grid-cols-[minmax(0,1fr)_auto] gap-3 border-b px-3 py-2 text-xs font-medium'>
          <span>{t('Instance results')}</span>
          <span>
            {t('{{count}} items', { count: props.batch.items.length })}
          </span>
        </div>
        <div className='divide-y'>
          {[...props.batch.items]
            .sort((left, right) => left.position - right.position)
            .map((item) => (
              <BatchItemRow
                key={`${item.position}-${item.instance_id}`}
                item={item}
                name={
                  props.instanceNames.get(item.instance_id) ||
                  t('Instance #{{id}}', { id: item.instance_id })
                }
              />
            ))}
        </div>
      </div>

      <dl className='grid gap-3 text-xs sm:grid-cols-3'>
        <Timestamp label={t('Planned')} value={props.batch.planned_at} />
        <Timestamp label={t('Started')} value={props.batch.executed_at} />
        <Timestamp label={t('Finished')} value={props.batch.finished_at} />
      </dl>
    </div>
  )
}

function SummaryValue(props: {
  label: string
  value: number
  tone?: 'success' | 'danger' | 'warning'
  className?: string
}) {
  return (
    <div
      className={cn(
        'border-border min-w-0 border-r border-b px-3 py-2.5 last:border-r-0 sm:border-b-0',
        props.className
      )}
    >
      <div className='text-muted-foreground truncate text-xs'>
        {props.label}
      </div>
      <div
        className={cn(
          'mt-0.5 text-lg font-semibold tabular-nums',
          props.tone === 'success' && 'text-emerald-700 dark:text-emerald-400',
          props.tone === 'danger' && 'text-red-700 dark:text-red-400',
          props.tone === 'warning' && 'text-amber-700 dark:text-amber-400'
        )}
      >
        {props.value}
      </div>
    </div>
  )
}

function BatchItemRow(props: { item: ManagedInstanceBatchItem; name: string }) {
  return (
    <div className='grid min-w-0 gap-2 px-3 py-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'>
      <div className='flex min-w-0 items-start gap-2.5'>
        <ItemStatusIcon status={props.item.status} />
        <div className='min-w-0'>
          <div className='truncate text-sm font-medium'>{props.name}</div>
          <div className='text-muted-foreground mt-0.5 font-mono text-[11px]'>
            #{props.item.instance_id}
          </div>
          {props.item.error_code && (
            <div className='text-destructive mt-1 font-mono text-xs break-all'>
              {props.item.error_code}
            </div>
          )}
        </div>
      </div>
      <OperationStatusBadge status={props.item.status} />
    </div>
  )
}

function BatchStatusBadge(props: {
  status: ManagedInstanceBatchStatus
  refreshing: boolean
}) {
  const { t } = useTranslation()
  const active = activeBatchStatuses.has(props.status)
  return (
    <Badge
      variant='outline'
      className={cn(
        'rounded-md',
        props.status === 'succeeded' &&
          'border-emerald-600/20 bg-emerald-600/10 text-emerald-700 dark:text-emerald-400',
        (props.status === 'failed' || props.status === 'partially_failed') &&
          'border-red-600/20 bg-red-600/10 text-red-700 dark:text-red-400',
        props.status === 'partially_planned' &&
          'border-amber-600/20 bg-amber-600/10 text-amber-700 dark:text-amber-400',
        props.status === 'needs_reconcile' &&
          'border-amber-600/30 bg-amber-600/10 text-amber-800 dark:text-amber-300',
        active &&
          props.status !== 'partially_planned' &&
          'border-blue-600/20 bg-blue-600/10 text-blue-700 dark:text-blue-400'
      )}
    >
      {(active || props.refreshing) && (
        <LoaderCircle className='animate-spin' />
      )}
      {t(batchStatusLabel(props.status))}
    </Badge>
  )
}

function OperationStatusBadge({
  status,
}: {
  status: ManagedInstanceOperationStatus
}) {
  const { t } = useTranslation()
  return (
    <Badge
      variant='outline'
      className={cn(
        'rounded-md',
        status === 'succeeded' &&
          'border-emerald-600/20 bg-emerald-600/10 text-emerald-700 dark:text-emerald-400',
        status === 'failed' &&
          'border-red-600/20 bg-red-600/10 text-red-700 dark:text-red-400',
        status === 'unknown' &&
          'border-amber-600/30 bg-amber-600/10 text-amber-800 dark:text-amber-300',
        (status === 'queued' || status === 'running') &&
          'border-blue-600/20 bg-blue-600/10 text-blue-700 dark:text-blue-400'
      )}
    >
      {(status === 'queued' || status === 'running') && (
        <LoaderCircle className='animate-spin' />
      )}
      {t(operationStatusLabel(status))}
    </Badge>
  )
}

function ItemStatusIcon({
  status,
}: {
  status: ManagedInstanceOperationStatus
}) {
  if (status === 'succeeded') {
    return <CheckCircle2 className='mt-0.5 size-4 shrink-0 text-emerald-600' />
  }
  if (status === 'failed') {
    return <XCircle className='text-destructive mt-0.5 size-4 shrink-0' />
  }
  if (status === 'unknown') {
    return <CircleAlert className='mt-0.5 size-4 shrink-0 text-amber-600' />
  }
  if (status === 'queued' || status === 'running') {
    return (
      <LoaderCircle className='mt-0.5 size-4 shrink-0 animate-spin text-blue-600' />
    )
  }
  return <Clock3 className='text-muted-foreground mt-0.5 size-4 shrink-0' />
}

function Timestamp({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <dt className='text-muted-foreground'>{label}</dt>
      <dd className='mt-0.5'>{formatTimestamp(value)}</dd>
    </div>
  )
}

function batchStatusLabel(status: ManagedInstanceBatchStatus): string {
  return {
    planning: 'Planning',
    planned: 'Planned',
    partially_planned: 'Partially planned',
    queued: 'Queued',
    running: 'Running',
    succeeded: 'Succeeded',
    partially_failed: 'Partially failed',
    failed: 'Failed',
    needs_reconcile: 'Needs reconciliation',
  }[status]
}

function operationStatusLabel(status: ManagedInstanceOperationStatus): string {
  return {
    planned: 'Planned',
    queued: 'Queued',
    running: 'Running',
    succeeded: 'Succeeded',
    failed: 'Failed',
    unknown: 'Unknown',
  }[status]
}

function createIdempotencyKey(): string {
  const random = globalThis.crypto?.randomUUID?.()
  if (random) return `managed-batch-${random}`
  return `managed-batch-${Date.now()}-${Math.random().toString(36).slice(2)}`
}
