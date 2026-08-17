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
import { useQueries, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import type { TFunction } from 'i18next'
import {
  Calculator,
  AlertTriangle,
  CheckCircle2,
  Clock3,
  CircleHelp,
  CircleDollarSign,
  DatabaseZap,
  Gauge,
  Network,
  RadioTower,
  RefreshCw,
  Search,
  Server,
  UserPlus,
  Users,
  XCircle,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  createFleetPresetRange,
  resolveFleetTimeRange,
  type FleetTimeRange,
} from '@/features/fleet-dashboard/time-range'
import { FleetTimeRangeFilter } from '@/features/fleet-dashboard/time-range-filter'
import {
  getManagedAccountSnapshot,
  getManagedInstances,
  refreshManagedAccountSnapshot,
} from '@/features/managed-instances/api'
import { InstanceConnectionAlert } from '@/features/managed-instances/components/instance-connection-alert'
import { isInstanceConnectionError } from '@/features/managed-instances/errors'
import type {
  ManagedInstance,
  ManagedAccountRangeInput,
  ManagedInstanceAccountOutputItem,
  ManagedInstanceInventoryItem,
  ManagedInstanceInventorySource,
  ManagedInstanceRealtimeState,
} from '@/features/managed-instances/types'
import { useManagedInstanceRealtimeEvents } from '@/features/managed-instances/use-realtime-events'
import { cn } from '@/lib/utils'

type AccountFamily = 'new_api' | 'sub2api' | 'conductor'
type ResourceRow = {
  instance: ManagedInstance
  item: ManagedInstanceInventoryItem
  source?: ManagedInstanceInventorySource
}
type SourceRow = {
  instance: ManagedInstance
  source: ManagedInstanceInventorySource
}
type OutputRow = {
  instance: ManagedInstance
  output: ManagedInstanceAccountOutputItem
}
type SortDirection = 'asc' | 'desc'
type AccountSortKey =
  | 'name'
  | 'instance'
  | 'created_at'
  | 'cost'
  | 'last_activity'
  | 'survival'
  | 'available'
type AccountPreferences = {
  family: AccountFamily
  selectedInstances: Record<AccountFamily, string>
}

const ACCOUNT_FAMILIES: readonly AccountFamily[] = [
  'new_api',
  'sub2api',
  'conductor',
]
const ALL_SITES_VALUE = 'all'
const INVENTORY_REFRESH_MS = 120_000
const FAILED_REFRESH_RETRY_MS = 60_000
const ACCOUNT_PREFERENCES_KEY = 'managed-account-preferences-v1'
const PANEL_CLASS = 'gap-0 rounded-lg py-0 shadow-xs'
const EMPTY_INSTANCES: ManagedInstance[] = []

const exactNumber = new Intl.NumberFormat(undefined, {
  maximumFractionDigits: 2,
})
const compactNumber = new Intl.NumberFormat(undefined, {
  notation: 'compact',
  maximumFractionDigits: 1,
})
const exactCurrency = new Intl.NumberFormat(undefined, {
  style: 'currency',
  currency: 'USD',
  maximumFractionDigits: 4,
})
const accountDateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'short',
})

function defaultPreferences(): AccountPreferences {
  return {
    family: 'new_api',
    selectedInstances: {
      new_api: ALL_SITES_VALUE,
      sub2api: ALL_SITES_VALUE,
      conductor: ALL_SITES_VALUE,
    },
  }
}

function readPreferences(): AccountPreferences {
  const fallback = defaultPreferences()
  if (typeof window === 'undefined') return fallback
  try {
    const parsed = JSON.parse(
      window.localStorage.getItem(ACCOUNT_PREFERENCES_KEY) ?? '{}'
    ) as Partial<AccountPreferences>
    const family = ACCOUNT_FAMILIES.includes(parsed.family as AccountFamily)
      ? (parsed.family as AccountFamily)
      : fallback.family
    return {
      family,
      selectedInstances: {
        ...fallback.selectedInstances,
        ...parsed.selectedInstances,
      },
    }
  } catch {
    return fallback
  }
}

function belongsToFamily(instance: ManagedInstance, family: AccountFamily) {
  if (family === 'sub2api') return instance.kind === 'sub2api'
  if (family === 'conductor') return instance.kind === 'conductor'
  return instance.kind === 'new_api' || instance.kind === 'huichuan'
}

function familyLabel(family: AccountFamily) {
  if (family === 'sub2api') return 'Sub2API'
  if (family === 'conductor') return 'Conductor'
  return 'New API'
}

function normalizeTimestampSeconds(value?: number) {
  if (!value || !Number.isFinite(value) || value <= 0) return null
  let normalized = value
  while (normalized > 100_000_000_000) normalized /= 1000
  return normalized
}

function formatTimestamp(value?: number) {
  const normalized = normalizeTimestampSeconds(value)
  if (normalized == null) return '--'
  const date = new Date(normalized * 1000)
  return Number.isNaN(date.getTime()) ? '--' : accountDateTime.format(date)
}

function getSurvivalSeconds(item: ManagedInstanceInventoryItem) {
  const createdAt = normalizeTimestampSeconds(item.created_at)
  if (createdAt == null) return null

  let end: number | undefined
  if (item.enabled === true) {
    end = Math.floor(Date.now() / 1000)
  } else if (item.enabled === false) {
    end = item.last_activity_at
  }
  const normalizedEnd = normalizeTimestampSeconds(end)
  if (normalizedEnd == null || normalizedEnd < createdAt) return null
  return normalizedEnd - createdAt
}

function compareSortValues(
  left: string | number | null | undefined,
  right: string | number | null | undefined,
  direction: SortDirection
) {
  const leftMissing = left == null || left === ''
  const rightMissing = right == null || right === ''
  if (leftMissing && rightMissing) return 0
  if (leftMissing) return 1
  if (rightMissing) return -1

  const comparison =
    typeof left === 'number' && typeof right === 'number'
      ? left - right
      : String(left).localeCompare(String(right), undefined, {
          numeric: true,
          sensitivity: 'base',
        })
  return direction === 'asc' ? comparison : -comparison
}

function availabilityRank(enabled?: boolean) {
  if (enabled == null) return 1
  return enabled ? 2 : 0
}

function isRateLimitedAccount(item: ManagedInstanceInventoryItem) {
  if (item.rate_limited) return true
  return [item.error_message, item.status].some((value) =>
    ['upstream_rate_limited', 'rate_limited', 'rate limited'].includes(
      value?.trim().toLowerCase() ?? ''
    )
  )
}

function compareResourceRows(
  left: ResourceRow,
  right: ResourceRow,
  sortKey: AccountSortKey,
  direction: SortDirection
) {
  let leftValue: string | number | null | undefined
  let rightValue: string | number | null | undefined
  switch (sortKey) {
    case 'name':
      leftValue = left.item.name
      rightValue = right.item.name
      break
    case 'instance':
      leftValue = left.instance.name
      rightValue = right.instance.name
      break
    case 'created_at':
      leftValue = left.item.created_at
      rightValue = right.item.created_at
      break
    case 'cost':
      leftValue = left.item.cost
      rightValue = right.item.cost
      break
    case 'last_activity':
      leftValue = left.item.last_activity_at
      rightValue = right.item.last_activity_at
      break
    case 'survival':
      leftValue = getSurvivalSeconds(left.item)
      rightValue = getSurvivalSeconds(right.item)
      break
    case 'available':
      leftValue = availabilityRank(left.item.enabled)
      rightValue = availabilityRank(right.item.enabled)
      break
  }
  const comparison = compareSortValues(leftValue, rightValue, direction)
  if (comparison !== 0) return comparison
  return (right.item.created_at ?? 0) - (left.item.created_at ?? 0)
}

function formatSurvivalDuration(seconds: number | null, t: TFunction) {
  if (seconds == null) return '--'

  const days = Math.floor(seconds / 86_400)
  const hours = Math.floor((seconds % 86_400) / 3_600)
  const minutes = Math.floor((seconds % 3_600) / 60)
  if (days > 0) return t('{{days}} days {{hours}} hours', { days, hours })
  if (hours > 0) {
    return t('{{hours}} hours {{minutes}} minutes', { hours, minutes })
  }
  if (minutes > 0) return t('{{minutes}} minutes', { minutes })
  return t('Less than 1 minute')
}

function formatCost(item: ManagedInstanceInventoryItem) {
  if (item.cost == null) return '--'
  if (item.cost_unit === 'usd') return exactCurrency.format(item.cost)
  return exactNumber.format(item.cost)
}

export function ManagedAccounts() {
  const { t } = useTranslation()
  const initialPreferences = useMemo(readPreferences, [])
  const [family, setFamily] = useState<AccountFamily>(initialPreferences.family)
  const [selectedInstances, setSelectedInstances] = useState(
    initialPreferences.selectedInstances
  )
  const [search, setSearch] = useState('')
  const [sortKey, setSortKey] = useState<AccountSortKey>('available')
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc')
  const [timeRange, setTimeRange] = useState<FleetTimeRange>(() =>
    createFleetPresetRange(7)
  )
  const automaticRefreshes = useRef(new Set<string>())
  const [submittingRefreshes, setSubmittingRefreshes] = useState<Set<number>>(
    () => new Set()
  )

  const instancesQuery = useQuery({
    queryKey: ['managed-account-instances'],
    queryFn: () => getManagedInstances({ search: '', kind: '', status: '' }),
    retry: 1,
    retryDelay: FAILED_REFRESH_RETRY_MS,
    refetchInterval: INVENTORY_REFRESH_MS,
  })
  const allInstances = instancesQuery.data?.data.items ?? EMPTY_INSTANCES
  const familyInstances = useMemo(
    () => allInstances.filter((instance) => belongsToFamily(instance, family)),
    [allInstances, family]
  )
  const selectedInstanceID = selectedInstances[family]
  const selectedInstance = familyInstances.find(
    (instance) => String(instance.id) === selectedInstanceID
  )
  const effectiveInstanceID = selectedInstance
    ? selectedInstanceID
    : ALL_SITES_VALUE
  const instances = useMemo(
    () => (selectedInstance ? [selectedInstance] : familyInstances),
    [familyInstances, selectedInstance]
  )
  const conductorInstanceIDs = useMemo(
    () =>
      instances
        .filter((instance) => instance.kind === 'conductor')
        .map((instance) => instance.id),
    [instances]
  )
  const conductorRealtime = useManagedInstanceRealtimeEvents(
    conductorInstanceIDs,
    ['rpm', 'accounts', 'sources', 'status']
  )
  const familyCounts = useMemo(
    () => ({
      new_api: allInstances.filter((item) => belongsToFamily(item, 'new_api'))
        .length,
      sub2api: allInstances.filter((item) => belongsToFamily(item, 'sub2api'))
        .length,
      conductor: allInstances.filter((item) =>
        belongsToFamily(item, 'conductor')
      ).length,
    }),
    [allInstances]
  )

  const accountRangeInput = useMemo<ManagedAccountRangeInput>(() => {
    if (timeRange.presetDays) {
      return {
        preset_days: timeRange.presetDays,
        timezone: 'Asia/Shanghai',
      }
    }
    const resolved = resolveFleetTimeRange(timeRange)
    return {
      start: Math.floor(resolved.start.getTime() / 1000),
      end: Math.floor(resolved.end.getTime() / 1000),
      timezone: 'Asia/Shanghai',
    }
  }, [timeRange])
  const rangeQueryKey = timeRange.presetDays
    ? `preset-${timeRange.presetDays}`
    : `${accountRangeInput.start}-${accountRangeInput.end}`

  const snapshotQueries = useQueries({
    queries: instances.map((instance) => ({
      queryKey: ['managed-account-snapshot', instance.id, rangeQueryKey],
      queryFn: () =>
        getManagedAccountSnapshot(instance.id, accountRangeInput, {
          silent: true,
        }),
      retry: 1,
      retryDelay: FAILED_REFRESH_RETRY_MS,
      staleTime: INVENTORY_REFRESH_MS / 2,
      refetchInterval: (query: { state: { data?: unknown } }) => {
        const response = query.state.data as
          | { data?: { task?: { status: string } } }
          | undefined
        const task = response?.data?.task
        return task && ['pending', 'running'].includes(task.status)
          ? 3_000
          : INVENTORY_REFRESH_MS
      },
    })),
  })
  const rows = useMemo<ResourceRow[]>(
    () =>
      instances.flatMap((instance, index) => {
        const snapshotPage =
          snapshotQueries[index]?.data?.data.inventory.observation?.data
        const realtime = conductorRealtime.states[instance.id]
        const useRealtime =
          instance.kind === 'conductor' &&
          realtime != null &&
          realtime.observed_at > 0 &&
          realtime.accounts != null
        const items = useRealtime
          ? (realtime.accounts ?? [])
          : snapshotPage?.items
        const sources = useRealtime
          ? (realtime.sources ?? snapshotPage?.sources ?? [])
          : (snapshotPage?.sources ?? [])
        if (!items) return []
        const sourceByID = new Map(sources.map((source) => [source.id, source]))
        return items.map((item) => ({
          instance,
          item,
          source: item.source_id ? sourceByID.get(item.source_id) : undefined,
        }))
      }),
    [conductorRealtime.states, instances, snapshotQueries]
  )
  const conductorSources = useMemo<SourceRow[]>(() => {
    const seen = new Set<string>()
    return instances.flatMap((instance, index) => {
      if (instance.kind !== 'conductor') return []
      const realtime = conductorRealtime.states[instance.id]
      const snapshotSources =
        snapshotQueries[index]?.data?.data.inventory.observation?.data
          ?.sources ?? []
      const sources = realtime?.sources ?? snapshotSources
      return sources.flatMap((source) => {
        const key = `${instance.id}:${source.id}`
        if (seen.has(key)) return []
        seen.add(key)
        return [{ instance, source }]
      })
    })
  }, [conductorRealtime.states, instances, snapshotQueries])
  const conductorStats = useMemo(() => {
    const validRPMRows = rows.filter(
      ({ item }) => item.rpm != null && item.rpm >= 0
    )
    return {
      nodes: conductorSources.length,
      connectedNodes: conductorSources.filter(({ source }) =>
        isConnectedSource(source.status)
      ).length,
      configuredAccounts: conductorSources.reduce(
        (sum, { source }) => sum + Math.max(0, source.account_count ?? 0),
        0
      ),
      rpm: validRPMRows.reduce((sum, { item }) => sum + (item.rpm ?? 0), 0),
      reportingAccounts: validRPMRows.length,
      activeSessions: rows.reduce(
        (sum, { item }) => sum + Math.max(0, item.active_sessions ?? 0),
        0
      ),
    }
  }, [conductorSources, rows])
  const outputRows = useMemo<OutputRow[]>(
    () =>
      instances.flatMap((instance, index) =>
        (
          snapshotQueries[index]?.data?.data.account_output.observation?.data
            ?.items ?? []
        ).map((output) => ({ instance, output }))
      ),
    [instances, snapshotQueries]
  )
  const outputTotals = useMemo(() => {
    const collected = outputRows.filter(
      ({ output }) => output.collection_status === 'succeeded'
    )
    const currencies = new Set(collected.map(({ output }) => output.currency))
    const currency = currencies.size === 1 ? [...currencies][0] : 'mixed'
    const amount =
      currency === 'mixed'
        ? null
        : collected.reduce((sum, { output }) => sum + output.amount, 0)
    return {
      added: outputRows.length,
      collected: collected.length,
      requests: collected.reduce(
        (sum, { output }) => sum + output.total_requests,
        0
      ),
      tokens: collected.reduce(
        (sum, { output }) => sum + output.total_tokens,
        0
      ),
      amount,
      average:
        amount == null ||
        collected.length !== outputRows.length ||
        outputRows.length === 0
          ? null
          : amount / outputRows.length,
      currency,
    }
  }, [outputRows])
  const normalizedSearch = search.trim().toLowerCase()
  const filteredRows = useMemo(() => {
    const filtered = normalizedSearch
      ? rows.filter(({ instance, item, source }) =>
          [
            item.id,
            item.name,
            item.platform,
            item.type,
            item.group,
            item.status,
            source?.name,
            source?.status,
            instance.name,
          ]
            .filter(Boolean)
            .join(' ')
            .toLowerCase()
            .includes(normalizedSearch)
        )
      : rows
    return [...filtered].sort((left, right) =>
      compareResourceRows(left, right, sortKey, sortDirection)
    )
  }, [normalizedSearch, rows, sortDirection, sortKey])
  const hasActiveTask = snapshotQueries.some((query) => {
    const task = query.data?.data.task
    return task && ['pending', 'running'].includes(task.status)
  })
  const refreshNeeded = snapshotQueries.some(
    (query) => query.data?.data.refresh_recommended
  )
  const loading = snapshotQueries.some(
    (query, index) =>
      query.isPending ||
      (!query.data?.data.inventory.observation &&
        (query.data?.data.refresh_recommended ||
          submittingRefreshes.has(instances[index]?.id) ||
          Boolean(query.data?.data.task)))
  )
  const error = snapshotQueries.some(
    (query) =>
      query.isError ||
      (query.data?.data.inventory.last_attempt_status === 'failed' &&
        !query.data.data.inventory.observation)
  )
  const connectionFailed = snapshotQueries.some(
    (query) =>
      isInstanceConnectionError(query.error) ||
      ['instance_connection_failed', 'remote_data_unavailable'].includes(
        query.data?.data.inventory.last_error_code ?? ''
      )
  )
  const collectedInstances = snapshotQueries.filter(
    (query) => query.data?.data.inventory.observation?.data
  ).length
  const available = rows.filter((row) => row.item.enabled === true).length
  const unavailable = rows.filter((row) => row.item.enabled === false).length
  const unknown = rows.length - available - unavailable
  const unavailableSurvival = rows.flatMap(({ item }) => {
    if (item.enabled !== false) return []
    const seconds = getSurvivalSeconds(item)
    return seconds == null ? [] : [seconds]
  })
  const averageUnavailableSurvival = unavailableSurvival.length
    ? Math.round(
        unavailableSurvival.reduce((sum, seconds) => sum + seconds, 0) /
          unavailableSurvival.length
      )
    : null
  const coverage = instances.length
    ? Math.round((collectedInstances / instances.length) * 100)
    : 0
  const isRefreshing =
    instancesQuery.isFetching ||
    snapshotQueries.some((query) => query.isFetching) ||
    hasActiveTask ||
    submittingRefreshes.size > 0
  const isCollecting =
    refreshNeeded || hasActiveTask || submittingRefreshes.size > 0
  const outputLoading = snapshotQueries.some(
    (query, index) =>
      !query.data?.data.account_output.observation &&
      (query.isPending ||
        query.data?.data.refresh_recommended ||
        submittingRefreshes.has(instances[index]?.id) ||
        Boolean(query.data?.data.task))
  )
  const outputError = snapshotQueries.some(
    (query) =>
      query.isError ||
      (query.data?.data.account_output.last_attempt_status === 'failed' &&
        !query.data.data.account_output.observation)
  )
  const cacheObservedAt = instances
    .map((instance, index) => {
      const realtime = conductorRealtime.states[instance.id]
      if (instance.kind === 'conductor' && realtime?.observed_at) {
        return realtime.observed_at
      }
      return (
        snapshotQueries[index]?.data?.data.inventory.observation?.observed_at ??
        0
      )
    })
    .filter((value) => value > 0)
    .sort((left, right) => left - right)[0]
  const cacheRefreshFailed = snapshotQueries.some(
    (query) =>
      query.data?.data.inventory.last_attempt_status === 'failed' ||
      query.data?.data.account_output.last_attempt_status === 'failed'
  )

  useEffect(() => {
    if (!instancesQuery.isSuccess || familyCounts[family] > 0) return
    const nextFamily = ACCOUNT_FAMILIES.find(
      (candidate) => familyCounts[candidate] > 0
    )
    if (nextFamily) setFamily(nextFamily)
  }, [family, familyCounts, instancesQuery.isSuccess])

  useEffect(() => {
    if (
      !instancesQuery.isSuccess ||
      selectedInstanceID === ALL_SITES_VALUE ||
      familyInstances.some(
        (instance) => String(instance.id) === selectedInstanceID
      )
    ) {
      return
    }
    setSelectedInstances((current) => ({
      ...current,
      [family]: ALL_SITES_VALUE,
    }))
  }, [family, familyInstances, instancesQuery.isSuccess, selectedInstanceID])

  useEffect(() => {
    if (typeof window === 'undefined') return
    window.localStorage.setItem(
      ACCOUNT_PREFERENCES_KEY,
      JSON.stringify({ family, selectedInstances })
    )
  }, [family, selectedInstances])

  useEffect(() => {
    instances.forEach((instance, index) => {
      const snapshot = snapshotQueries[index]?.data?.data
      const key = `${instance.id}:${rangeQueryKey}`
      if (
        !snapshot?.refresh_recommended ||
        snapshot.task ||
        automaticRefreshes.current.has(key)
      ) {
        return
      }
      automaticRefreshes.current.add(key)
      setSubmittingRefreshes((current) => new Set(current).add(instance.id))
      void refreshManagedAccountSnapshot(
        instance.id,
        { ...accountRangeInput, force: false },
        { silent: true }
      )
        .then(() => snapshotQueries[index]?.refetch())
        .finally(() => {
          setSubmittingRefreshes((current) => {
            const next = new Set(current)
            next.delete(instance.id)
            return next
          })
        })
    })
  }, [accountRangeInput, instances, rangeQueryKey, snapshotQueries])

  const refresh = () => {
    conductorRealtime.reconnect()
    void instancesQuery.refetch()
    instances.forEach((instance, index) => {
      setSubmittingRefreshes((current) => new Set(current).add(instance.id))
      void refreshManagedAccountSnapshot(
        instance.id,
        { ...accountRangeInput, force: true },
        { silent: true }
      )
        .then(() => snapshotQueries[index]?.refetch())
        .finally(() => {
          setSubmittingRefreshes((current) => {
            const next = new Set(current)
            next.delete(instance.id)
            return next
          })
        })
    })
  }

  let content: ReactNode
  if (instancesQuery.isLoading) {
    content = <AccountPageSkeleton />
  } else if (instancesQuery.isError) {
    content = <AccountPageError onRetry={refresh} />
  } else if (instances.length === 0) {
    content = <EmptyAccounts family={family} />
  } else {
    content = (
      <div className='grid gap-4 pb-6'>
        {connectionFailed && (
          <InstanceConnectionAlert onRetry={refresh} retrying={isRefreshing} />
        )}
        <AccountCacheStatus
          observedAt={cacheObservedAt}
          refreshing={isCollecting}
          failed={cacheRefreshFailed}
          hasData={collectedInstances > 0}
        />
        {family === 'conductor' && (
          <ConductorRealtimeStatus
            status={conductorRealtime.status}
            states={conductorRealtime.states}
            instanceCount={instances.length}
          />
        )}
        {family === 'conductor' && (
          <ConductorNodeSummary
            sources={conductorSources}
            stats={conductorStats}
          />
        )}
        <AccountSummary
          total={rows.length}
          available={available}
          unavailable={unavailable}
          unknown={unknown}
          coverage={coverage}
          showAverageSurvival={family !== 'new_api'}
          averageUnavailableSurvival={averageUnavailableSurvival}
          survivalSampleCount={unavailableSurvival.length}
        />
        <AccountOutputPanel
          family={family}
          rows={outputRows}
          totals={outputTotals}
          loading={outputLoading}
          error={outputError}
        />
        <AccountTable
          family={family}
          rows={filteredRows}
          total={rows.length}
          loading={loading}
          error={error}
          searching={normalizedSearch !== ''}
          sortKey={sortKey}
          sortDirection={sortDirection}
          onSortKeyChange={setSortKey}
          onSortDirectionChange={setSortDirection}
        />
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Account management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <FleetTimeRangeFilter value={timeRange} onChange={setTimeRange} />
        <Button
          variant='outline'
          size='icon-sm'
          aria-label={t('Refresh')}
          onClick={refresh}
        >
          <RefreshCw className={isRefreshing ? 'animate-spin' : ''} />
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='grid gap-4'>
          <div className='bg-card border-border/80 flex flex-wrap items-center gap-2 rounded-lg border p-2.5 shadow-xs sm:p-3'>
            <SegmentedControl
              value={family}
              options={ACCOUNT_FAMILIES}
              getLabel={(option) =>
                `${t(familyLabel(option))} · ${familyCounts[option]}`
              }
              onChange={(value) => {
                setFamily(value)
                if (value === 'new_api' && sortKey === 'survival') {
                  setSortKey('available')
                }
              }}
            />
            <Select
              items={[
                {
                  value: ALL_SITES_VALUE,
                  label: `${t('All sites')} · ${familyInstances.length}`,
                },
                ...familyInstances.map((instance) => ({
                  value: String(instance.id),
                  label: instance.name,
                })),
              ]}
              value={effectiveInstanceID}
              onValueChange={(value) => {
                if (!value) return
                setSelectedInstances((current) => ({
                  ...current,
                  [family]: value,
                }))
              }}
            >
              <SelectTrigger
                className='h-8 max-w-full min-w-44 sm:w-64'
                aria-label={t('Select site')}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value={ALL_SITES_VALUE}>
                    {t('All sites')} · {familyInstances.length}
                  </SelectItem>
                  {familyInstances.map((instance) => (
                    <SelectItem key={instance.id} value={String(instance.id)}>
                      <span className='flex min-w-0 items-center gap-2'>
                        <span
                          className={cn(
                            'size-1.5 shrink-0 rounded-full',
                            instance.status === 'healthy' && 'bg-success',
                            instance.status === 'degraded' && 'bg-warning',
                            ['offline', 'auth_failed'].includes(
                              instance.status
                            ) && 'bg-destructive',
                            instance.status === 'unknown' &&
                              'bg-muted-foreground/50'
                          )}
                        />
                        <span className='truncate'>{instance.name}</span>
                      </span>
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <div className='relative min-w-48 flex-1 sm:ms-auto sm:max-w-xs'>
              <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2' />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('Search accounts or channels')}
                aria-label={t('Search accounts or channels')}
                className='h-8 ps-8'
              />
            </div>
          </div>
          {content}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function AccountCacheStatus(props: {
  observedAt?: number
  refreshing: boolean
  failed: boolean
  hasData: boolean
}) {
  const { t } = useTranslation()
  let Icon = CheckCircle2
  let tone = 'border-success/25 bg-success/5 text-success'
  if (props.failed) {
    Icon = AlertTriangle
    tone = 'border-warning/35 bg-warning/5 text-warning'
  } else if (props.refreshing) {
    Icon = RefreshCw
    tone = 'border-primary/25 bg-primary/5 text-primary'
  }
  let label = t('Account snapshot is up to date')
  if (!props.hasData && props.refreshing) {
    label = t('Collecting account data for the first time')
  } else if (props.failed && props.hasData) {
    label = t('Refresh failed; showing the last successful account snapshot')
  } else if (props.failed) {
    label = t('Account data collection failed')
  } else if (props.refreshing) {
    label = t('Updating account data in the background')
  }
  return (
    <div
      className={cn(
        'flex min-h-10 flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm',
        tone
      )}
    >
      <span className='flex min-w-0 items-center gap-2 font-medium'>
        <Icon
          className={cn(
            'size-4 shrink-0',
            props.refreshing && !props.failed && 'animate-spin'
          )}
        />
        <span className='truncate'>{label}</span>
      </span>
      {props.observedAt ? (
        <Badge variant='outline' className='bg-background/70 tabular-nums'>
          {t('Collected at {{time}}', {
            time: formatTimestamp(props.observedAt),
          })}
        </Badge>
      ) : null}
    </div>
  )
}

function ConductorRealtimeStatus(props: {
  status: string
  states: Record<number, ManagedInstanceRealtimeState>
  instanceCount: number
}) {
  const states = Object.values(props.states)
  const connected =
    props.instanceCount > 0 &&
    states.length === props.instanceCount &&
    states.every((state) => state.stream_status === 'connected' && !state.stale)
  const observedAt = states.reduce(
    (latest, state) => Math.max(latest, state.observed_at ?? 0),
    0
  )
  const reconnecting = ['connecting', 'reconnecting', 'error'].includes(
    props.status
  )
  let statusLabel = 'Conductor 实时账号数据连接中'
  if (connected) {
    statusLabel = 'Conductor 实时账号流已连接'
  } else if (states.length) {
    statusLabel = '实时流正在重连，当前展示最后一次实时数据'
  }
  return (
    <div
      className={cn(
        'flex min-h-10 flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm',
        connected
          ? 'border-success/25 bg-success/5 text-success'
          : 'border-warning/35 bg-warning/5 text-warning'
      )}
    >
      <span className='flex min-w-0 items-center gap-2 font-medium'>
        <RadioTower
          className={cn('size-4 shrink-0', reconnecting && 'animate-pulse')}
        />
        <span>{statusLabel}</span>
      </span>
      {observedAt > 0 && (
        <Badge variant='outline' className='bg-background/70 tabular-nums'>
          更新于 {formatTimestamp(observedAt)}
        </Badge>
      )}
    </div>
  )
}

function ConductorNodeSummary(props: {
  sources: SourceRow[]
  stats: {
    nodes: number
    connectedNodes: number
    configuredAccounts: number
    rpm: number
    reportingAccounts: number
    activeSessions: number
  }
}) {
  const items = [
    {
      label: '工作节点',
      value: exactNumber.format(props.stats.nodes),
      hint: `${props.stats.connectedNodes} 个已连接`,
      icon: Network,
      tone: 'bg-primary/10 text-primary',
    },
    {
      label: '节点配置账号',
      value: exactNumber.format(props.stats.configuredAccounts),
      hint: `来自 ${props.sources.length} 个节点配置`,
      icon: Server,
      tone: 'bg-success/10 text-success',
    },
    {
      label: '实时总 RPM',
      value: exactNumber.format(props.stats.rpm),
      hint: `${props.stats.reportingAccounts} 个账号参与统计`,
      icon: Gauge,
      tone: 'bg-warning/10 text-warning',
    },
    {
      label: '活跃会话',
      value: exactNumber.format(props.stats.activeSessions),
      hint: '所有实时账号汇总',
      icon: DatabaseZap,
      tone: 'bg-violet-500/10 text-violet-600 dark:text-violet-400',
    },
  ]
  return (
    <div className='border-border/80 bg-card grid overflow-hidden rounded-lg border shadow-xs sm:grid-cols-2 xl:grid-cols-4'>
      {items.map((item) => (
        <div
          key={item.label}
          className='border-border/70 flex min-h-28 items-start justify-between gap-3 border-b p-4 sm:odd:border-r xl:border-b-0 xl:not-last:border-r'
        >
          <div className='min-w-0'>
            <p className='text-muted-foreground text-sm'>{item.label}</p>
            <p className='mt-2 text-2xl font-semibold tabular-nums'>
              {item.value}
            </p>
            <p className='text-muted-foreground mt-1 truncate text-xs'>
              {item.hint}
            </p>
          </div>
          <span
            className={cn(
              'flex size-9 shrink-0 items-center justify-center rounded-md',
              item.tone
            )}
          >
            <item.icon className='size-4' />
          </span>
        </div>
      ))}
    </div>
  )
}

function AccountSummary(props: {
  total: number
  available: number
  unavailable: number
  unknown: number
  coverage: number
  showAverageSurvival: boolean
  averageUnavailableSurvival: number | null
  survivalSampleCount: number
}) {
  const { t } = useTranslation()
  const items = [
    {
      key: 'total',
      label: t('Total resources'),
      value: props.total,
      hint: t('{{count}} unknown status', { count: props.unknown }),
      icon: Users,
      className: 'bg-primary/10 text-primary',
    },
    {
      key: 'available',
      label: t('Available resources'),
      value: props.available,
      hint: t('Ready for scheduling'),
      icon: CheckCircle2,
      className: 'bg-success/10 text-success',
    },
    {
      key: 'unavailable',
      label: t('Unavailable resources'),
      value: props.unavailable,
      hint: t('Disabled or unschedulable'),
      icon: XCircle,
      className: 'bg-destructive/10 text-destructive',
    },
    {
      key: 'coverage',
      label: t('Collection coverage'),
      value: `${props.coverage}%`,
      hint: t('Selected site coverage'),
      icon: CircleHelp,
      className: 'bg-warning/10 text-warning',
    },
  ]
  if (props.showAverageSurvival) {
    items.splice(3, 0, {
      key: 'average-unavailable-survival',
      label: t('Average unavailable survival time'),
      value: formatSurvivalDuration(props.averageUnavailableSurvival, t),
      hint: t('{{count}} accounts included', {
        count: props.survivalSampleCount,
      }),
      icon: Clock3,
      className: 'bg-orange-500/10 text-orange-600 dark:text-orange-400',
    })
  }
  return (
    <section
      className={cn(
        'grid grid-cols-2 gap-3',
        props.showAverageSurvival ? 'xl:grid-cols-5' : 'xl:grid-cols-4'
      )}
    >
      {items.map((item) => (
        <Card key={item.key} className='gap-0 rounded-lg py-0 shadow-xs'>
          <CardContent className='flex min-h-28 items-start justify-between gap-3 p-4'>
            <div className='min-w-0'>
              <p className='text-muted-foreground truncate text-sm'>
                {item.label}
              </p>
              <p className='mt-2 text-2xl font-semibold tabular-nums'>
                {item.value}
              </p>
              <p className='text-muted-foreground mt-1 truncate text-xs'>
                {item.hint}
              </p>
            </div>
            <span
              className={cn(
                'flex size-8 shrink-0 items-center justify-center rounded-md',
                item.className
              )}
            >
              <item.icon className='size-4' />
            </span>
          </CardContent>
        </Card>
      ))}
    </section>
  )
}

function formatOutputAmount(value: number | null, currency: string) {
  if (value == null || currency === 'mixed') return '--'
  if (currency.toUpperCase() === 'USD') return exactCurrency.format(value)
  return `${exactNumber.format(value)} ${currency || ''}`.trim()
}

function AccountOutputPanel(props: {
  family: AccountFamily
  rows: OutputRow[]
  totals: {
    added: number
    collected: number
    requests: number
    tokens: number
    amount: number | null
    average: number | null
    currency: string
  }
  loading: boolean
  error: boolean
}) {
  const { t } = useTranslation()
  const summary = [
    {
      key: 'added',
      label: t('Accounts added in selected period'),
      value: props.totals.added,
      icon: UserPlus,
      tone: 'bg-sky-500/10 text-sky-600 dark:text-sky-400',
    },
    {
      key: 'requests',
      label: t('Output requests'),
      value: formatOptionalNumber(props.totals.requests),
      icon: Users,
      tone: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
    },
    {
      key: 'tokens',
      label: t('Output tokens'),
      value: formatOptionalNumber(props.totals.tokens),
      icon: DatabaseZap,
      tone: 'bg-violet-500/10 text-violet-600 dark:text-violet-400',
    },
    {
      key: 'amount',
      label: t('Total output amount'),
      value: formatOutputAmount(props.totals.amount, props.totals.currency),
      icon: CircleDollarSign,
      tone: 'bg-orange-500/10 text-orange-600 dark:text-orange-400',
    },
    {
      key: 'average',
      label: t('Average output per account'),
      value: formatOutputAmount(props.totals.average, props.totals.currency),
      icon: Calculator,
      tone: 'bg-amber-500/10 text-amber-600 dark:text-amber-400',
    },
  ]
  let detailContent: ReactNode
  if (props.loading && props.rows.length === 0) {
    detailContent = <TableSkeleton wide={false} />
  } else if (props.rows.length === 0) {
    detailContent = (
      <PanelEmpty
        text={
          props.error
            ? t('Account output data could not be loaded')
            : t('No accounts were added in the selected period')
        }
      />
    )
  } else {
    detailContent = (
      <div className='overflow-x-auto'>
        <AccountOutputTable family={props.family} rows={props.rows} />
      </div>
    )
  }

  return (
    <Card className={PANEL_CLASS}>
      <CardHeader className='border-border/70 border-b py-3.5'>
        <CardTitle className='flex items-center gap-2'>
          <CircleDollarSign className='text-muted-foreground size-4' />
          {t('New account output')}
        </CardTitle>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Output generated in the selected period by accounts added in that period'
          )}
        </p>
      </CardHeader>
      <CardContent className='p-0'>
        <div className='bg-border grid grid-cols-2 gap-px border-b lg:grid-cols-3 xl:grid-cols-5'>
          {summary.map((item) => (
            <div key={item.key} className='bg-card min-h-24 p-4'>
              <div className='flex items-start justify-between gap-2'>
                <p className='text-muted-foreground text-xs font-medium'>
                  {item.label}
                </p>
                <span
                  className={cn(
                    'flex size-7 shrink-0 items-center justify-center rounded-md',
                    item.tone
                  )}
                >
                  <item.icon className='size-3.5' />
                </span>
              </div>
              <p className='mt-2 font-mono text-xl font-semibold tabular-nums'>
                {item.value}
              </p>
            </div>
          ))}
        </div>
        {detailContent}
      </CardContent>
    </Card>
  )
}

function AccountOutputTable({
  family,
  rows,
}: {
  family: AccountFamily
  rows: OutputRow[]
}) {
  const { t } = useTranslation()
  const isChannel = family === 'new_api'
  return (
    <Table className='min-w-[860px]'>
      <TableHeader className='bg-muted/35'>
        <TableRow>
          <TableHead className='ps-6'>
            {t(isChannel ? 'Channel' : 'Account')}
          </TableHead>
          <TableHead>{t('Instance')}</TableHead>
          <TableHead>{t(isChannel ? 'Created At' : 'Uploaded at')}</TableHead>
          <TableHead className='text-right'>{t('Requests')}</TableHead>
          <TableHead className='text-right'>{t('Tokens')}</TableHead>
          <TableHead className='pe-6 text-right'>
            {t('Output amount')}
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map(({ instance, output }) => (
          <TableRow key={`${instance.id}:${output.account.id}`}>
            <TableCell className='ps-6'>
              <p className='max-w-52 truncate font-medium'>
                {output.account.name || `#${output.account.id}`}
              </p>
              <p className='text-muted-foreground text-xs tabular-nums'>
                #{output.account.id}
              </p>
            </TableCell>
            <TableCell>{instance.name}</TableCell>
            <TableCell className='whitespace-nowrap'>
              {formatTimestamp(output.account.created_at)}
            </TableCell>
            <TableCell className='text-right tabular-nums'>
              {output.collection_status === 'succeeded'
                ? formatOptionalNumber(output.total_requests)
                : '--'}
            </TableCell>
            <TableCell className='text-right tabular-nums'>
              {output.collection_status === 'succeeded'
                ? formatOptionalNumber(output.total_tokens)
                : '--'}
            </TableCell>
            <TableCell className='pe-6 text-right font-medium tabular-nums'>
              {output.collection_status === 'succeeded'
                ? formatOutputAmount(output.amount, output.currency)
                : t('Collection failed')}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function AccountTable(props: {
  family: AccountFamily
  rows: ResourceRow[]
  total: number
  loading: boolean
  error: boolean
  searching: boolean
  sortKey: AccountSortKey
  sortDirection: SortDirection
  onSortKeyChange: (sortKey: AccountSortKey) => void
  onSortDirectionChange: (direction: SortDirection) => void
}) {
  const { t } = useTranslation()
  const [selectedSource, setSelectedSource] = useState<SourceRow | null>(null)
  const isChannel = props.family === 'new_api'
  const isConductor = props.family === 'conductor'
  const showsSurvival = !isChannel
  const sortOptions: { value: AccountSortKey; label: string }[] = [
    { value: 'available', label: t('Available') },
    { value: 'name', label: t(isChannel ? 'Channel' : 'Account') },
    { value: 'instance', label: t('Instance') },
    {
      value: 'created_at',
      label: t(isChannel ? 'Created At' : 'Uploaded at'),
    },
    {
      value: 'cost',
      label: t(isChannel ? 'Used quota' : 'Total consumption'),
    },
    { value: 'last_activity', label: t('Last activity') },
  ]
  if (showsSurvival) {
    sortOptions.push({ value: 'survival', label: t('Survival time') })
  }
  const directionOptions: { value: SortDirection; label: string }[] = [
    { value: 'desc', label: t('Desc') },
    { value: 'asc', label: t('Asc') },
  ]
  let emptyText = t(isChannel ? 'No channel data' : 'No account data')
  if (props.searching) emptyText = t('No matching accounts or channels')
  if (props.error) emptyText = t('Account data could not be loaded')
  let tableMinWidth = 'min-w-[1140px]'
  if (isChannel) tableMinWidth = 'min-w-[980px]'
  else if (isConductor) tableMinWidth = 'min-w-[1280px]'
  let content: ReactNode
  if (props.loading && props.total === 0) {
    content = <TableSkeleton wide={!isChannel} />
  } else if (props.rows.length === 0) {
    content = <PanelEmpty text={emptyText} />
  } else {
    content = (
      <Table className={tableMinWidth}>
        <TableHeader className='bg-muted/35'>
          <TableRow>
            <TableHead className='ps-6'>
              {t(isChannel ? 'Channel' : 'Account')}
            </TableHead>
            <TableHead>{t('Instance')}</TableHead>
            <TableHead>
              {isConductor ? '工作节点' : `${t('Platform')} / ${t('Type')}`}
            </TableHead>
            <TableHead>{t(isChannel ? 'Created At' : 'Uploaded at')}</TableHead>
            <TableHead className='text-right'>
              {isConductor
                ? '运行负载'
                : t(isChannel ? 'Used quota' : 'Total consumption')}
            </TableHead>
            <TableHead>{t('Last activity')}</TableHead>
            {showsSurvival && <TableHead>{t('Survival time')}</TableHead>}
            <TableHead className='pe-6 text-right'>{t('Available')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody className='[&>tr]:h-16'>
          {props.rows.map(({ instance, item, source }) => {
            const descriptors = [item.platform, item.type, item.group].filter(
              (value, index, values): value is string =>
                Boolean(value) && values.indexOf(value) === index
            )
            const survivalSeconds = showsSurvival
              ? getSurvivalSeconds(item)
              : null
            const rateLimited = isRateLimitedAccount(item)
            return (
              <TableRow key={`${instance.id}:${item.id}`}>
                <TableCell className='ps-6'>
                  <div className='max-w-52 min-w-36'>
                    <p className='truncate font-medium'>
                      {item.name || `#${item.id}`}
                    </p>
                    <p className='text-muted-foreground text-xs tabular-nums'>
                      #{item.id}
                    </p>
                  </div>
                </TableCell>
                <TableCell>
                  <Link
                    to='/instances/$id'
                    params={{ id: String(instance.id) }}
                    className='block max-w-40 truncate text-sm hover:underline'
                  >
                    {instance.name}
                  </Link>
                </TableCell>
                <TableCell>
                  {isConductor ? (
                    <SourceCell
                      source={source}
                      sourceID={item.source_id}
                      onOpen={(nextSource) =>
                        setSelectedSource({ instance, source: nextSource })
                      }
                    />
                  ) : (
                    <span className='block max-w-44 truncate text-sm'>
                      {descriptors.join(' / ') || '--'}
                    </span>
                  )}
                </TableCell>
                <TableCell className='text-muted-foreground whitespace-nowrap'>
                  {formatTimestamp(item.created_at)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {isConductor ? (
                    <>
                      <p className='font-medium'>
                        {formatOptionalNumber(item.rpm)} RPM
                      </p>
                      <p className='text-muted-foreground text-xs'>
                        {formatOptionalNumber(item.active_sessions)} 个会话 ·{' '}
                        {item.utilization_5h == null
                          ? '--'
                          : `${(item.utilization_5h * 100).toFixed(1)}%`}{' '}
                        / 5h
                      </p>
                    </>
                  ) : (
                    <p className='font-medium'>{formatCost(item)}</p>
                  )}
                  {!isConductor &&
                    (isChannel
                      ? item.balance != null && (
                          <p className='text-muted-foreground text-xs'>
                            {t('Balance')} {exactCurrency.format(item.balance)}
                          </p>
                        )
                      : (item.requests != null || item.tokens != null) && (
                          <p className='text-muted-foreground text-xs'>
                            {formatOptionalNumber(item.requests)}{' '}
                            {t('Requests')} /{' '}
                            {formatOptionalNumber(item.tokens)} {t('Tokens')}
                          </p>
                        ))}
                </TableCell>
                <TableCell className='whitespace-nowrap'>
                  <p className='text-sm'>
                    {formatTimestamp(item.last_activity_at)}
                  </p>
                  {item.response_time_ms != null && (
                    <p className='text-muted-foreground text-xs tabular-nums'>
                      {item.response_time_ms} ms
                    </p>
                  )}
                </TableCell>
                {showsSurvival && (
                  <TableCell className='whitespace-nowrap'>
                    <p className='text-sm font-medium tabular-nums'>
                      {formatSurvivalDuration(survivalSeconds, t)}
                    </p>
                    {survivalSeconds != null && (
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          item.enabled === false
                            ? 'Until last call'
                            : 'Still active'
                        )}
                      </p>
                    )}
                  </TableCell>
                )}
                <TableCell className='pe-6 text-right'>
                  <AvailabilityBadge
                    enabled={item.enabled}
                    rateLimited={rateLimited}
                  />
                  {item.error_message && (
                    <p
                      className={cn(
                        'ms-auto mt-1 max-w-40 truncate text-xs',
                        rateLimited ? 'text-warning' : 'text-destructive'
                      )}
                      title={item.error_message}
                    >
                      {t(item.error_message)}
                    </p>
                  )}
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    )
  }

  return (
    <Card className={PANEL_CLASS}>
      <CardHeader className='border-border/70 flex-row flex-wrap items-start justify-between gap-3 space-y-0 border-b py-3.5'>
        <div className='min-w-0'>
          <CardTitle className='flex items-center gap-2'>
            <Users className='text-muted-foreground size-4' />
            {t(isChannel ? 'Channel details' : 'Account details')}
          </CardTitle>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              isChannel
                ? 'Remote channel availability and consumption'
                : 'Remote account availability and consumption'
            )}
          </p>
        </div>
        <div className='flex max-w-full flex-wrap items-center justify-end gap-1.5'>
          <Select
            items={sortOptions}
            value={props.sortKey}
            onValueChange={(value) =>
              value && props.onSortKeyChange(value as AccountSortKey)
            }
          >
            <SelectTrigger className='h-7 w-36 text-xs' aria-label={t('Sort')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent align='end'>
              {sortOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            items={directionOptions}
            value={props.sortDirection}
            onValueChange={(value) =>
              value && props.onSortDirectionChange(value as SortDirection)
            }
          >
            <SelectTrigger className='h-7 w-20 text-xs' aria-label={t('Sort')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent align='end'>
              {directionOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Badge variant='secondary' className='tabular-nums'>
            {props.rows.length === props.total
              ? props.total
              : `${props.rows.length} / ${props.total}`}
          </Badge>
        </div>
      </CardHeader>
      {props.error && props.rows.length > 0 && (
        <div className='border-border bg-destructive/5 text-destructive border-b px-6 py-2 text-xs'>
          {t('Some account data could not be loaded')}
        </div>
      )}
      <CardContent className='overflow-x-auto px-0'>{content}</CardContent>
      <SourceDetailsDialog
        selected={selectedSource}
        onOpenChange={(open) => !open && setSelectedSource(null)}
      />
    </Card>
  )
}

function SourceCell(props: {
  source?: ManagedInstanceInventorySource
  sourceID?: string
  onOpen: (source: ManagedInstanceInventorySource) => void
}) {
  if (!props.source && !props.sourceID) {
    return <span className='text-muted-foreground text-sm'>--</span>
  }
  const source =
    props.source ??
    ({
      id: props.sourceID ?? 'unknown',
      name: `未知节点 #${props.sourceID ?? 'unknown'}`,
      status: 'unknown',
    } satisfies ManagedInstanceInventorySource)
  const connected = isConnectedSource(source.status)
  let statusTone = 'bg-muted-foreground/50'
  if (connected) statusTone = 'bg-success'
  else if (source.status) statusTone = 'bg-warning'
  return (
    <button
      type='button'
      className='hover:bg-muted/70 focus-visible:ring-ring flex max-w-48 items-center gap-2 rounded-md px-2 py-1 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none'
      onClick={() => props.onOpen(source)}
    >
      <span className={cn('size-2 shrink-0 rounded-full', statusTone)} />
      <span className='min-w-0'>
        <span className='block truncate text-sm font-medium'>
          {source.name}
        </span>
        <span className='text-muted-foreground block truncate text-xs'>
          {connected ? '已连接' : source.status || '状态未知'}
        </span>
      </span>
    </button>
  )
}

function SourceDetailsDialog(props: {
  selected: SourceRow | null
  onOpenChange: (open: boolean) => void
}) {
  const source = props.selected?.source
  let enabledLabel = '未知'
  if (source?.enabled === true) enabledLabel = '已启用'
  else if (source?.enabled === false) enabledLabel = '已停用'
  return (
    <Dialog open={Boolean(source)} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <Network className='text-muted-foreground size-4' />
            {source?.name ?? '工作节点'}
          </DialogTitle>
          <DialogDescription>
            {props.selected?.instance.name} 的 Conductor 工作节点，只读信息
          </DialogDescription>
        </DialogHeader>
        {source && (
          <div className='grid gap-3 rounded-lg border p-4'>
            <SourceDetail label='节点 ID' value={source.id} mono />
            <SourceDetail
              label='连接状态'
              value={
                isConnectedSource(source.status)
                  ? '已连接'
                  : source.status || '未知'
              }
            />
            <SourceDetail label='启用状态' value={enabledLabel} />
            <SourceDetail
              label='配置账号数'
              value={exactNumber.format(source.account_count ?? 0)}
            />
            <SourceDetail
              label='内部 WS 地址'
              value={source.url || '--'}
              mono
            />
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

function SourceDetail(props: { label: string; value: string; mono?: boolean }) {
  return (
    <div className='grid gap-1 sm:grid-cols-[7rem_minmax(0,1fr)] sm:items-start'>
      <span className='text-muted-foreground text-sm'>{props.label}</span>
      <span
        className={cn(
          'break-all text-sm',
          props.mono && 'font-mono text-xs leading-5'
        )}
      >
        {props.value}
      </span>
    </div>
  )
}

function isConnectedSource(status?: string) {
  const normalized = status?.trim().toLowerCase()
  return ['connected', 'healthy', 'online', 'ready'].includes(normalized ?? '')
}

function formatOptionalNumber(value?: number) {
  return value == null ? '--' : compactNumber.format(value)
}

function AvailabilityBadge({
  enabled,
  rateLimited,
}: {
  enabled?: boolean
  rateLimited?: boolean
}) {
  const { t } = useTranslation()
  if (rateLimited) {
    return (
      <Badge
        variant='outline'
        className='border-warning/25 bg-warning/10 text-warning'
      >
        <AlertTriangle />
        {t('Rate limited')}
      </Badge>
    )
  }
  if (enabled == null) return <Badge variant='secondary'>{t('Unknown')}</Badge>
  if (!enabled) return <Badge variant='destructive'>{t('Unavailable')}</Badge>
  return (
    <Badge
      variant='outline'
      className='border-success/20 bg-success/10 text-success'
    >
      <CheckCircle2 />
      {t('Available')}
    </Badge>
  )
}

function SegmentedControl<T extends string>(props: {
  value: T
  options: readonly T[]
  onChange: (value: T) => void
  getLabel?: (value: T) => string
}) {
  return (
    <div className='bg-muted flex h-8 max-w-full items-center gap-0.5 overflow-x-auto rounded-md p-0.5'>
      {props.options.map((option) => (
        <button
          key={option}
          type='button'
          aria-pressed={props.value === option}
          className={cn(
            'focus-visible:ring-ring h-7 min-w-max rounded-sm px-2 text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none',
            props.value === option
              ? 'bg-background text-foreground shadow-xs'
              : 'text-muted-foreground hover:text-foreground'
          )}
          onClick={() => props.onChange(option)}
        >
          {props.getLabel?.(option) ?? option}
        </button>
      ))}
    </div>
  )
}

function TableSkeleton(props: { wide: boolean }) {
  return (
    <div
      className={cn(
        'grid gap-px',
        props.wide ? 'min-w-[1140px]' : 'min-w-[980px]'
      )}
    >
      {['first', 'second', 'third', 'fourth'].map((key) => (
        <div key={key} className='flex h-16 items-center gap-8 px-6'>
          <Skeleton className='h-4 w-36' />
          <Skeleton className='h-4 w-28' />
          <Skeleton className='h-4 w-24' />
          <Skeleton className='h-4 w-32' />
          <Skeleton className='ms-auto h-5 w-16' />
        </div>
      ))}
    </div>
  )
}

function AccountPageSkeleton() {
  return (
    <div className='grid gap-4'>
      <div className='grid grid-cols-2 gap-3 xl:grid-cols-4'>
        {['total', 'available', 'unavailable', 'coverage'].map((key) => (
          <Skeleton key={key} className='h-28 rounded-lg' />
        ))}
      </div>
      <Skeleton className='h-[420px] rounded-lg' />
    </div>
  )
}

function AccountPageError({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className='flex min-h-[50vh] flex-col items-center justify-center text-center'>
      <XCircle className='text-destructive mb-3 size-9' />
      <h2 className='text-lg font-semibold'>
        {t('Account data could not be loaded')}
      </h2>
      <Button variant='outline' className='mt-4' onClick={onRetry}>
        <RefreshCw />
        {t('Retry')}
      </Button>
    </div>
  )
}

function EmptyAccounts({ family }: { family: AccountFamily }) {
  const { t } = useTranslation()
  return (
    <div className='flex min-h-[50vh] flex-col items-center justify-center px-4 text-center'>
      <span className='bg-muted mb-4 flex size-12 items-center justify-center rounded-lg'>
        <Server className='text-muted-foreground size-6' />
      </span>
      <h2 className='text-lg font-semibold'>{t('No instances yet')}</h2>
      <p className='text-muted-foreground mt-1 max-w-sm text-sm'>
        {t('Add a {{kind}} instance to start collecting fleet data.', {
          kind: t(familyLabel(family)),
        })}
      </p>
      <Button className='mt-4' render={<Link to='/instances' />}>
        {t('Go to instance center')}
      </Button>
    </div>
  )
}

function PanelEmpty({ text }: { text: string }) {
  return (
    <div className='text-muted-foreground flex min-h-[280px] items-center justify-center px-4 text-center text-sm'>
      {text}
    </div>
  )
}
