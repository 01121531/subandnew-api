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
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  Bell,
  Boxes,
  CircleAlert,
  LoaderCircle,
  RefreshCw,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import {
  getManagedInstanceAlerts,
  getManagedInstanceInventory,
  getManagedInstanceMetrics,
} from '../api'
import { formatTimestamp } from '../lib'
import type {
  ManagedInstance,
  ManagedInstanceAlert,
  ManagedInstanceCollectionStatus,
  ManagedInstanceMetricSample,
} from '../types'

type ObservationTab = 'inventory' | 'metrics' | 'alerts'

export function ObservabilityPanel({
  instance,
}: {
  instance: ManagedInstance
}) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<ObservationTab>('inventory')
  const [inventoryCursor, setInventoryCursor] = useState('')
  const [inventoryHistory, setInventoryHistory] = useState<string[]>([])
  const supportsInventory = useMemo(
    () =>
      instance.capabilities.some(
        (capability) =>
          capability === 'channels.list' || capability === 'accounts.list'
      ),
    [instance.capabilities]
  )
  const inventoryQuery = useQuery({
    queryKey: ['managed-instance-inventory', instance.id, inventoryCursor],
    queryFn: () =>
      getManagedInstanceInventory(instance.id, 'auto', inventoryCursor),
    enabled: tab === 'inventory' && supportsInventory,
    staleTime: 60_000,
  })

  useEffect(() => {
    setInventoryCursor('')
    setInventoryHistory([])
  }, [instance.id])
  const metricsQuery = useQuery({
    queryKey: ['managed-instance-metrics', instance.id],
    queryFn: () => getManagedInstanceMetrics(instance.id),
    enabled: tab === 'metrics' && supportsInventory,
    staleTime: 60_000,
  })
  const alertsQuery = useQuery({
    queryKey: ['managed-instance-alerts', instance.id],
    queryFn: () => getManagedInstanceAlerts(instance.id),
    enabled: tab === 'alerts',
    staleTime: 30_000,
  })

  let currentFetching = inventoryQuery.isFetching
  if (tab === 'metrics') currentFetching = metricsQuery.isFetching
  if (tab === 'alerts') currentFetching = alertsQuery.isFetching

  const refreshCurrent = () => {
    if (tab === 'inventory') return inventoryQuery.refetch()
    if (tab === 'metrics') return metricsQuery.refetch()
    return alertsQuery.refetch()
  }

  let inventoryContent: React.ReactNode = (
    <InventoryContent
      observation={inventoryQuery.data?.data}
      hasPrevious={inventoryHistory.length > 0}
      onPrevious={() => {
        const previous = inventoryHistory.at(-1) ?? ''
        setInventoryHistory((history) => history.slice(0, -1))
        setInventoryCursor(previous)
      }}
      onNext={(cursor) => {
        setInventoryHistory((history) => [...history, inventoryCursor])
        setInventoryCursor(cursor)
      }}
    />
  )
  if (!supportsInventory) inventoryContent = <UnsupportedCapability />
  else if (inventoryQuery.isPending) inventoryContent = <ObservationLoading />
  else if (inventoryQuery.isError) {
    inventoryContent = (
      <CollectionUnavailable onRetry={() => void inventoryQuery.refetch()} />
    )
  }

  let metricsContent: React.ReactNode = (
    <MetricsContent observation={metricsQuery.data?.data} />
  )
  if (!supportsInventory) metricsContent = <UnsupportedCapability />
  else if (metricsQuery.isPending) metricsContent = <ObservationLoading />
  else if (metricsQuery.isError) {
    metricsContent = (
      <CollectionUnavailable onRetry={() => void metricsQuery.refetch()} />
    )
  }

  let alertsContent: React.ReactNode = (
    <AlertsContent alerts={alertsQuery.data?.data.items ?? []} />
  )
  if (alertsQuery.isPending) alertsContent = <ObservationLoading />
  else if (alertsQuery.isError) {
    alertsContent = (
      <CollectionUnavailable onRetry={() => void alertsQuery.refetch()} />
    )
  }

  return (
    <section className='min-w-0 rounded-lg border'>
      <header className='flex min-w-0 items-center justify-between gap-3 border-b px-4 py-3'>
        <div className='min-w-0'>
          <h2 className='text-sm font-semibold'>
            {t('Instance observations')}
          </h2>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {t('Normalized remote data with source and collection time.')}
          </p>
        </div>
        <Button
          variant='ghost'
          size='icon-sm'
          aria-label={t('Refresh observations')}
          disabled={currentFetching || (!supportsInventory && tab !== 'alerts')}
          onClick={() => void refreshCurrent()}
        >
          <RefreshCw className={cn(currentFetching && 'animate-spin')} />
        </Button>
      </header>

      <Tabs
        value={tab}
        onValueChange={(value) => setTab(value as ObservationTab)}
      >
        <div className='overflow-x-auto border-b px-4 pt-3'>
          <TabsList>
            <TabsTrigger value='inventory'>
              <Boxes />
              {t('Resources')}
            </TabsTrigger>
            <TabsTrigger value='metrics'>
              <Activity />
              {t('Metrics')}
            </TabsTrigger>
            <TabsTrigger value='alerts'>
              <Bell />
              {t('Alerts')}
            </TabsTrigger>
          </TabsList>
        </div>

        <TabsContent value='inventory' className='m-0 p-4'>
          {inventoryContent}
        </TabsContent>
        <TabsContent value='metrics' className='m-0 p-4'>
          {metricsContent}
        </TabsContent>
        <TabsContent value='alerts' className='m-0 p-4'>
          {alertsContent}
        </TabsContent>
      </Tabs>
    </section>
  )
}

function InventoryContent(props: {
  observation?: Awaited<ReturnType<typeof getManagedInstanceInventory>>['data']
  hasPrevious: boolean
  onPrevious: () => void
  onNext: (cursor: string) => void
}) {
  const { t } = useTranslation()
  const observation = props.observation
  if (!observation) return <CollectionUnavailable />
  if (observation.collection_status !== 'succeeded' || !observation.data) {
    return (
      <CollectionUnavailable
        status={observation.collection_status}
        errorCode={observation.error_code}
      />
    )
  }
  return (
    <div className='grid min-w-0 gap-3'>
      <ObservationMeta
        status={observation.collection_status}
        observedAt={observation.observed_at}
      />
      <div className='overflow-x-auto rounded-md border'>
        <table className='w-full min-w-[620px] text-left text-xs'>
          <thead className='bg-muted/50 text-muted-foreground'>
            <tr>
              <th className='px-3 py-2 font-medium'>{t('ID')}</th>
              <th className='px-3 py-2 font-medium'>{t('Name')}</th>
              <th className='px-3 py-2 font-medium'>{t('Type')}</th>
              <th className='px-3 py-2 font-medium'>{t('Group')}</th>
              <th className='px-3 py-2 font-medium'>{t('Status')}</th>
              <th className='px-3 py-2 font-medium'>{t('Enabled')}</th>
            </tr>
          </thead>
          <tbody className='divide-y'>
            {observation.data.items.map((item) => (
              <tr key={item.id}>
                <td className='px-3 py-2 font-mono'>{item.id}</td>
                <td className='px-3 py-2 font-medium'>{item.name || '-'}</td>
                <td className='px-3 py-2'>{item.type || '-'}</td>
                <td className='px-3 py-2'>{item.group || '-'}</td>
                <td className='px-3 py-2'>{item.status || '-'}</td>
                <td className='px-3 py-2'>
                  {item.enabled == null ? '-' : t(item.enabled ? 'Yes' : 'No')}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {observation.data.items.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t('No remote resources found.')}
        </p>
      )}
      <p className='text-muted-foreground text-xs'>
        {t('{{count}} normalized resources', { count: observation.data.total })}
      </p>
      {(props.hasPrevious || observation.data.next_cursor) && (
        <div className='flex items-center justify-end gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={!props.hasPrevious}
            onClick={props.onPrevious}
          >
            {t('Previous')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={!observation.data.next_cursor}
            onClick={() =>
              observation.data?.next_cursor &&
              props.onNext(observation.data.next_cursor)
            }
          >
            {t('Next')}
          </Button>
        </div>
      )}
    </div>
  )
}

function MetricsContent(props: {
  observation?: Awaited<ReturnType<typeof getManagedInstanceMetrics>>['data']
}) {
  const { t } = useTranslation()
  const observation = props.observation
  if (!observation) return <CollectionUnavailable />
  if (observation.collection_status !== 'succeeded' || !observation.data) {
    return (
      <CollectionUnavailable
        status={observation.collection_status}
        errorCode={observation.error_code}
      />
    )
  }
  const metrics = [
    ['Requests', observation.data.requests],
    ['Tokens', observation.data.tokens],
    ['Cost', observation.data.cost],
    ['Error rate', observation.data.error_rate],
    ['Latency', observation.data.latency],
  ] as const
  return (
    <div className='grid gap-4'>
      <ObservationMeta
        status={observation.collection_status}
        observedAt={observation.observed_at}
      />
      <div className='bg-border grid gap-px overflow-hidden rounded-md border sm:grid-cols-2 lg:grid-cols-5'>
        {metrics.map(([label, sample]) => (
          <MetricCell key={label} label={t(label)} sample={sample} />
        ))}
      </div>
      <div className='divide-y rounded-md border'>
        {observation.data.resources.map((resource) => (
          <div
            key={resource.resource_kind}
            className='grid gap-2 px-3 py-3 sm:grid-cols-4'
          >
            <span className='text-sm font-medium'>
              {resource.resource_kind}
            </span>
            <span className='text-sm'>
              {t('Total')}: {resource.total}
            </span>
            <span className='text-sm'>
              {t('Enabled')}: {resource.enabled ?? '-'}
            </span>
            <span className='text-sm'>
              {t('Unhealthy')}: {resource.unhealthy ?? '-'}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

function MetricCell({
  label,
  sample,
}: {
  label: string
  sample: ManagedInstanceMetricSample
}) {
  const { t } = useTranslation()
  return (
    <div className='bg-background min-w-0 px-3 py-3'>
      <div className='text-muted-foreground text-xs'>{label}</div>
      <div className='mt-1 text-base font-semibold'>
        {sample.value == null
          ? t('Unavailable')
          : `${sample.value} ${sample.unit}`}
      </div>
      <CollectionStatusBadge status={sample.collection_status} />
    </div>
  )
}

function AlertsContent({ alerts }: { alerts: ManagedInstanceAlert[] }) {
  const { t } = useTranslation()
  if (alerts.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('No instance alerts.')}
      </p>
    )
  }
  return (
    <div className='divide-y'>
      {alerts.map((alert) => (
        <div
          key={alert.id}
          className='grid min-w-0 gap-2 py-3 first:pt-0 last:pb-0 sm:grid-cols-[minmax(150px,0.35fr)_minmax(0,1fr)_auto]'
        >
          <div>
            <div className='flex items-center gap-2 text-sm font-medium'>
              <CircleAlert className='size-4' />
              {t(
                alert.alert_type === 'credential'
                  ? 'Credential alert'
                  : 'Availability alert'
              )}
            </div>
            <div className='text-muted-foreground mt-1 text-xs'>
              {formatTimestamp(alert.last_seen_at)}
            </div>
          </div>
          <div className='min-w-0 font-mono text-xs break-all'>
            {alert.error_code}
            <div className='text-muted-foreground mt-1 font-sans'>
              {t('{{count}} occurrences', { count: alert.occurrences })}
            </div>
          </div>
          <Badge variant={alert.status === 'open' ? 'destructive' : 'outline'}>
            {t(alert.status === 'open' ? 'Open' : 'Resolved')}
          </Badge>
        </div>
      ))}
    </div>
  )
}

function ObservationMeta({
  status,
  observedAt,
}: {
  status: ManagedInstanceCollectionStatus
  observedAt: number
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap items-center justify-between gap-2'>
      <CollectionStatusBadge status={status} />
      <span className='text-muted-foreground text-xs'>
        {t('Collected at {{time}}', { time: formatTimestamp(observedAt) })}
      </span>
    </div>
  )
}

function CollectionStatusBadge({
  status,
}: {
  status: ManagedInstanceCollectionStatus
}) {
  const { t } = useTranslation()
  let label = 'Unsupported'
  if (status === 'succeeded') label = 'Collected'
  if (status === 'failed') label = 'Failed'
  return (
    <Badge
      variant='outline'
      className={cn(
        status === 'succeeded' &&
          'border-emerald-600/20 bg-emerald-600/10 text-emerald-700 dark:text-emerald-400',
        status === 'failed' &&
          'border-red-600/20 bg-red-600/10 text-red-700 dark:text-red-400'
      )}
    >
      {t(label)}
    </Badge>
  )
}

function UnsupportedCapability() {
  const { t } = useTranslation()
  return (
    <p className='text-muted-foreground text-sm'>
      {t('The current remote version does not advertise this capability.')}
    </p>
  )
}

function CollectionUnavailable({
  status,
  errorCode,
  onRetry,
}: {
  status?: ManagedInstanceCollectionStatus
  errorCode?: string
  onRetry?: () => void
}) {
  const { t } = useTranslation()
  return (
    <div
      role='alert'
      className='flex flex-wrap items-start justify-between gap-3 text-sm'
    >
      <div className='flex items-start gap-2'>
        <CircleAlert className='text-destructive mt-0.5 size-4 shrink-0' />
        <div>
          <div className='font-medium'>{t('Collection unavailable')}</div>
          <div className='text-muted-foreground mt-1 font-mono text-xs'>
            {errorCode || status || 'collection_failed'}
          </div>
        </div>
      </div>
      {onRetry && (
        <Button variant='outline' size='sm' onClick={onRetry}>
          <RefreshCw />
          {t('Retry')}
        </Button>
      )}
    </div>
  )
}

function ObservationLoading() {
  return (
    <div className='grid gap-3'>
      <div className='flex items-center gap-2 text-sm'>
        <LoaderCircle className='size-4 animate-spin' />
        <Skeleton className='h-4 w-40' />
      </div>
      <Skeleton className='h-28 w-full' />
    </div>
  )
}
