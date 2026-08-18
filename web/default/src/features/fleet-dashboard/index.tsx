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
import { keepPreviousData, useQueries, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Activity,
  Building2,
  CheckCircle2,
  CircleDollarSign,
  Cpu,
  DatabaseZap,
  Gauge,
  Radio,
  RefreshCw,
  Server,
  ServerOff,
  UserCheck,
  Users,
} from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  XAxis,
  YAxis,
} from 'recharts'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@/components/ui/chart'
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
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import {
  getManagedDashboardSnapshots,
  getManagedInstanceRPMHistory,
  getManagedInstanceRealtimeMetrics,
  getManagedInstances,
  refreshManagedDashboard,
} from '../managed-instances/api'
import { InstanceConnectionAlert } from '../managed-instances/components/instance-connection-alert'
import { StatusBadge } from '../managed-instances/components/status-badge'
import { isInstanceConnectionError } from '../managed-instances/errors'
import type {
  ManagedDashboardSnapshotSection,
  ManagedInstance,
  ManagedInstanceSummary,
  ManagedInstanceRPMHistoryBucket,
} from '../managed-instances/types'
import { useManagedDashboardEvents } from '../managed-instances/use-dashboard-events'
import { useManagedInstanceRealtimeEvents } from '../managed-instances/use-realtime-events'
import {
  createFleetPresetRange,
  FLEET_TIME_PRESETS,
  resolveFleetTimeRange,
  type FleetPresetDays,
  type FleetTimeRange,
} from './time-range'
import { FleetTimeRangeFilter } from './time-range-filter'

type MetricKey = 'requests' | 'tokens' | 'quota'
type TrendMetricKey = MetricKey | 'rpm'
type FleetFamily = 'new_api' | 'sub2api' | 'conductor'

type InstanceMetricRow = {
  instance: ManagedInstance
  summary?: ManagedInstanceSummary
  observedAt: number
  summaryObservedAt: number
  todayObservedAt: number
  collected: boolean
  requests: number | null
  rpm: number | null
  rpmCapacity: number | null
  concurrencyUsed: number | null
  concurrencyMax: number | null
  accountsAvailable: number | null
  accountsTotal: number | null
  todayCost: number | null
  tokens: number | null
  quota: number | null
  lastAttemptAt: number
  lastAttemptStatus: string
  lastErrorCode: string
  stale: boolean
}

type DailyUsageData = {
  date: string
  value: number
}

type RPMHistoryData = {
  timestamp: number
  rpm: number
  capacity: number | null
  samples: number
}

type HealthData = {
  key: string
  label: string
  color: string
  value: number
}

const RPM_REFRESH_MS = 10_000
const RPM_HISTORY_REFRESH_MS = 10_000
const DASHBOARD_ERROR_RETRY_MS = 15_000
const DASHBOARD_RETRY_COUNT = 3
const FLEET_FAMILIES: readonly FleetFamily[] = [
  'new_api',
  'sub2api',
  'conductor',
]
const ALL_SITES_VALUE = 'all'
const DASHBOARD_PREFERENCES_KEY = 'fleet-dashboard-preferences-v1'
const PANEL_CARD_CLASS = 'gap-0 rounded-lg py-0 shadow-xs'
const PANEL_HEADER_CLASS = 'border-border/70 border-b py-3.5'

const EMPTY_INSTANCES: ManagedInstance[] = []
const KPI_SKELETON_KEYS = [
  'availability',
  'requests',
  'rpm',
  'tokens',
  'quota',
  'coverage',
]

function retryDelay(attemptIndex: number): number {
  return Math.min(1000 * 2 ** attemptIndex, DASHBOARD_ERROR_RETRY_MS)
}

const compactNumber = new Intl.NumberFormat(undefined, {
  notation: 'compact',
  maximumFractionDigits: 1,
})
const exactNumber = new Intl.NumberFormat(undefined, {
  maximumFractionDigits: 2,
})
const compactCurrency = new Intl.NumberFormat(undefined, {
  style: 'currency',
  currency: 'USD',
  notation: 'compact',
  maximumFractionDigits: 2,
})
const exactDecimal = new Intl.NumberFormat(undefined, {
  minimumFractionDigits: 8,
  maximumFractionDigits: 8,
})
const trendAxisDate = new Intl.DateTimeFormat(undefined, {
  month: '2-digit',
  day: '2-digit',
  timeZone: 'UTC',
})
const trendTooltipDate = new Intl.DateTimeFormat(undefined, {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  timeZone: 'UTC',
})
const rpmMinuteTime = new Intl.DateTimeFormat(undefined, {
  hour: '2-digit',
  minute: '2-digit',
})
const rpmHourTime = new Intl.DateTimeFormat(undefined, {
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
})
const rpmTooltipTime = new Intl.DateTimeFormat(undefined, {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
})

function formatRPMTimestamp(
  value: unknown,
  formatter: Intl.DateTimeFormat
): string {
  const timestamp = Number(value)
  if (!Number.isFinite(timestamp)) return ''
  const date = new Date(timestamp * 1000)
  return Number.isFinite(date.getTime()) ? formatter.format(date) : ''
}

const CONSUMPTION_CHART_CONFIG = {
  value: { label: 'Value', color: 'var(--chart-1)' },
} satisfies ChartConfig

const RPM_CHART_CONFIG = {
  rpm: { label: '实时 RPM', color: 'var(--chart-2)' },
  capacity: { label: '最大容量', color: 'var(--chart-3)' },
} satisfies ChartConfig

const HEALTH_CHART_CONFIG = {
  healthy: { label: 'Healthy', color: 'var(--color-success)' },
  degraded: { label: 'Degraded', color: 'var(--color-warning)' },
  offline: { label: 'Offline', color: 'var(--color-destructive)' },
  auth_failed: {
    label: 'Auth failed',
    color: 'var(--color-fuchsia-500)',
  },
  unknown: { label: 'Unknown', color: 'var(--color-muted-foreground)' },
} satisfies ChartConfig

type FleetDashboardPreferences = {
  family: FleetFamily
  selectedInstances: Record<FleetFamily, string>
  timeRange: FleetTimeRange
  trendMetric: TrendMetricKey
  consumptionMetric: MetricKey
}

function defaultDashboardPreferences(): FleetDashboardPreferences {
  return {
    family: 'new_api',
    selectedInstances: {
      new_api: ALL_SITES_VALUE,
      sub2api: ALL_SITES_VALUE,
      conductor: ALL_SITES_VALUE,
    },
    timeRange: createFleetPresetRange(7),
    trendMetric: 'requests',
    consumptionMetric: 'requests',
  }
}

function isFleetFamily(value: unknown): value is FleetFamily {
  return FLEET_FAMILIES.some((family) => family === value)
}

function isMetricKey(value: unknown): value is MetricKey {
  return value === 'requests' || value === 'tokens' || value === 'quota'
}

function isTrendMetricKey(value: unknown): value is TrendMetricKey {
  return isMetricKey(value) || value === 'rpm'
}

function isFleetPresetDays(value: unknown): value is FleetPresetDays {
  return FLEET_TIME_PRESETS.some((preset) => preset.days === value)
}

function readDashboardPreferences(): FleetDashboardPreferences {
  const fallback = defaultDashboardPreferences()
  if (typeof window === 'undefined') return fallback
  try {
    const parsed = JSON.parse(
      window.localStorage.getItem(DASHBOARD_PREFERENCES_KEY) ?? '{}'
    ) as {
      family?: unknown
      selectedInstances?: Record<string, unknown>
      timeRange?: { start?: unknown; end?: unknown; presetDays?: unknown }
      metric?: unknown
      trendMetric?: unknown
      consumptionMetric?: unknown
    }
    const presetDays = isFleetPresetDays(parsed.timeRange?.presetDays)
      ? parsed.timeRange.presetDays
      : null
    const start = new Date(String(parsed.timeRange?.start ?? ''))
    const end = new Date(String(parsed.timeRange?.end ?? ''))
    const validCustomRange =
      !Number.isNaN(start.getTime()) &&
      !Number.isNaN(end.getTime()) &&
      start.getTime() <= end.getTime()
    let timeRange = fallback.timeRange
    if (presetDays) {
      timeRange = createFleetPresetRange(presetDays)
    } else if (validCustomRange) {
      timeRange = { start, end, presetDays: null }
    }

    const legacyMetric = isMetricKey(parsed.metric)
      ? parsed.metric
      : fallback.consumptionMetric

    return {
      family: isFleetFamily(parsed.family) ? parsed.family : fallback.family,
      selectedInstances: {
        new_api:
          typeof parsed.selectedInstances?.new_api === 'string'
            ? parsed.selectedInstances.new_api
            : ALL_SITES_VALUE,
        sub2api:
          typeof parsed.selectedInstances?.sub2api === 'string'
            ? parsed.selectedInstances.sub2api
            : ALL_SITES_VALUE,
        conductor:
          typeof parsed.selectedInstances?.conductor === 'string'
            ? parsed.selectedInstances.conductor
            : ALL_SITES_VALUE,
      },
      timeRange,
      trendMetric: isTrendMetricKey(parsed.trendMetric)
        ? parsed.trendMetric
        : legacyMetric,
      consumptionMetric: isMetricKey(parsed.consumptionMetric)
        ? parsed.consumptionMetric
        : legacyMetric,
    }
  } catch {
    return fallback
  }
}

function formatMetric(value: number | null, compact = true) {
  if (value == null) return '--'
  return (compact ? compactNumber : exactNumber).format(value)
}

function formatUsageMetric(
  metric: MetricKey,
  value: number | null,
  family: FleetFamily,
  compact = true
) {
  if (value == null) return '--'
  if (metric === 'quota' && family !== 'new_api') {
    return compact
      ? compactCurrency.format(value)
      : `US$${exactDecimal.format(value)}`
  }
  return formatMetric(value, compact)
}

function metricValue(
  sample: ManagedInstanceSummary['requests'] | undefined
): number | null {
  return sample?.collection_status === 'succeeded' && sample.value != null
    ? sample.value
    : null
}

function latestDashboardSection(
  cached: ManagedDashboardSnapshotSection | undefined,
  streamed: ManagedDashboardSnapshotSection | undefined
) {
  if (!streamed) return cached
  if (!cached) return streamed
  return streamed.last_attempt_at >= cached.last_attempt_at ? streamed : cached
}

function trendMetricValue(
  point: ManagedInstanceSummary['trend'][number],
  metric: MetricKey
) {
  if (metric === 'quota') return point.cost
  return point[metric]
}

function formatTrendDate(value: string, full = false) {
  const date = new Date(`${value}T00:00:00Z`)
  if (Number.isNaN(date.getTime())) return value
  return (full ? trendTooltipDate : trendAxisDate).format(date)
}

function metricLabel(metric: MetricKey, family: FleetFamily) {
  switch (metric) {
    case 'requests':
      return 'Requests'
    case 'tokens':
      return 'Tokens'
    case 'quota':
      return family !== 'new_api' ? 'Actual cost' : 'Quota'
  }
}

function familyLabel(family: FleetFamily) {
  if (family === 'sub2api') return 'Sub2API'
  if (family === 'conductor') return 'Conductor'
  return 'New API'
}

function belongsToFamily(instance: ManagedInstance, family: FleetFamily) {
  if (family === 'sub2api') return instance.kind === 'sub2api'
  if (family === 'conductor') return instance.kind === 'conductor'
  return instance.kind === 'new_api' || instance.kind === 'huichuan'
}

export function FleetDashboard() {
  const { t } = useTranslation()
  const initialPreferences = useMemo(readDashboardPreferences, [])
  const [family, setFamily] = useState<FleetFamily>(initialPreferences.family)
  const [selectedInstances, setSelectedInstances] = useState(
    initialPreferences.selectedInstances
  )
  const [timeRange, setTimeRange] = useState(initialPreferences.timeRange)
  const [trendMetric, setTrendMetric] = useState<TrendMetricKey>(
    initialPreferences.trendMetric
  )
  const [consumptionMetric, setConsumptionMetric] = useState<MetricKey>(
    initialPreferences.consumptionMetric
  )
  const [rpmHistoryBucket, setRPMHistoryBucket] =
    useState<ManagedInstanceRPMHistoryBucket>('minute')
  const [refreshRequestedAt, setRefreshRequestedAt] = useState(0)
  let effectiveTrendMetric = trendMetric
  if (
    family === 'conductor' &&
    (trendMetric === 'requests' || trendMetric === 'tokens')
  ) {
    effectiveTrendMetric = 'quota'
  } else if (family === 'new_api' && trendMetric === 'rpm') {
    effectiveTrendMetric = 'requests'
  }
  const effectiveConsumptionMetric =
    family === 'conductor' ? 'quota' : consumptionMetric

  const instancesQuery = useQuery({
    queryKey: ['fleet-dashboard-instances'],
    queryFn: () => getManagedInstances({ search: '', kind: '', status: '' }),
    placeholderData: keepPreviousData,
    retry: DASHBOARD_RETRY_COUNT,
    retryDelay,
    staleTime: Number.POSITIVE_INFINITY,
  })
  const allInstances = instancesQuery.data?.data.items ?? EMPTY_INSTANCES
  const familyInstances = useMemo(
    () => allInstances.filter((instance) => belongsToFamily(instance, family)),
    [allInstances, family]
  )
  const selectedInstanceID = selectedInstances[family]
  const selectedInstance = useMemo(
    () =>
      familyInstances.find(
        (instance) => String(instance.id) === selectedInstanceID
      ),
    [familyInstances, selectedInstanceID]
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
  const rpmHistoryInstanceIDs = useMemo(
    () =>
      instances
        .filter(
          (instance) =>
            instance.kind === 'conductor' || instance.kind === 'sub2api'
        )
        .map((instance) => instance.id),
    [instances]
  )
  const conductorRealtime = useManagedInstanceRealtimeEvents(
    conductorInstanceIDs,
    ['rpm', 'accounts', 'status']
  )
  const instanceIDs = useMemo(
    () => instances.map((instance) => instance.id),
    [instances]
  )
  const dashboardRangeInput = useMemo(() => {
    if (timeRange.presetDays) return { preset_days: timeRange.presetDays }
    const resolved = resolveFleetTimeRange(timeRange)
    return {
      start: Math.floor(resolved.start.getTime() / 1000),
      end: Math.floor(resolved.end.getTime() / 1000),
    }
  }, [timeRange])
  const dashboardEvents = useManagedDashboardEvents(instanceIDs)
  const dashboardQuery = useQuery({
    queryKey: [
      'fleet-dashboard-snapshots',
      instanceIDs.join(','),
      dashboardRangeInput.preset_days ?? 'custom',
      dashboardRangeInput.start ?? 0,
      dashboardRangeInput.end ?? 0,
    ],
    queryFn: () =>
      getManagedDashboardSnapshots(instanceIDs, dashboardRangeInput, {
        silent: true,
      }),
    enabled: instanceIDs.length > 0,
    retry: DASHBOARD_RETRY_COUNT,
    retryDelay,
    staleTime: Number.POSITIVE_INFINITY,
  })
  const rpmHistoryQuery = useQuery({
    queryKey: [
      'fleet-dashboard-rpm-history',
      rpmHistoryInstanceIDs.join(','),
      rpmHistoryBucket,
    ],
    queryFn: () =>
      getManagedInstanceRPMHistory(rpmHistoryInstanceIDs, rpmHistoryBucket, {
        silent: true,
      }),
    enabled:
      family !== 'new_api' &&
      effectiveTrendMetric === 'rpm' &&
      rpmHistoryInstanceIDs.length > 0,
    placeholderData: keepPreviousData,
    retry: DASHBOARD_RETRY_COUNT,
    retryDelay,
    staleTime: RPM_HISTORY_REFRESH_MS / 2,
    refetchInterval: (query) =>
      query.state.status === 'error'
        ? DASHBOARD_ERROR_RETRY_MS
        : RPM_HISTORY_REFRESH_MS,
    refetchIntervalInBackground: true,
  })
  const rpmQueries = useQueries({
    queries: instances.map((instance) => ({
      queryKey: ['fleet-dashboard-rpm', instance.id],
      queryFn: async () => {
        const response = await getManagedInstanceRealtimeMetrics(instance.id, {
          silent: true,
        })
        const observation = response.data
        const realtime = observation.data
        const hasRealtimeData =
          realtime?.rpm.collection_status === 'succeeded' ||
          realtime?.concurrency_collection_status === 'succeeded' ||
          realtime?.accounts_collection_status === 'succeeded'
        if (
          observation.collection_status !== 'succeeded' ||
          !realtime ||
          !hasRealtimeData
        ) {
          throw new Error(observation.error_code || 'rpm_unavailable')
        }
        return response
      },
      placeholderData: keepPreviousData,
      retry: DASHBOARD_RETRY_COUNT,
      retryDelay,
      enabled: instance.kind !== 'conductor',
      staleTime: RPM_REFRESH_MS / 2,
      refetchInterval: RPM_REFRESH_MS,
      refetchIntervalInBackground: true,
    })),
  })
  const rows = useMemo<InstanceMetricRow[]>(
    () =>
      instances.map((instance, index) => {
        const cached = dashboardQuery.data?.data.items.find(
          (item) => item.instance_id === instance.id
        )
        const rangeKey = dashboardQuery.data?.data.range.range_key ?? ''
        const summarySection = latestDashboardSection(
          cached?.summary,
          dashboardEvents.snapshots[`${instance.id}:${rangeKey}`]
        )
        const todaySection = latestDashboardSection(
          cached?.today,
          dashboardEvents.snapshots[`${instance.id}:preset-1`]
        )
        const observation = summarySection?.observation
        const summary = observation?.data
        const streamed = conductorRealtime.states[instance.id]
        const polledRealtime = rpmQueries[index]?.data?.data?.data
        const realtime =
          instance.kind === 'conductor' ? streamed : polledRealtime
        const accountsReady =
          instance.kind === 'conductor'
            ? Boolean(streamed?.observed_at)
            : instance.kind === 'sub2api' &&
              polledRealtime?.accounts_collection_status === 'succeeded'
        const todayCost = metricValue(todaySection?.observation?.data?.cost)
        return {
          instance,
          summary,
          observedAt: Math.max(
            observation?.observed_at ?? 0,
            todaySection?.observation?.observed_at ?? 0,
            streamed?.observed_at ?? 0
          ),
          summaryObservedAt: observation?.observed_at ?? 0,
          todayObservedAt: todaySection?.observation?.observed_at ?? 0,
          collected: observation?.collection_status === 'succeeded',
          requests: metricValue(summary?.requests),
          rpm: metricValue(realtime?.rpm),
          rpmCapacity: metricValue(realtime?.rpm_capacity),
          concurrencyUsed: metricValue(realtime?.concurrency_used),
          concurrencyMax: metricValue(realtime?.concurrency_max),
          accountsAvailable: accountsReady
            ? (realtime?.accounts_available ?? 0)
            : null,
          accountsTotal: accountsReady ? (realtime?.accounts_total ?? 0) : null,
          todayCost,
          tokens: metricValue(summary?.tokens),
          quota: metricValue(summary?.cost),
          lastAttemptAt: Math.max(
            summarySection?.last_attempt_at ?? 0,
            todaySection?.last_attempt_at ?? 0
          ),
          lastAttemptStatus:
            summarySection?.last_attempt_status === 'failed' ||
            todaySection?.last_attempt_status === 'failed'
              ? 'failed'
              : (summarySection?.last_attempt_status ?? ''),
          lastErrorCode:
            summarySection?.last_error_code ??
            todaySection?.last_error_code ??
            '',
          stale: Boolean(summarySection?.stale || todaySection?.stale),
        }
      }),
    [
      conductorRealtime.states,
      dashboardEvents.snapshots,
      dashboardQuery.data,
      instances,
      rpmQueries,
    ]
  )

  const totals = useMemo(
    () => ({
      requests: rows.reduce((sum, row) => sum + (row.requests ?? 0), 0),
      rpm: rows.reduce((sum, row) => sum + (row.rpm ?? 0), 0),
      rpmCapacity: rows.reduce((sum, row) => sum + (row.rpmCapacity ?? 0), 0),
      concurrencyUsed: rows.reduce(
        (sum, row) => sum + (row.concurrencyUsed ?? 0),
        0
      ),
      concurrencyMax: rows.reduce(
        (sum, row) => sum + (row.concurrencyMax ?? 0),
        0
      ),
      accountsAvailable: rows.reduce(
        (sum, row) => sum + (row.accountsAvailable ?? 0),
        0
      ),
      accountsTotal: rows.reduce(
        (sum, row) => sum + (row.accountsTotal ?? 0),
        0
      ),
      todayCost: rows.reduce((sum, row) => sum + (row.todayCost ?? 0), 0),
      tokens: rows.reduce((sum, row) => sum + (row.tokens ?? 0), 0),
      quota: rows.reduce((sum, row) => sum + (row.quota ?? 0), 0),
      collected: rows.filter((row) => row.collected).length,
      metricReady: rows.filter(
        (row) => row.requests != null || row.tokens != null || row.quota != null
      ).length,
      requestsReady: rows.filter((row) => row.requests != null).length,
      rpmReady: rows.filter((row) => row.rpm != null).length,
      rpmCapacityReady: rows.filter((row) => row.rpmCapacity != null).length,
      concurrencyReady: rows.filter(
        (row) => row.concurrencyUsed != null && row.concurrencyMax != null
      ).length,
      accountsReady: rows.filter((row) => row.accountsTotal != null).length,
      todayCostReady: rows.filter((row) => row.todayCost != null).length,
      tokensReady: rows.filter((row) => row.tokens != null).length,
      quotaReady: rows.filter((row) => row.quota != null).length,
      healthy: instances.filter((item) => item.status === 'healthy').length,
    }),
    [instances, rows]
  )
  const healthData = useMemo<HealthData[]>(() => {
    const statuses = [
      { key: 'healthy', label: t('Healthy'), color: 'var(--color-success)' },
      { key: 'degraded', label: t('Degraded'), color: 'var(--color-warning)' },
      {
        key: 'offline',
        label: t('Offline'),
        color: 'var(--color-destructive)',
      },
      {
        key: 'auth_failed',
        label: t('Auth failed'),
        color: 'var(--color-fuchsia-500)',
      },
      {
        key: 'unknown',
        label: t('Unknown'),
        color: 'var(--color-muted-foreground)',
      },
    ]
    return statuses
      .map((status) => ({
        ...status,
        value: instances.filter((item) => item.status === status.key).length,
      }))
      .filter((item) => item.value > 0)
  }, [instances, t])
  const chartData = useMemo(
    () =>
      [...rows]
        .filter((row) => row[effectiveConsumptionMetric] != null)
        .sort(
          (a, b) =>
            (b[effectiveConsumptionMetric] ?? 0) -
            (a[effectiveConsumptionMetric] ?? 0)
        )
        .slice(0, 10)
        .map((row) => ({
          name: row.instance.name,
          value: row[effectiveConsumptionMetric] ?? 0,
        })),
    [effectiveConsumptionMetric, rows]
  )
  const dailyUsageData = useMemo<DailyUsageData[]>(() => {
    if (effectiveTrendMetric === 'rpm') return []
    const values = new Map<string, number>()
    for (const row of rows) {
      for (const point of row.summary?.trend ?? []) {
        values.set(
          point.date,
          (values.get(point.date) ?? 0) +
            trendMetricValue(point, effectiveTrendMetric)
        )
      }
    }
    return [...values.entries()]
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([date, value]) => ({ date, value }))
  }, [effectiveTrendMetric, rows])
  const dailyUsageLoading =
    dailyUsageData.length === 0 &&
    (dashboardQuery.isPending ||
      rows.some((row) => !row.collected && row.lastAttemptStatus !== 'failed'))
  const dailyUsageError =
    !dailyUsageLoading &&
    dailyUsageData.length === 0 &&
    (dashboardQuery.isError ||
      rows.some((row) => row.lastAttemptStatus === 'failed'))
  const connectionFailed =
    dashboardQuery.isError ||
    rows.some((row) => row.lastAttemptStatus === 'failed') ||
    rpmQueries.some((query) => isInstanceConnectionError(query.error))
  const isRefreshing =
    dashboardQuery.isFetching ||
    rows.some((row) => row.lastAttemptStatus === 'running') ||
    (refreshRequestedAt > 0 &&
      rows.some((row) => row.lastAttemptAt < refreshRequestedAt))
  const coverage = instances.length
    ? Math.round((totals.collected / instances.length) * 100)
    : 0
  const metricCoverage = instances.length
    ? Math.round(
        ((family === 'conductor' ? totals.quotaReady : totals.metricReady) /
          instances.length) *
          100
      )
    : 0
  const healthRate = instances.length
    ? Math.round((totals.healthy / instances.length) * 1000) / 10
    : 0
  const familyCounts = useMemo(
    () => ({
      new_api: allInstances.filter((instance) =>
        belongsToFamily(instance, 'new_api')
      ).length,
      sub2api: allInstances.filter((instance) =>
        belongsToFamily(instance, 'sub2api')
      ).length,
      conductor: allInstances.filter((instance) =>
        belongsToFamily(instance, 'conductor')
      ).length,
    }),
    [allInstances]
  )

  useEffect(() => {
    if (!instancesQuery.isSuccess || familyCounts[family] > 0) return
    const nextFamily = FLEET_FAMILIES.find(
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
      DASHBOARD_PREFERENCES_KEY,
      JSON.stringify({
        family,
        selectedInstances,
        timeRange: {
          start: timeRange.start.toISOString(),
          end: timeRange.end.toISOString(),
          presetDays: timeRange.presetDays,
        },
        metric: consumptionMetric,
        trendMetric,
        consumptionMetric,
      })
    )
  }, [consumptionMetric, family, selectedInstances, timeRange, trendMetric])

  const lastObservedAt = Math.max(0, ...rows.map((row) => row.observedAt))
  const refetchInstances = instancesQuery.refetch

  useEffect(() => {
    if (dashboardEvents.topologyRevision > 0) void refetchInstances()
  }, [dashboardEvents.topologyRevision, refetchInstances])

  useEffect(() => {
    if (
      refreshRequestedAt > 0 &&
      rows.length > 0 &&
      rows.every(
        (row) =>
          row.lastAttemptAt >= refreshRequestedAt &&
          row.lastAttemptStatus !== 'running'
      )
    ) {
      setRefreshRequestedAt(0)
    }
  }, [refreshRequestedAt, rows])

  const refresh = async () => {
    const requestedAt = Math.floor(Date.now() / 1000)
    setRefreshRequestedAt(requestedAt)
    void instancesQuery.refetch()
    try {
      await refreshManagedDashboard(instanceIDs, dashboardRangeInput)
    } catch {
      setRefreshRequestedAt(0)
    }
    for (const query of rpmQueries) void query.refetch()
    if (family !== 'new_api' && effectiveTrendMetric === 'rpm') {
      void rpmHistoryQuery.refetch()
    }
    dashboardEvents.reconnect()
  }

  const refreshRPM = () => {
    conductorRealtime.reconnect()
    for (const query of rpmQueries) void query.refetch()
  }
  const rpmRefreshing =
    rpmQueries.some((query) => query.isFetching) ||
    ['connecting', 'reconnecting'].includes(conductorRealtime.status)

  const handleFamilyChange = (nextFamily: FleetFamily) => {
    setFamily(nextFamily)
  }

  let content: ReactNode
  if (instancesQuery.isLoading) {
    content = <DashboardSkeleton />
  } else if (instancesQuery.isError && !instancesQuery.data) {
    content = <DashboardError onRetry={refresh} />
  } else if (instances.length === 0) {
    content = <EmptyFleet family={family} />
  } else {
    content = (
      <div className='grid gap-4'>
        {connectionFailed && (
          <InstanceConnectionAlert
            onRetry={refresh}
            retrying={isRefreshing || rpmRefreshing}
          />
        )}
        <DashboardContent
          instances={instances}
          family={family}
          rows={rows}
          totals={totals}
          healthData={healthData}
          chartData={chartData}
          dailyUsageData={dailyUsageData}
          dailyUsageLoading={dailyUsageLoading}
          dailyUsageError={dailyUsageError}
          healthRate={healthRate}
          coverage={coverage}
          metricCoverage={metricCoverage}
          trendMetric={effectiveTrendMetric}
          onTrendMetricChange={setTrendMetric}
          consumptionMetric={effectiveConsumptionMetric}
          onConsumptionMetricChange={setConsumptionMetric}
          rpmRefreshing={rpmRefreshing}
          onRPMRefresh={refreshRPM}
          rpmHistoryBucket={rpmHistoryBucket}
          onRPMHistoryBucketChange={setRPMHistoryBucket}
          rpmHistoryData={rpmHistoryQuery.data?.data.points ?? []}
          rpmHistoryLoading={rpmHistoryQuery.isLoading}
          rpmHistoryError={rpmHistoryQuery.isError}
        />
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Fleet overview')}</SectionPageLayout.Title>
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
          <div className='bg-card border-border/80 flex flex-wrap items-center justify-between gap-3 rounded-lg border px-2.5 py-2 shadow-xs sm:px-3'>
            <div className='flex min-w-0 flex-1 flex-wrap items-center gap-2'>
              <SegmentedControl
                value={family}
                options={FLEET_FAMILIES}
                getLabel={(option) =>
                  `${t(familyLabel(option))} · ${familyCounts[option]}`
                }
                onChange={handleFamilyChange}
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
                  if (value) {
                    setSelectedInstances((current) => ({
                      ...current,
                      [family]: value,
                    }))
                  }
                }}
              >
                <SelectTrigger
                  size='sm'
                  className='w-full min-w-0 sm:w-64'
                  aria-label={t('Select site')}
                >
                  <Building2 className='text-muted-foreground size-3.5' />
                  <SelectValue placeholder={t('Select site')} />
                </SelectTrigger>
                <SelectContent
                  align='start'
                  alignItemWithTrigger={false}
                  className='min-w-64'
                >
                  <SelectGroup>
                    <SelectItem value={ALL_SITES_VALUE}>
                      <span className='flex min-w-0 flex-1 items-center justify-between gap-3'>
                        <span className='truncate'>{t('All sites')}</span>
                        <span className='text-muted-foreground font-mono text-xs tabular-nums'>
                          {familyInstances.length}
                        </span>
                      </span>
                    </SelectItem>
                    {familyInstances.map((instance) => (
                      <SelectItem key={instance.id} value={String(instance.id)}>
                        <span className='flex min-w-0 flex-1 items-center gap-2'>
                          <span
                            className={cn(
                              'size-1.5 shrink-0 rounded-full',
                              instance.status === 'healthy' && 'bg-emerald-500',
                              instance.status === 'degraded' && 'bg-amber-500',
                              instance.status === 'offline' && 'bg-red-500',
                              instance.status === 'auth_failed' &&
                                'bg-fuchsia-500',
                              instance.status === 'unknown' &&
                                'bg-muted-foreground/50'
                            )}
                            aria-hidden='true'
                          />
                          <span className='truncate'>{instance.name}</span>
                        </span>
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className='flex min-w-0 items-center gap-2 text-xs'>
              <span className='border-success/20 bg-success/5 text-success flex items-center gap-1.5 rounded-md border px-2 py-1 font-medium'>
                <Radio
                  className={cn('size-3.5', isRefreshing && 'animate-pulse')}
                />
                {t('Auto-refreshing every {{seconds}}s', {
                  seconds: 60,
                })}
              </span>
              {lastObservedAt > 0 && (
                <span className='text-muted-foreground hidden tabular-nums sm:inline'>
                  {t('Updated {{time}}', {
                    time: new Date(lastObservedAt * 1000).toLocaleTimeString(),
                  })}
                </span>
              )}
            </div>
          </div>
          {content}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

type DashboardContentProps = {
  instances: ManagedInstance[]
  family: FleetFamily
  rows: InstanceMetricRow[]
  totals: {
    requests: number
    rpm: number
    rpmCapacity: number
    concurrencyUsed: number
    concurrencyMax: number
    accountsAvailable: number
    accountsTotal: number
    todayCost: number
    tokens: number
    quota: number
    collected: number
    metricReady: number
    requestsReady: number
    rpmReady: number
    rpmCapacityReady: number
    concurrencyReady: number
    accountsReady: number
    todayCostReady: number
    tokensReady: number
    quotaReady: number
    healthy: number
  }
  healthData: HealthData[]
  chartData: { name: string; value: number }[]
  dailyUsageData: DailyUsageData[]
  dailyUsageLoading: boolean
  dailyUsageError: boolean
  healthRate: number
  coverage: number
  metricCoverage: number
  trendMetric: TrendMetricKey
  onTrendMetricChange: (metric: TrendMetricKey) => void
  consumptionMetric: MetricKey
  onConsumptionMetricChange: (metric: MetricKey) => void
  rpmRefreshing: boolean
  onRPMRefresh: () => void
  rpmHistoryBucket: ManagedInstanceRPMHistoryBucket
  onRPMHistoryBucketChange: (bucket: ManagedInstanceRPMHistoryBucket) => void
  rpmHistoryData: RPMHistoryData[]
  rpmHistoryLoading: boolean
  rpmHistoryError: boolean
}

function DashboardContent(props: DashboardContentProps) {
  const observedAt = Math.max(
    0,
    ...props.rows.map((row) => row.summaryObservedAt)
  )
  return (
    <div className='grid gap-4 pb-6'>
      <SummaryGrid {...props} />
      <DailyUsagePanel
        family={props.family}
        metric={props.trendMetric}
        data={props.dailyUsageData}
        loading={props.dailyUsageLoading}
        error={props.dailyUsageError}
        onMetricChange={props.onTrendMetricChange}
        rpmHistoryBucket={props.rpmHistoryBucket}
        onRPMHistoryBucketChange={props.onRPMHistoryBucketChange}
        rpmHistoryData={props.rpmHistoryData}
        rpmHistoryLoading={props.rpmHistoryLoading}
        rpmHistoryError={props.rpmHistoryError}
        observedAt={observedAt}
      />
      <section className='grid gap-4 xl:grid-cols-[minmax(0,1.7fr)_minmax(300px,0.8fr)]'>
        <ConsumptionPanel
          family={props.family}
          metric={props.consumptionMetric}
          data={props.chartData}
          onMetricChange={props.onConsumptionMetricChange}
          observedAt={observedAt}
        />
        <HealthPanel data={props.healthData} total={props.instances.length} />
      </section>
      <PerformanceTable family={props.family} rows={props.rows} />
    </div>
  )
}

function DailyUsagePanel(props: {
  family: FleetFamily
  metric: TrendMetricKey
  data: DailyUsageData[]
  loading: boolean
  error: boolean
  onMetricChange: (metric: TrendMetricKey) => void
  rpmHistoryBucket: ManagedInstanceRPMHistoryBucket
  onRPMHistoryBucketChange: (bucket: ManagedInstanceRPMHistoryBucket) => void
  rpmHistoryData: RPMHistoryData[]
  rpmHistoryLoading: boolean
  rpmHistoryError: boolean
  observedAt: number
}) {
  const { t } = useTranslation()
  const isRPM = props.family !== 'new_api' && props.metric === 'rpm'
  const usageMetric = props.metric === 'rpm' ? null : props.metric
  let subtitle = t('Daily totals in the selected period')
  if (isRPM) {
    subtitle =
      props.rpmHistoryBucket === 'minute'
        ? t('Average RPM per minute over the last 60 minutes')
        : t('Average RPM per hour over the last 24 hours')
  }
  let metricOptions: TrendMetricKey[] = ['requests', 'tokens', 'quota']
  if (props.family === 'conductor') metricOptions = ['quota', 'rpm']
  else if (props.family === 'sub2api') metricOptions.push('rpm')
  return (
    <Card className={PANEL_CARD_CLASS}>
      <CardHeader
        className={cn(
          PANEL_HEADER_CLASS,
          'flex-row items-start justify-between gap-4 space-y-0'
        )}
      >
        <div>
          <CardTitle>{t('Daily usage trend')}</CardTitle>
          <p className='text-muted-foreground mt-1 text-sm'>{subtitle}</p>
        </div>
        <div className='flex flex-wrap items-center justify-end gap-2'>
          <SegmentedControl
            value={props.metric}
            options={metricOptions}
            getLabel={(value) =>
              value === 'rpm' ? 'RPM' : t(metricLabel(value, props.family))
            }
            onChange={props.onMetricChange}
          />
          {isRPM && (
            <SegmentedControl
              value={props.rpmHistoryBucket}
              options={['minute', 'hour']}
              getLabel={(value) =>
                t(value === 'minute' ? 'By minute' : 'By hour')
              }
              onChange={props.onRPMHistoryBucketChange}
            />
          )}
        </div>
      </CardHeader>
      <CardContent className='py-4'>
        {isRPM &&
          props.rpmHistoryLoading &&
          props.rpmHistoryData.length === 0 && (
            <div className='text-muted-foreground flex min-h-[280px] items-center justify-center gap-2 text-sm'>
              <RefreshCw className='size-4 animate-spin' aria-hidden='true' />
              <span>{t('Data loading')}</span>
            </div>
          )}
        {isRPM && props.rpmHistoryData.length > 0 && (
          <ChartContainer
            config={RPM_CHART_CONFIG}
            className='aspect-auto h-[300px] w-full'
          >
            <LineChart
              accessibilityLayer
              data={props.rpmHistoryData}
              margin={{ top: 10, right: 12, left: -12, bottom: 4 }}
            >
              <CartesianGrid vertical={false} strokeDasharray='3 3' />
              <XAxis
                dataKey='timestamp'
                axisLine={false}
                tickLine={false}
                tickMargin={10}
                minTickGap={36}
                tickFormatter={(value: number) =>
                  formatRPMTimestamp(
                    value,
                    props.rpmHistoryBucket === 'minute'
                      ? rpmMinuteTime
                      : rpmHourTime
                  )
                }
              />
              <YAxis
                axisLine={false}
                tickLine={false}
                width={54}
                domain={[0, 'auto']}
                tickFormatter={(value: number) => formatMetric(value, false)}
              />
              <ChartTooltip
                cursor={{ stroke: 'var(--color-border)' }}
                content={
                  <ChartTooltipContent
                    indicator='dot'
                    labelFormatter={(_, payload) =>
                      formatRPMTimestamp(
                        payload?.[0]?.payload?.timestamp,
                        rpmTooltipTime
                      )
                    }
                    formatter={(value, name, item) => {
                      const point = item.payload as RPMHistoryData
                      const utilization =
                        point.capacity != null && point.capacity > 0
                          ? (point.rpm / point.capacity) * 100
                          : null
                      return (
                        <div className='flex w-full min-w-44 items-center gap-2'>
                          <span
                            className='size-2 shrink-0 rounded-sm'
                            style={{ backgroundColor: item.color }}
                          />
                          <span className='text-muted-foreground flex-1'>
                            {String(name)}
                          </span>
                          <span className='font-mono font-medium tabular-nums'>
                            {formatMetric(Number(value), false)} RPM
                            {name === '最大容量' && utilization != null
                              ? ` · 占用 ${utilization.toFixed(1)}%`
                              : ''}
                          </span>
                        </div>
                      )
                    }}
                  />
                }
              />
              <Line
                type='monotone'
                dataKey='rpm'
                name='实时 RPM'
                stroke='var(--color-rpm)'
                strokeWidth={2.25}
                dot={props.rpmHistoryData.length <= 24}
                activeDot={{ r: 4 }}
              />
              {props.family === 'conductor' && (
                <Line
                  type='stepAfter'
                  dataKey='capacity'
                  name='最大容量'
                  stroke='var(--color-capacity)'
                  strokeWidth={2}
                  strokeDasharray='7 5'
                  dot={false}
                  activeDot={{ r: 4 }}
                  connectNulls={false}
                />
              )}
              <ChartLegend content={<ChartLegendContent />} />
            </LineChart>
          </ChartContainer>
        )}
        {isRPM &&
          props.rpmHistoryData.length === 0 &&
          !props.rpmHistoryLoading &&
          props.rpmHistoryError && <PanelEmpty text={t('Failed to load')} />}
        {isRPM &&
          props.rpmHistoryData.length === 0 &&
          !props.rpmHistoryLoading &&
          !props.rpmHistoryError && (
            <PanelEmpty text={t('RPM history is being collected')} />
          )}
        {usageMetric && props.loading && (
          <div className='text-muted-foreground flex min-h-[280px] items-center justify-center gap-2 text-sm'>
            <RefreshCw className='size-4 animate-spin' aria-hidden='true' />
            <span>{t('Data loading')}</span>
          </div>
        )}
        {usageMetric && !props.loading && props.data.length > 0 && (
          <ChartContainer
            config={CONSUMPTION_CHART_CONFIG}
            className='aspect-auto h-[300px] w-full'
          >
            <LineChart
              accessibilityLayer
              data={props.data}
              margin={{ top: 10, right: 12, left: -12, bottom: 4 }}
            >
              <CartesianGrid vertical={false} strokeDasharray='3 3' />
              <XAxis
                dataKey='date'
                axisLine={false}
                tickLine={false}
                tickMargin={10}
                minTickGap={28}
                tickFormatter={(value: string) => formatTrendDate(value)}
              />
              <YAxis
                axisLine={false}
                tickLine={false}
                width={54}
                tickFormatter={(value: number) =>
                  formatUsageMetric(usageMetric, value, props.family)
                }
              />
              <ChartTooltip
                cursor={{ stroke: 'var(--color-border)' }}
                content={
                  <ChartTooltipContent
                    indicator='line'
                    labelFormatter={(label) =>
                      formatTrendDate(String(label), true)
                    }
                    formatter={(value) => (
                      <span className='grid gap-0.5'>
                        <span className='font-mono font-medium tabular-nums'>
                          {formatUsageMetric(
                            usageMetric,
                            Number(value),
                            props.family,
                            false
                          )}
                        </span>
                        {props.observedAt > 0 && (
                          <span className='text-muted-foreground text-[11px]'>
                            {t('Collected at {{time}}', {
                              time: new Date(
                                props.observedAt * 1000
                              ).toLocaleString(),
                            })}
                          </span>
                        )}
                      </span>
                    )}
                  />
                }
              />
              <Line
                type='monotone'
                dataKey='value'
                name={t(metricLabel(usageMetric, props.family))}
                stroke='var(--color-value)'
                strokeWidth={2.25}
                dot={props.data.length <= 14}
                activeDot={{ r: 4 }}
              />
            </LineChart>
          </ChartContainer>
        )}
        {usageMetric && !props.loading && props.error && (
          <PanelEmpty text={t('Failed to load')} />
        )}
        {usageMetric &&
          !props.loading &&
          !props.error &&
          props.data.length === 0 && (
            <PanelEmpty text={t('No daily usage data for this period')} />
          )}
      </CardContent>
    </Card>
  )
}

function SummaryGrid(props: DashboardContentProps) {
  const { t } = useTranslation()
  let availabilityTone: MetricCardTone = 'success'
  if (props.healthRate < 75) availabilityTone = 'danger'
  else if (props.healthRate < 100) availabilityTone = 'amber'
  const costLabel =
    props.family !== 'new_api' ? t('Actual cost') : t('Quota usage')
  let costDetail = t('Remote quota units')
  if (props.family === 'sub2api') {
    costDetail = t('Actual cost reported by Sub2API')
  } else if (props.family === 'conductor') {
    costDetail = t('Actual cost calculated from Conductor prices')
  }
  const rpmDetail = `${t('Last 60 seconds across {{count}} instances', {
    count: props.totals.rpmReady,
  })} · ${t('Auto-refreshing every {{seconds}}s', {
    seconds: RPM_REFRESH_MS / 1000,
  })}`
  const capacityReady =
    props.family === 'conductor' &&
    props.instances.length > 0 &&
    props.totals.rpmCapacityReady === props.instances.length
  const rpmValue = props.totals.rpmReady
    ? `${formatMetric(props.totals.rpm)} / ${capacityReady ? formatMetric(props.totals.rpmCapacity) : '--'}`
    : `-- / ${capacityReady ? formatMetric(props.totals.rpmCapacity) : '--'}`
  const rpmCapacityDetail = capacityReady
    ? `${props.totals.rpmCapacity > 0 ? ((props.totals.rpm / props.totals.rpmCapacity) * 100).toFixed(1) : '0.0'}% · ${formatMetric(props.totals.accountsAvailable, false)} 个可用账号`
    : '最大容量数据加载中'
  const showsAccountMetrics =
    props.family === 'conductor' || props.family === 'sub2api'
  const costObservedAt = Math.max(
    0,
    ...props.rows.map((row) => row.summaryObservedAt)
  )
  const todayObservedAt = Math.max(
    0,
    ...props.rows.map((row) => row.todayObservedAt)
  )
  const amountStale = props.rows.some((row) => row.stale)
  const summaryPending = props.rows.some(
    (row) => !row.collected && row.lastAttemptStatus !== 'failed'
  )
  let resolvedCostDetail = t('No metric data available')
  if (props.totals.quotaReady) resolvedCostDetail = costDetail
  else if (summaryPending) resolvedCostDetail = t('Data loading')
  let todayCostDetail = t('No metric data available')
  if (props.totals.todayCostReady) {
    todayCostDetail = t('Across {{count}} instances', {
      count: props.totals.todayCostReady,
    })
  } else if (summaryPending) {
    todayCostDetail = t('Data loading')
  }

  return (
    <section
      aria-label={t('Fleet summary')}
      className={cn(
        'bg-border border-border/80 grid grid-cols-2 gap-px overflow-hidden rounded-lg border shadow-xs sm:grid-cols-3',
        props.family === 'conductor' && 'xl:grid-cols-7',
        props.family === 'sub2api' && 'xl:grid-cols-4 2xl:grid-cols-7',
        props.family === 'new_api' && 'xl:grid-cols-6'
      )}
    >
      <MetricCard
        icon={CheckCircle2}
        label={t('Fleet availability')}
        value={`${props.healthRate}%`}
        detail={t('{{healthy}} of {{total}} healthy', {
          healthy: props.totals.healthy,
          total: props.instances.length,
        })}
        tone={availabilityTone}
      />
      {props.family === 'new_api' && (
        <MetricCard
          icon={Activity}
          label={t('Requests')}
          value={formatMetric(
            props.totals.requestsReady ? props.totals.requests : null
          )}
          detail={t('Across {{count}} instances', {
            count: props.totals.requestsReady,
          })}
          tone='blue'
        />
      )}
      {props.family === 'sub2api' ? (
        <MetricCard
          icon={Cpu}
          label={t('Concurrency')}
          value={
            props.totals.concurrencyReady === props.instances.length
              ? `${formatMetric(props.totals.concurrencyUsed, false)} / ${formatMetric(props.totals.concurrencyMax, false)}`
              : '-- / --'
          }
          detail={t('Sub2API group {{id}}', { id: 49 })}
          tone='amber'
          action={
            <Button
              variant='ghost'
              size='icon-xs'
              aria-label={t('Refresh')}
              title={t('Refresh')}
              onClick={props.onRPMRefresh}
            >
              <RefreshCw
                className={cn('size-3', props.rpmRefreshing && 'animate-spin')}
              />
            </Button>
          }
        />
      ) : (
        <MetricCard
          icon={Radio}
          label='RPM'
          value={
            props.family === 'conductor'
              ? rpmValue
              : formatMetric(props.totals.rpmReady ? props.totals.rpm : null)
          }
          detail={props.family === 'conductor' ? rpmCapacityDetail : rpmDetail}
          tone='success'
          action={
            <Button
              variant='ghost'
              size='icon-xs'
              aria-label={t('Refresh')}
              title={t('Refresh')}
              onClick={props.onRPMRefresh}
            >
              <RefreshCw
                className={cn('size-3', props.rpmRefreshing && 'animate-spin')}
              />
            </Button>
          }
        />
      )}
      {showsAccountMetrics && (
        <MetricCard
          icon={UserCheck}
          label={t('Available accounts')}
          value={formatMetric(
            props.totals.accountsReady ? props.totals.accountsAvailable : null,
            false
          )}
          detail={t('Real-time across {{count}} instances', {
            count: props.totals.accountsReady,
          })}
          tone='success'
        />
      )}
      {showsAccountMetrics && (
        <MetricCard
          icon={Users}
          label={t('Total accounts')}
          value={formatMetric(
            props.totals.accountsReady ? props.totals.accountsTotal : null,
            false
          )}
          detail={t('Real-time across {{count}} instances', {
            count: props.totals.accountsReady,
          })}
          tone='blue'
        />
      )}
      {props.family === 'new_api' && (
        <MetricCard
          icon={DatabaseZap}
          label={t('Tokens')}
          value={formatMetric(
            props.totals.tokensReady ? props.totals.tokens : null
          )}
          detail={t('Across {{count}} instances', {
            count: props.totals.tokensReady,
          })}
          tone='violet'
        />
      )}
      <MetricCard
        icon={CircleDollarSign}
        label={costLabel}
        value={formatUsageMetric(
          'quota',
          props.totals.quotaReady ? props.totals.quota : null,
          props.family
        )}
        detail={resolvedCostDetail}
        tone='amber'
        exactValue={
          props.family !== 'new_api' && props.totals.quotaReady
            ? `US$${exactDecimal.format(props.totals.quota)}`
            : undefined
        }
        observedAt={costObservedAt}
        stale={amountStale}
      />
      {props.family !== 'new_api' && (
        <MetricCard
          icon={CircleDollarSign}
          label={t('Today consumption')}
          value={formatUsageMetric(
            'quota',
            props.totals.todayCostReady ? props.totals.todayCost : null,
            props.family
          )}
          detail={todayCostDetail}
          tone='violet'
          exactValue={
            props.totals.todayCostReady
              ? `US$${exactDecimal.format(props.totals.todayCost)}`
              : undefined
          }
          observedAt={todayObservedAt}
          stale={amountStale}
        />
      )}
      <MetricCard
        icon={Gauge}
        label={t('Metric coverage')}
        value={`${props.metricCoverage}%`}
        detail={t('{{count}} metrics available', {
          count:
            props.family === 'conductor'
              ? props.totals.quotaReady
              : props.totals.metricReady,
        })}
        tone='neutral'
      />
    </section>
  )
}

function ConsumptionPanel(props: {
  family: FleetFamily
  metric: MetricKey
  data: { name: string; value: number }[]
  onMetricChange: (metric: MetricKey) => void
  observedAt: number
}) {
  const { t } = useTranslation()
  return (
    <Card className={PANEL_CARD_CLASS}>
      <CardHeader
        className={cn(
          PANEL_HEADER_CLASS,
          'flex-row items-start justify-between gap-4 space-y-0'
        )}
      >
        <div>
          <CardTitle>{t('Instance consumption')}</CardTitle>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Top instances in the selected period')}
          </p>
        </div>
        {props.family !== 'conductor' && (
          <SegmentedControl
            value={props.metric}
            options={['requests', 'tokens', 'quota']}
            getLabel={(value) => t(metricLabel(value, props.family))}
            onChange={props.onMetricChange}
          />
        )}
      </CardHeader>
      <CardContent className='py-4'>
        {props.data.length ? (
          <ChartContainer
            config={CONSUMPTION_CHART_CONFIG}
            className='aspect-auto h-[280px] w-full'
          >
            <BarChart
              accessibilityLayer
              data={props.data}
              margin={{ top: 8, right: 8, left: -12, bottom: 4 }}
            >
              <CartesianGrid vertical={false} strokeDasharray='3 3' />
              <XAxis
                dataKey='name'
                axisLine={false}
                tickLine={false}
                tickMargin={10}
                interval={0}
                tickFormatter={(value: string) =>
                  value.length > 8 ? `${value.slice(0, 8)}…` : value
                }
              />
              <YAxis
                axisLine={false}
                tickLine={false}
                width={54}
                tickFormatter={(value: number) =>
                  formatUsageMetric(props.metric, value, props.family)
                }
              />
              <ChartTooltip
                cursor={{ fill: 'var(--color-muted)', opacity: 0.45 }}
                content={
                  <ChartTooltipContent
                    formatter={(value) => (
                      <span className='grid gap-0.5'>
                        <span className='font-mono font-medium tabular-nums'>
                          {formatUsageMetric(
                            props.metric,
                            Number(value),
                            props.family,
                            false
                          )}
                        </span>
                        {props.observedAt > 0 && (
                          <span className='text-muted-foreground text-[11px]'>
                            {t('Collected at {{time}}', {
                              time: new Date(
                                props.observedAt * 1000
                              ).toLocaleString(),
                            })}
                          </span>
                        )}
                      </span>
                    )}
                  />
                }
              />
              <Bar
                dataKey='value'
                name={t(metricLabel(props.metric, props.family))}
                fill='var(--color-value)'
                radius={[4, 4, 0, 0]}
                maxBarSize={42}
              />
            </BarChart>
          </ChartContainer>
        ) : (
          <PanelEmpty text={t('No metric data for this period')} />
        )}
      </CardContent>
    </Card>
  )
}

function HealthPanel({ data, total }: { data: HealthData[]; total: number }) {
  const { t } = useTranslation()
  return (
    <Card className={PANEL_CARD_CLASS}>
      <CardHeader className={PANEL_HEADER_CLASS}>
        <CardTitle>{t('Fleet health')}</CardTitle>
        <p className='text-muted-foreground text-sm'>
          {t('Current instance status distribution')}
        </p>
      </CardHeader>
      <CardContent className='grid gap-4 py-4 sm:grid-cols-[180px_1fr] xl:grid-cols-1'>
        <div className='relative mx-auto h-[180px] w-[180px]'>
          <ChartContainer
            config={HEALTH_CHART_CONFIG}
            className='aspect-square h-full w-full'
          >
            <PieChart accessibilityLayer>
              <Pie
                data={data}
                dataKey='value'
                nameKey='label'
                innerRadius={55}
                outerRadius={76}
                paddingAngle={3}
                strokeWidth={0}
              >
                {data.map((item) => (
                  <Cell key={item.key} fill={item.color} />
                ))}
              </Pie>
              <ChartTooltip content={<ChartTooltipContent />} />
            </PieChart>
          </ChartContainer>
          <div className='pointer-events-none absolute inset-0 flex flex-col items-center justify-center'>
            <span className='text-2xl font-semibold tabular-nums'>{total}</span>
            <span className='text-muted-foreground text-xs'>
              {t('Instances')}
            </span>
          </div>
        </div>
        <div className='grid content-center gap-2'>
          {data.map((item) => (
            <div
              key={item.key}
              className='flex items-center justify-between gap-3 text-sm'
            >
              <span className='flex min-w-0 items-center gap-2'>
                <span
                  className='size-2.5 shrink-0 rounded-full'
                  style={{ backgroundColor: item.color }}
                />
                <span className='text-muted-foreground truncate'>
                  {item.label}
                </span>
              </span>
              <span className='font-medium tabular-nums'>{item.value}</span>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function PerformanceTable({
  family,
  rows,
}: {
  family: FleetFamily
  rows: InstanceMetricRow[]
}) {
  const { t } = useTranslation()
  const sortMetric = family === 'new_api' ? 'requests' : 'quota'
  const showsAccountMetrics = family === 'conductor' || family === 'sub2api'
  const sortedRows = [...rows]
    .sort((a, b) => (b[sortMetric] ?? -1) - (a[sortMetric] ?? -1))
    .slice(0, 12)
  return (
    <Card className={PANEL_CARD_CLASS}>
      <CardHeader
        className={cn(
          PANEL_HEADER_CLASS,
          'flex-row items-center justify-between space-y-0'
        )}
      >
        <div>
          <CardTitle>{t('Instance performance')}</CardTitle>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Consumption and collection status by instance')}
          </p>
        </div>
        <Button variant='outline' size='sm' render={<Link to='/instances' />}>
          <Server />
          {t('Manage instances')}
        </Button>
      </CardHeader>
      <CardContent className='px-0'>
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader className='bg-muted/35'>
              <TableRow>
                <TableHead className='ps-6'>{t('Instance')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                {family === 'new_api' && (
                  <TableHead className='text-right'>{t('Requests')}</TableHead>
                )}
                {family !== 'sub2api' && (
                  <TableHead className='text-right'>RPM</TableHead>
                )}
                {family === 'sub2api' && (
                  <TableHead className='text-right'>
                    {t('Concurrency')}
                  </TableHead>
                )}
                {showsAccountMetrics && (
                  <TableHead className='text-right'>
                    {t('Available accounts')}
                  </TableHead>
                )}
                {showsAccountMetrics && (
                  <TableHead className='text-right'>
                    {t('Total accounts')}
                  </TableHead>
                )}
                {family === 'new_api' && (
                  <TableHead className='text-right'>{t('Tokens')}</TableHead>
                )}
                <TableHead className='text-right'>
                  {t(metricLabel('quota', family))}
                </TableHead>
                {family !== 'new_api' && (
                  <TableHead className='text-right'>
                    {t('Today consumption')}
                  </TableHead>
                )}
                <TableHead>{t('Version')}</TableHead>
                <TableHead className='pe-6 text-right'>
                  {t('Last seen')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody className='[&>tr]:h-13'>
              {sortedRows.map((row) => (
                <TableRow key={row.instance.id}>
                  <TableCell className='ps-6'>
                    <div className='min-w-44'>
                      <Link
                        to='/instances/$id'
                        params={{ id: String(row.instance.id) }}
                        className='font-medium hover:underline'
                      >
                        {row.instance.name}
                      </Link>
                      <p className='text-muted-foreground max-w-56 truncate text-xs'>
                        {row.instance.base_url}
                      </p>
                    </div>
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={row.instance.status} />
                  </TableCell>
                  {family === 'new_api' && (
                    <TableCell className='text-right font-mono text-xs tabular-nums'>
                      {formatMetric(row.requests, false)}
                    </TableCell>
                  )}
                  {family !== 'sub2api' && (
                    <TableCell className='text-right font-mono text-xs tabular-nums'>
                      {formatMetric(row.rpm, false)}
                    </TableCell>
                  )}
                  {family === 'sub2api' && (
                    <TableCell className='text-right font-mono text-xs tabular-nums'>
                      {row.concurrencyUsed != null && row.concurrencyMax != null
                        ? `${formatMetric(row.concurrencyUsed, false)} / ${formatMetric(row.concurrencyMax, false)}`
                        : '-- / --'}
                    </TableCell>
                  )}
                  {showsAccountMetrics && (
                    <TableCell className='text-right font-mono text-xs tabular-nums'>
                      {formatMetric(row.accountsAvailable, false)}
                    </TableCell>
                  )}
                  {showsAccountMetrics && (
                    <TableCell className='text-right font-mono text-xs tabular-nums'>
                      {formatMetric(row.accountsTotal, false)}
                    </TableCell>
                  )}
                  {family === 'new_api' && (
                    <TableCell className='text-right font-mono text-xs tabular-nums'>
                      {formatMetric(row.tokens, false)}
                    </TableCell>
                  )}
                  <TableCell className='text-right font-mono text-xs tabular-nums'>
                    {formatUsageMetric('quota', row.quota, family, false)}
                  </TableCell>
                  {family !== 'new_api' && (
                    <TableCell className='text-right font-mono text-xs tabular-nums'>
                      {formatUsageMetric('quota', row.todayCost, family, false)}
                    </TableCell>
                  )}
                  <TableCell className='font-mono text-xs'>
                    {row.instance.version || '--'}
                  </TableCell>
                  <TableCell className='text-muted-foreground pe-6 text-right text-xs tabular-nums'>
                    {row.instance.last_seen_at
                      ? new Date(
                          row.instance.last_seen_at * 1000
                        ).toLocaleString()
                      : '--'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  )
}

type MetricCardTone =
  | 'success'
  | 'blue'
  | 'violet'
  | 'amber'
  | 'danger'
  | 'neutral'

function MetricCard(props: {
  icon: React.ElementType
  label: string
  value: string | number
  detail: string
  tone: MetricCardTone
  action?: ReactNode
  exactValue?: string
  observedAt?: number
  stale?: boolean
}) {
  const { t } = useTranslation()
  const toneClass: Record<MetricCardTone, string> = {
    success: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
    blue: 'bg-sky-500/10 text-sky-600 dark:text-sky-400',
    violet: 'bg-violet-500/10 text-violet-600 dark:text-violet-400',
    amber: 'bg-amber-500/10 text-amber-600 dark:text-amber-400',
    danger: 'bg-red-500/10 text-red-600 dark:text-red-400',
    neutral: 'bg-muted text-muted-foreground',
  }
  return (
    <div className='bg-card min-h-28 min-w-0 px-3 py-3.5 sm:px-4 sm:py-4'>
      <div className='flex min-w-0 items-center justify-between gap-2'>
        <p className='text-muted-foreground truncate text-xs font-medium'>
          {props.label}
        </p>
        <div className='flex shrink-0 items-center gap-1'>
          {props.action}
          <span
            className={cn(
              'flex size-7 shrink-0 items-center justify-center rounded-md',
              toneClass[props.tone]
            )}
          >
            <props.icon className='size-3.5' aria-hidden='true' />
          </span>
        </div>
      </div>
      {props.exactValue ? (
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type='button'
                className='focus-visible:ring-ring mt-2 block max-w-full cursor-help truncate text-left font-mono text-xl font-semibold tracking-tight tabular-nums outline-none focus-visible:ring-2 sm:text-2xl'
              />
            }
          >
            {props.value}
          </TooltipTrigger>
          <TooltipContent className='grid gap-1'>
            <span className='font-mono tabular-nums'>{props.exactValue}</span>
            {props.observedAt ? (
              <span className='text-xs opacity-80'>
                {props.stale
                  ? t('Last successful collection at {{time}}', {
                      time: new Date(props.observedAt * 1000).toLocaleString(),
                    })
                  : t('Collected at {{time}}', {
                      time: new Date(props.observedAt * 1000).toLocaleString(),
                    })}
              </span>
            ) : null}
          </TooltipContent>
        </Tooltip>
      ) : (
        <p className='mt-2 font-mono text-xl font-semibold tracking-tight tabular-nums sm:text-2xl'>
          {props.value}
        </p>
      )}
      <p className='text-muted-foreground mt-1 truncate text-xs'>
        {props.detail}
      </p>
    </div>
  )
}

function SegmentedControl<T extends string>(props: {
  value: T
  options: readonly T[]
  onChange: (value: T) => void
  getLabel?: (value: T) => string
}) {
  return (
    <div className='bg-muted flex h-8 shrink-0 items-center rounded-md p-0.5'>
      {props.options.map((option) => (
        <button
          key={option}
          type='button'
          aria-pressed={props.value === option}
          className={cn(
            'focus-visible:ring-ring h-7 min-w-12 rounded-sm px-2 text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none',
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

function PanelEmpty({
  text,
  compact = false,
}: {
  text: string
  compact?: boolean
}) {
  return (
    <div
      className={cn(
        'text-muted-foreground flex items-center justify-center text-sm',
        compact ? 'min-h-24' : 'min-h-[280px]'
      )}
    >
      {text}
    </div>
  )
}

function EmptyFleet({ family }: { family: FleetFamily }) {
  const { t } = useTranslation()
  return (
    <div className='flex min-h-[60vh] flex-col items-center justify-center px-4 text-center'>
      <span className='bg-muted mb-4 flex size-12 items-center justify-center rounded-lg'>
        <Server className='text-muted-foreground size-6' />
      </span>
      <h2 className='text-lg font-semibold'>{t('No instances yet')}</h2>
      <p className='text-muted-foreground mt-1 max-w-sm text-sm'>
        {t('Add a {{kind}} instance to start collecting fleet data.', {
          kind: t(familyLabel(family)),
        })}
      </p>
      <Button className='mt-4' size='sm' render={<Link to='/instances' />}>
        {t('Go to instance center')}
      </Button>
    </div>
  )
}

function DashboardError({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className='flex min-h-[60vh] flex-col items-center justify-center px-4 text-center'>
      <span className='bg-destructive/10 text-destructive mb-4 flex size-12 items-center justify-center rounded-lg'>
        <ServerOff className='size-6' />
      </span>
      <h2 className='text-lg font-semibold'>
        {t('Could not load fleet data')}
      </h2>
      <p className='text-muted-foreground mt-1 max-w-sm text-sm'>
        {t('Check the control plane connection and try again.')}
      </p>
      <Button className='mt-4' variant='outline' size='sm' onClick={onRetry}>
        <RefreshCw />
        {t('Retry')}
      </Button>
    </div>
  )
}

function DashboardSkeleton() {
  return (
    <div className='grid gap-4'>
      <div className='bg-border border-border/80 grid grid-cols-2 gap-px overflow-hidden rounded-lg border sm:grid-cols-3 xl:grid-cols-6'>
        {KPI_SKELETON_KEYS.map((key) => (
          <Skeleton key={key} className='h-28 rounded-none' />
        ))}
      </div>
      <div className='grid gap-4 xl:grid-cols-[minmax(0,1.7fr)_minmax(300px,0.8fr)]'>
        <Skeleton className='h-[380px] rounded-lg' />
        <Skeleton className='h-[380px] rounded-lg' />
      </div>
      <Skeleton className='h-56 rounded-lg' />
      <Skeleton className='h-72 rounded-lg' />
    </div>
  )
}
