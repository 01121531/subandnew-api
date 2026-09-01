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
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Activity,
  BadgeCheck,
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
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
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
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
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
  getManagedInstances,
  refreshManagedDashboard,
  refreshManagedRealtime,
} from '../managed-instances/api'
import { InstanceConnectionAlert } from '../managed-instances/components/instance-connection-alert'
import { StatusBadge } from '../managed-instances/components/status-badge'
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
type TrendMetricKey = MetricKey | 'rpm' | 'success_rate' | 'accounts'
type FleetFamily = 'new_api' | 'sub2api' | 'conductor' | 'claude_gateway'

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
  successRate: number | null
  successRateSampleCount: number
  concurrencyUsed: number | null
  concurrencyMax: number | null
  accountsAvailable: number | null
  accountsTotal: number | null
  todayCost: number | null
  todayCostStale: boolean
  cost7D: number | null
  cost7DObservedAt: number
  cost7DStale: boolean
  cost30D: number | null
  cost30DObservedAt: number
  cost30DStale: boolean
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
  success_rate: number | null
  success_rate_samples: number
  accounts_available: number | null
  accounts_total: number | null
  account_samples: number
}

type HealthData = {
  key: string
  label: string
  color: string
  value: number
}

const RPM_HISTORY_REFRESH_MS = 10_000
const DASHBOARD_ERROR_RETRY_MS = 15_000
const DASHBOARD_RETRY_COUNT = 3
const FLEET_FAMILIES: readonly FleetFamily[] = [
  'new_api',
  'sub2api',
  'conductor',
  'claude_gateway',
]
const ALL_SITES_VALUE = 'all'
const DASHBOARD_PREFERENCES_KEY = 'fleet-dashboard-preferences-v1'
const PANEL_CARD_CLASS = 'min-w-0 gap-0 rounded-lg py-0 shadow-xs'
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

const SUCCESS_RATE_CHART_CONFIG = {
  success_rate: { label: '成功率', color: 'var(--chart-2)' },
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
      claude_gateway: ALL_SITES_VALUE,
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
  return (
    isMetricKey(value) ||
    value === 'rpm' ||
    value === 'success_rate' ||
    value === 'accounts'
  )
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
        claude_gateway:
          typeof parsed.selectedInstances?.claude_gateway === 'string'
            ? parsed.selectedInstances.claude_gateway
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
  if (family === 'claude_gateway') return 'Claude Gateway'
  return 'New API'
}

function belongsToFamily(instance: ManagedInstance, family: FleetFamily) {
  if (family === 'sub2api') return instance.kind === 'sub2api'
  if (family === 'conductor') return instance.kind === 'conductor'
  if (family === 'claude_gateway') return instance.kind === 'claude_gateway'
  return instance.kind === 'new_api' || instance.kind === 'huichuan'
}

function instanceFamily(instance: ManagedInstance): FleetFamily | null {
  if (instance.kind === 'sub2api') return 'sub2api'
  if (instance.kind === 'conductor') return 'conductor'
  if (instance.kind === 'claude_gateway') return 'claude_gateway'
  if (instance.kind === 'new_api' || instance.kind === 'huichuan') {
    return 'new_api'
  }
  return null
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
  const [manualRPMRefreshing, setManualRPMRefreshing] = useState(false)
  let effectiveTrendMetric = trendMetric
  if (
    family === 'claude_gateway' &&
    trendMetric !== 'rpm' &&
    trendMetric !== 'success_rate' &&
    trendMetric !== 'accounts'
  ) {
    effectiveTrendMetric = 'rpm'
  } else if (
    family === 'conductor' &&
    (trendMetric === 'requests' ||
      trendMetric === 'tokens' ||
      trendMetric === 'success_rate' ||
      trendMetric === 'accounts')
  ) {
    effectiveTrendMetric = 'quota'
  } else if (
    family !== 'claude_gateway' &&
    (trendMetric === 'success_rate' || trendMetric === 'accounts')
  ) {
    effectiveTrendMetric = 'rpm'
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
  const supportedInstances = useMemo(
    () => allInstances.filter((instance) => instanceFamily(instance) != null),
    [allInstances]
  )
  const familyInstances = useMemo(
    () =>
      supportedInstances.filter((instance) =>
        belongsToFamily(instance, family)
      ),
    [family, supportedInstances]
  )
  const selectedInstanceID = selectedInstances[family]
  const selectedInstance = useMemo(
    () =>
      familyInstances.find(
        (instance) => String(instance.id) === selectedInstanceID
      ),
    [familyInstances, selectedInstanceID]
  )
  const instances = useMemo(
    () => (selectedInstance ? [selectedInstance] : []),
    [selectedInstance]
  )
  const instanceIDs = useMemo(
    () => instances.map((instance) => instance.id),
    [instances]
  )
  const rpmHistoryInstanceIDs = useMemo(
    () => instances.map((instance) => instance.id),
    [instances]
  )
  const realtimeEvents = useManagedInstanceRealtimeEvents(instanceIDs, [
    'rpm',
    'accounts',
    'status',
  ])
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
      (effectiveTrendMetric === 'rpm' ||
        effectiveTrendMetric === 'success_rate' ||
        effectiveTrendMetric === 'accounts') &&
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
  const rows = useMemo<InstanceMetricRow[]>(
    () =>
      instances.map((instance) => {
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
        const realtime = realtimeEvents.states[instance.id]
        const accountsReady =
          instance.kind === 'conductor' || instance.kind === 'claude_gateway'
            ? Boolean(realtime?.observed_at)
            : instance.kind === 'sub2api' &&
              realtime?.accounts_collection_status === 'succeeded'
        const usesRealtimeCosts = instance.kind === 'claude_gateway'
        const todayCost = metricValue(
          usesRealtimeCosts
            ? realtime?.today_cost
            : todaySection?.observation?.data?.cost
        )
        return {
          instance,
          summary,
          observedAt: Math.max(
            observation?.observed_at ?? 0,
            todaySection?.observation?.observed_at ?? 0,
            realtime?.observed_at ?? 0
          ),
          summaryObservedAt: observation?.observed_at ?? 0,
          todayObservedAt: usesRealtimeCosts
            ? (realtime?.today_cost_observed_at ?? 0)
            : (todaySection?.observation?.observed_at ?? 0),
          collected: observation?.collection_status === 'succeeded',
          requests: metricValue(summary?.requests),
          rpm: metricValue(realtime?.rpm),
          rpmCapacity: metricValue(realtime?.rpm_capacity),
          successRate: metricValue(realtime?.success_rate),
          successRateSampleCount: realtime?.success_rate_sample_count ?? 0,
          concurrencyUsed: metricValue(realtime?.concurrency_used),
          concurrencyMax: metricValue(realtime?.concurrency_max),
          accountsAvailable: accountsReady
            ? (realtime?.accounts_available ?? 0)
            : null,
          accountsTotal: accountsReady ? (realtime?.accounts_total ?? 0) : null,
          todayCost,
          todayCostStale: usesRealtimeCosts
            ? Boolean(realtime?.today_cost_stale || realtime?.stale)
            : Boolean(todaySection?.stale),
          cost7D: usesRealtimeCosts ? metricValue(realtime?.cost_7d) : null,
          cost7DObservedAt: usesRealtimeCosts
            ? (realtime?.cost_7d_observed_at ?? 0)
            : 0,
          cost7DStale: usesRealtimeCosts
            ? Boolean(realtime?.cost_7d_stale || realtime?.stale)
            : false,
          cost30D: usesRealtimeCosts ? metricValue(realtime?.cost_30d) : null,
          cost30DObservedAt: usesRealtimeCosts
            ? (realtime?.cost_30d_observed_at ?? 0)
            : 0,
          cost30DStale: usesRealtimeCosts
            ? Boolean(realtime?.cost_30d_stale || realtime?.stale)
            : false,
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
          stale: Boolean(
            summarySection?.stale ||
            todaySection?.stale ||
            (instance.kind === 'claude_gateway' && realtime?.stale)
          ),
        }
      }),
    [
      dashboardEvents.snapshots,
      dashboardQuery.data,
      instances,
      realtimeEvents.states,
    ]
  )

  const totals = useMemo(() => {
    const successRows = rows.filter((row) => row.successRate != null)
    const successRateSampleCount = successRows.reduce(
      (sum, row) => sum + row.successRateSampleCount,
      0
    )
    let successRate = 0
    if (successRateSampleCount > 0) {
      successRate =
        successRows.reduce(
          (sum, row) =>
            sum + (row.successRate ?? 0) * row.successRateSampleCount,
          0
        ) / successRateSampleCount
    } else if (successRows.length > 0) {
      successRate =
        successRows.reduce((sum, row) => sum + (row.successRate ?? 0), 0) /
        successRows.length
    }
    return {
      requests: rows.reduce((sum, row) => sum + (row.requests ?? 0), 0),
      rpm: rows.reduce((sum, row) => sum + (row.rpm ?? 0), 0),
      rpmCapacity: rows.reduce((sum, row) => sum + (row.rpmCapacity ?? 0), 0),
      successRate,
      successRateSampleCount,
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
      cost7D: rows.reduce((sum, row) => sum + (row.cost7D ?? 0), 0),
      cost30D: rows.reduce((sum, row) => sum + (row.cost30D ?? 0), 0),
      tokens: rows.reduce((sum, row) => sum + (row.tokens ?? 0), 0),
      quota: rows.reduce((sum, row) => sum + (row.quota ?? 0), 0),
      collected: rows.filter((row) => row.collected).length,
      metricReady: rows.filter(
        (row) => row.requests != null || row.tokens != null || row.quota != null
      ).length,
      requestsReady: rows.filter((row) => row.requests != null).length,
      rpmReady: rows.filter((row) => row.rpm != null).length,
      successRateReady: successRows.length,
      rpmCapacityReady: rows.filter((row) => row.rpmCapacity != null).length,
      concurrencyReady: rows.filter(
        (row) => row.concurrencyUsed != null && row.concurrencyMax != null
      ).length,
      accountsReady: rows.filter((row) => row.accountsTotal != null).length,
      todayCostReady: rows.filter((row) => row.todayCost != null).length,
      cost7DReady: rows.filter((row) => row.cost7D != null).length,
      cost30DReady: rows.filter((row) => row.cost30D != null).length,
      tokensReady: rows.filter((row) => row.tokens != null).length,
      quotaReady: rows.filter((row) => row.quota != null).length,
      healthy: instances.filter((item) => item.status === 'healthy').length,
    }
  }, [instances, rows])
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
    if (
      effectiveTrendMetric === 'rpm' ||
      effectiveTrendMetric === 'success_rate' ||
      effectiveTrendMetric === 'accounts'
    ) {
      return []
    }
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
    rows.some((row) => row.lastAttemptStatus === 'failed')
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
      claude_gateway: allInstances.filter((instance) =>
        belongsToFamily(instance, 'claude_gateway')
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
      familyInstances.some(
        (instance) => String(instance.id) === selectedInstanceID
      )
    ) {
      return
    }
    const fallback = familyInstances[0]
    if (!fallback) return
    setSelectedInstances((current) => ({
      ...current,
      [family]: String(fallback.id),
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
      await Promise.all([
        refreshManagedDashboard(instanceIDs, dashboardRangeInput),
        refreshManagedRealtime(instanceIDs),
      ])
    } catch {
      setRefreshRequestedAt(0)
    }
    if (
      effectiveTrendMetric === 'rpm' ||
      effectiveTrendMetric === 'success_rate' ||
      effectiveTrendMetric === 'accounts'
    ) {
      void rpmHistoryQuery.refetch()
    }
    dashboardEvents.reconnect()
    realtimeEvents.reconnect()
  }

  const refreshRPM = async () => {
    setManualRPMRefreshing(true)
    try {
      await refreshManagedRealtime(instanceIDs)
    } finally {
      realtimeEvents.reconnect()
      void rpmHistoryQuery.refetch()
      setManualRPMRefreshing(false)
    }
  }
  const rpmRefreshing =
    manualRPMRefreshing ||
    ['connecting', 'reconnecting'].includes(realtimeEvents.status)

  const handleInstanceChange = (instanceID: string) => {
    const instance = supportedInstances.find(
      (candidate) => String(candidate.id) === instanceID
    )
    if (!instance) return
    const nextFamily = instanceFamily(instance)
    if (!nextFamily) return
    setFamily(nextFamily)
    setSelectedInstances((current) => ({
      ...current,
      [nextFamily]: instanceID,
    }))
  }

  let content: ReactNode
  if (instancesQuery.isLoading) {
    content = <DashboardSkeleton />
  } else if (instancesQuery.isError && !instancesQuery.data) {
    content = <DashboardError onRetry={refresh} />
  } else if (supportedInstances.length === 0) {
    content = <EmptyFleet family={family} />
  } else if (!selectedInstance) {
    content = <DashboardSkeleton />
  } else {
    content = (
      <div className='grid min-w-0 grid-cols-[minmax(0,1fr)] gap-4'>
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
      <SectionPageLayout.Actions className='max-md:w-full'>
        <div className='flex w-full min-w-0 items-center gap-2 md:w-auto'>
          <FleetTimeRangeFilter value={timeRange} onChange={setTimeRange} />
          <Button
            variant='outline'
            size='icon-sm'
            className='size-11 shrink-0 md:size-8'
            aria-label={t('Refresh')}
            onClick={refresh}
          >
            <RefreshCw className={isRefreshing ? 'animate-spin' : ''} />
          </Button>
        </div>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='grid min-w-0 grid-cols-[minmax(0,1fr)] gap-4 overflow-x-hidden'>
          <div className='bg-card border-border/80 flex flex-wrap items-center justify-between gap-2 rounded-lg border p-2 shadow-xs sm:gap-3 sm:px-3'>
            <div className='w-full min-w-0 md:flex-1'>
              <div className='md:hidden'>
                <InstanceSelect
                  instances={supportedInstances}
                  value={selectedInstance ? String(selectedInstance.id) : ''}
                  onChange={handleInstanceChange}
                />
              </div>
              <div className='hidden md:block'>
                <InstanceTabs
                  instances={supportedInstances}
                  value={selectedInstance ? String(selectedInstance.id) : ''}
                  onChange={handleInstanceChange}
                />
              </div>
            </div>
            <div className='flex w-full min-w-0 items-center gap-2 text-xs md:w-auto'>
              <span className='border-success/20 bg-success/5 text-success flex min-h-9 min-w-0 flex-1 items-center gap-1.5 rounded-md border px-2 py-1 font-medium md:min-h-0 md:flex-none'>
                <Radio
                  className={cn('size-3.5', isRefreshing && 'animate-pulse')}
                />
                <span className='min-w-0 break-words'>
                  {t('Auto-refreshing every {{seconds}}s', {
                    seconds: 60,
                  })}
                </span>
              </span>
              {lastObservedAt > 0 && (
                <span className='text-muted-foreground hidden tabular-nums md:inline'>
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
    successRate: number
    successRateSampleCount: number
    concurrencyUsed: number
    concurrencyMax: number
    accountsAvailable: number
    accountsTotal: number
    todayCost: number
    cost7D: number
    cost30D: number
    tokens: number
    quota: number
    collected: number
    metricReady: number
    requestsReady: number
    rpmReady: number
    successRateReady: number
    rpmCapacityReady: number
    concurrencyReady: number
    accountsReady: number
    todayCostReady: number
    cost7DReady: number
    cost30DReady: number
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
    <div className='grid min-w-0 grid-cols-[minmax(0,1fr)] gap-4 pb-6'>
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
  const isRPM = props.metric === 'rpm'
  const isSuccessRate = props.metric === 'success_rate'
  const isAccounts = props.metric === 'accounts'
  const isRealtimeHistory = isRPM || isSuccessRate || isAccounts
  const usageMetric: MetricKey | null = isRealtimeHistory
    ? null
    : (props.metric as MetricKey)
  let historyData = props.rpmHistoryData
  if (isSuccessRate) {
    historyData = props.rpmHistoryData.filter(
      (point) => point.success_rate != null
    )
  } else if (isAccounts) {
    historyData = props.rpmHistoryData.filter(
      (point) =>
        point.account_samples > 0 &&
        point.accounts_available != null &&
        point.accounts_total != null
    )
  }
  const accountChartConfig = useMemo(
    () =>
      ({
        accounts_available: {
          label: t('Available accounts'),
          color: 'var(--color-success)',
        },
        accounts_total: {
          label: t('Total accounts'),
          color: 'var(--chart-1)',
        },
      }) satisfies ChartConfig,
    [t]
  )
  let realtimeChartConfig: ChartConfig = RPM_CHART_CONFIG
  if (isSuccessRate) {
    realtimeChartConfig = SUCCESS_RATE_CHART_CONFIG
  } else if (isAccounts) {
    realtimeChartConfig = accountChartConfig
  }
  let subtitle = t('Daily totals in the selected period')
  if (isRPM) {
    subtitle =
      props.rpmHistoryBucket === 'minute'
        ? t('Average RPM per minute over the last 60 minutes')
        : t('Average RPM per hour over the last 24 hours')
  } else if (isSuccessRate) {
    subtitle =
      props.rpmHistoryBucket === 'minute'
        ? t('Average success rate snapshot per minute over the last 60 minutes')
        : t('Average success rate snapshot per hour over the last 24 hours')
  } else if (isAccounts) {
    subtitle =
      props.rpmHistoryBucket === 'minute'
        ? t('Last account count per minute over the last 60 minutes')
        : t('Last account count per hour over the last 24 hours')
  }
  let metricOptions: TrendMetricKey[] = ['requests', 'tokens', 'quota']
  if (props.family === 'conductor') {
    metricOptions = ['quota', 'rpm']
  } else if (props.family === 'claude_gateway') {
    metricOptions = ['rpm', 'success_rate', 'accounts']
  } else {
    metricOptions.push('rpm')
  }
  const trendMetricLabel = (value: TrendMetricKey) => {
    if (value === 'rpm') return 'RPM'
    if (value === 'success_rate') return t('Success rate')
    if (value === 'accounts') return t('Account count')
    return t(metricLabel(value, props.family))
  }
  let panelTitle = t('Daily usage trend')
  if (isSuccessRate) {
    panelTitle = t('Success rate trend')
  } else if (isAccounts) {
    panelTitle = t('Account count trend')
  }
  let historyEmptyMessage = 'RPM history is being collected'
  if (isSuccessRate) {
    historyEmptyMessage = 'Success rate history is being collected'
  } else if (isAccounts) {
    historyEmptyMessage = 'Account count history is being collected'
  }
  return (
    <Card className={PANEL_CARD_CLASS}>
      <CardHeader
        className={cn(
          PANEL_HEADER_CLASS,
          'flex flex-col items-stretch justify-between gap-3 space-y-0 md:flex-row md:items-start md:gap-4'
        )}
      >
        <div className='min-w-0'>
          <CardTitle>{panelTitle}</CardTitle>
          <p className='text-muted-foreground mt-1 text-sm break-words'>
            {subtitle}
          </p>
        </div>
        <div className='grid w-full gap-2 md:flex md:w-auto md:flex-wrap md:items-center md:justify-end'>
          <NativeSelect
            className='w-full md:hidden [&_select]:h-11'
            name='dashboard-trend-metric'
            value={props.metric}
            aria-label={panelTitle}
            onChange={(event) =>
              props.onMetricChange(event.target.value as TrendMetricKey)
            }
          >
            {metricOptions.map((option) => (
              <NativeSelectOption key={option} value={option}>
                {trendMetricLabel(option)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <div className='hidden md:block'>
            <SegmentedControl
              value={props.metric}
              options={metricOptions}
              getLabel={trendMetricLabel}
              onChange={props.onMetricChange}
            />
          </div>
          {isRealtimeHistory && (
            <>
              <NativeSelect
                className='w-full md:hidden [&_select]:h-11'
                name='dashboard-history-bucket'
                value={props.rpmHistoryBucket}
                aria-label={t('Time Range')}
                onChange={(event) =>
                  props.onRPMHistoryBucketChange(
                    event.target.value as ManagedInstanceRPMHistoryBucket
                  )
                }
              >
                {(['minute', 'hour'] as const).map((option) => (
                  <NativeSelectOption key={option} value={option}>
                    {t(option === 'minute' ? 'By minute' : 'By hour')}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
              <div className='hidden md:block'>
                <SegmentedControl
                  value={props.rpmHistoryBucket}
                  options={['minute', 'hour']}
                  getLabel={(value) =>
                    t(value === 'minute' ? 'By minute' : 'By hour')
                  }
                  onChange={props.onRPMHistoryBucketChange}
                />
              </div>
            </>
          )}
        </div>
      </CardHeader>
      <CardContent className='px-2 py-4 sm:px-6'>
        {isRealtimeHistory &&
          props.rpmHistoryLoading &&
          historyData.length === 0 && (
            <div className='text-muted-foreground flex min-h-[280px] items-center justify-center gap-2 text-sm'>
              <RefreshCw className='size-4 animate-spin' aria-hidden='true' />
              <span>{t('Data loading')}</span>
            </div>
          )}
        {isRealtimeHistory && historyData.length > 0 && (
          <ChartContainer
            config={realtimeChartConfig}
            className='aspect-auto h-[240px] w-full min-[420px]:h-[260px] sm:h-[280px] md:h-[300px]'
          >
            <LineChart
              accessibilityLayer
              data={historyData}
              margin={{ top: 10, right: 8, left: -6, bottom: 4 }}
            >
              <CartesianGrid vertical={false} strokeDasharray='3 3' />
              <XAxis
                dataKey='timestamp'
                axisLine={false}
                tickLine={false}
                tickMargin={10}
                minTickGap={44}
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
                width={48}
                domain={[0, 'auto']}
                tickFormatter={(value: number) =>
                  isSuccessRate
                    ? `${(value * 100).toFixed(0)}%`
                    : formatMetric(value, false)
                }
              />
              <ChartTooltip
                cursor={{ stroke: 'var(--color-border)' }}
                content={
                  <ChartTooltipContent
                    className='max-w-[calc(100vw-2rem)]'
                    indicator='dot'
                    labelFormatter={(_, payload) =>
                      formatRPMTimestamp(
                        payload?.[0]?.payload?.timestamp,
                        rpmTooltipTime
                      )
                    }
                    formatter={(value, name, item) => {
                      const point = item.payload as RPMHistoryData
                      if (isSuccessRate) {
                        return (
                          <div className='flex w-full min-w-0 items-center gap-2'>
                            <span
                              className='size-2 shrink-0 rounded-sm'
                              style={{ backgroundColor: item.color }}
                            />
                            <span className='text-muted-foreground flex-1'>
                              {t('Success rate')}
                            </span>
                            <span className='font-mono font-medium tabular-nums'>
                              {(Number(value) * 100).toFixed(2)}%
                            </span>
                          </div>
                        )
                      }
                      if (isAccounts) {
                        return (
                          <div className='flex w-full min-w-0 items-center gap-2'>
                            <span
                              className='size-2 shrink-0 rounded-sm'
                              style={{ backgroundColor: item.color }}
                            />
                            <span className='text-muted-foreground flex-1'>
                              {String(name)}
                            </span>
                            <span className='font-mono font-medium tabular-nums'>
                              {exactNumber.format(Number(value))}
                            </span>
                          </div>
                        )
                      }
                      const utilization =
                        point.capacity != null && point.capacity > 0
                          ? (point.rpm / point.capacity) * 100
                          : null
                      return (
                        <div className='flex w-full min-w-0 items-center gap-2'>
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
              {isSuccessRate && (
                <Line
                  type='monotone'
                  dataKey='success_rate'
                  name={t('Success rate')}
                  stroke='var(--color-success_rate)'
                  strokeWidth={2.25}
                  dot={historyData.length <= 24}
                  activeDot={{ r: 4 }}
                />
              )}
              {isAccounts && (
                <>
                  <Line
                    type='stepAfter'
                    dataKey='accounts_available'
                    name={t('Available accounts')}
                    stroke='var(--color-accounts_available)'
                    strokeWidth={2.25}
                    dot={historyData.length <= 24}
                    activeDot={{ r: 4 }}
                    connectNulls={false}
                  />
                  <Line
                    type='stepAfter'
                    dataKey='accounts_total'
                    name={t('Total accounts')}
                    stroke='var(--color-accounts_total)'
                    strokeWidth={2.25}
                    dot={historyData.length <= 24}
                    activeDot={{ r: 4 }}
                    connectNulls={false}
                  />
                </>
              )}
              {isRPM && (
                <Line
                  type='monotone'
                  dataKey='rpm'
                  name='实时 RPM'
                  stroke='var(--color-rpm)'
                  strokeWidth={2.25}
                  dot={historyData.length <= 24}
                  activeDot={{ r: 4 }}
                />
              )}
              {isRPM && props.family === 'conductor' && (
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
        {isRealtimeHistory &&
          historyData.length === 0 &&
          !props.rpmHistoryLoading &&
          props.rpmHistoryError && <PanelEmpty text={t('Failed to load')} />}
        {isRealtimeHistory &&
          historyData.length === 0 &&
          !props.rpmHistoryLoading &&
          !props.rpmHistoryError && (
            <PanelEmpty text={t(historyEmptyMessage)} />
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
            className='aspect-auto h-[240px] w-full min-[420px]:h-[260px] sm:h-[280px] md:h-[300px]'
          >
            <LineChart
              accessibilityLayer
              data={props.data}
              margin={{ top: 10, right: 8, left: -6, bottom: 4 }}
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
                width={48}
                tickFormatter={(value: number) =>
                  formatUsageMetric(usageMetric, value, props.family)
                }
              />
              <ChartTooltip
                cursor={{ stroke: 'var(--color-border)' }}
                content={
                  <ChartTooltipContent
                    className='max-w-[calc(100vw-2rem)]'
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
    seconds: 10,
  })}`
  const capacityReady =
    props.family === 'conductor' &&
    props.instances.length > 0 &&
    props.totals.rpmCapacityReady === props.instances.length
  const capacityValue = capacityReady
    ? formatMetric(props.totals.rpmCapacity)
    : '--'
  const rpmValue = props.totals.rpmReady
    ? `${formatMetric(props.totals.rpm)} / ${capacityValue}`
    : `-- / ${capacityValue}`
  const capacityUsage =
    capacityReady && props.totals.rpmCapacity > 0
      ? ((props.totals.rpm / props.totals.rpmCapacity) * 100).toFixed(1)
      : '0.0'
  const rpmCapacityDetail = capacityReady
    ? `${capacityUsage}% · ${formatMetric(props.totals.accountsAvailable, false)} 个可用账号`
    : '最大容量数据加载中'
  const showsAccountMetrics =
    props.family === 'conductor' ||
    props.family === 'sub2api' ||
    props.family === 'claude_gateway'
  const costObservedAt = Math.max(
    0,
    ...props.rows.map((row) => row.summaryObservedAt)
  )
  const todayObservedAt = Math.max(
    0,
    ...props.rows.map((row) => row.todayObservedAt)
  )
  const cost7DObservedAt = Math.max(
    0,
    ...props.rows.map((row) => row.cost7DObservedAt)
  )
  const cost30DObservedAt = Math.max(
    0,
    ...props.rows.map((row) => row.cost30DObservedAt)
  )
  const amountStale = props.rows.some((row) => row.stale)
  const todayCostStale = props.rows.some(
    (row) => row.todayCost != null && row.todayCostStale
  )
  const cost7DStale = props.rows.some(
    (row) => row.cost7D != null && row.cost7DStale
  )
  const cost30DStale = props.rows.some(
    (row) => row.cost30D != null && row.cost30DStale
  )
  const successRateReady = props.totals.successRateReady > 0
  let successRateTone: MetricCardTone = 'neutral'
  if (successRateReady) {
    if (props.totals.successRate >= 0.95) successRateTone = 'success'
    else if (props.totals.successRate >= 0.8) successRateTone = 'amber'
    else successRateTone = 'danger'
  }
  const summaryPending = props.rows.some(
    (row) => !row.collected && row.lastAttemptStatus !== 'failed'
  )
  let resolvedCostDetail = t('No metric data available')
  if (props.totals.quotaReady) resolvedCostDetail = costDetail
  else if (summaryPending) resolvedCostDetail = t('Data loading')
  let todayCostDetail = t('No metric data available')
  if (props.totals.todayCostReady) {
    todayCostDetail =
      props.family === 'claude_gateway' && todayCostStale
        ? t('Refresh failed; showing the last successful data')
        : t('Across {{count}} instances', {
            count: props.totals.todayCostReady,
          })
  } else if (props.family === 'claude_gateway') {
    todayCostDetail = t('Real-time data is connecting')
  } else if (summaryPending) {
    todayCostDetail = t('Data loading')
  }
  let cost7DDetail = t('Real-time data is connecting')
  if (cost7DStale) {
    cost7DDetail = t('Refresh failed; showing the last successful data')
  } else if (props.totals.cost7DReady) {
    cost7DDetail = t('Across {{count}} instances', {
      count: props.totals.cost7DReady,
    })
  }
  let cost30DDetail = t('Real-time data is connecting')
  if (cost30DStale) {
    cost30DDetail = t('Refresh failed; showing the last successful data')
  } else if (props.totals.cost30DReady) {
    cost30DDetail = t('Across {{count}} instances', {
      count: props.totals.cost30DReady,
    })
  }

  return (
    <section
      aria-label={t('Fleet summary')}
      className={cn(
        'bg-border border-border/80 grid grid-cols-1 gap-px overflow-hidden rounded-lg border shadow-xs min-[420px]:grid-cols-2 md:grid-cols-3',
        props.family === 'conductor' && 'xl:grid-cols-7',
        props.family === 'sub2api' && 'xl:grid-cols-4 2xl:grid-cols-8',
        props.family === 'claude_gateway' && 'xl:grid-cols-5 2xl:grid-cols-9',
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
      {props.family === 'sub2api' && (
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
              className='size-11 md:size-6'
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
            className='size-11 md:size-6'
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
      {props.family === 'claude_gateway' && (
        <MetricCard
          icon={BadgeCheck}
          label={t('Success rate')}
          value={
            successRateReady
              ? `${(props.totals.successRate * 100).toFixed(2)}%`
              : '--'
          }
          detail={
            successRateReady
              ? t('{{count}} requests sampled', {
                  count: formatMetric(
                    props.totals.successRateSampleCount,
                    false
                  ),
                })
              : t('Real-time data is connecting')
          }
          tone={successRateTone}
          observedAt={Math.max(0, ...props.rows.map((row) => row.observedAt))}
          stale={props.rows.some((row) => row.successRate != null && row.stale)}
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
      {props.family !== 'claude_gateway' && (
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
      )}
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
          stale={
            props.family === 'claude_gateway' ? todayCostStale : amountStale
          }
        />
      )}
      {props.family === 'claude_gateway' && (
        <MetricCard
          icon={CircleDollarSign}
          label={t('7-day actual consumption')}
          value={formatUsageMetric(
            'quota',
            props.totals.cost7DReady ? props.totals.cost7D : null,
            props.family
          )}
          detail={cost7DDetail}
          tone='blue'
          exactValue={
            props.totals.cost7DReady
              ? `US$${exactDecimal.format(props.totals.cost7D)}`
              : undefined
          }
          observedAt={cost7DObservedAt}
          stale={cost7DStale}
        />
      )}
      {props.family === 'claude_gateway' && (
        <MetricCard
          icon={CircleDollarSign}
          label={t('30-day actual consumption')}
          value={formatUsageMetric(
            'quota',
            props.totals.cost30DReady ? props.totals.cost30D : null,
            props.family
          )}
          detail={cost30DDetail}
          tone='amber'
          exactValue={
            props.totals.cost30DReady
              ? `US$${exactDecimal.format(props.totals.cost30D)}`
              : undefined
          }
          observedAt={cost30DObservedAt}
          stale={cost30DStale}
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
          'flex flex-col items-stretch justify-between gap-3 space-y-0 md:flex-row md:items-start md:gap-4'
        )}
      >
        <div className='min-w-0'>
          <CardTitle>{t('Instance consumption')}</CardTitle>
          <p className='text-muted-foreground mt-1 text-sm break-words'>
            {t('Top instances in the selected period')}
          </p>
        </div>
        {props.family !== 'conductor' && (
          <>
            <NativeSelect
              className='w-full md:hidden [&_select]:h-11'
              name='dashboard-consumption-metric'
              value={props.metric}
              aria-label={t('Instance consumption')}
              onChange={(event) =>
                props.onMetricChange(event.target.value as MetricKey)
              }
            >
              {(['requests', 'tokens', 'quota'] as const).map((option) => (
                <NativeSelectOption key={option} value={option}>
                  {t(metricLabel(option, props.family))}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <div className='hidden md:block'>
              <SegmentedControl
                value={props.metric}
                options={['requests', 'tokens', 'quota']}
                getLabel={(value) => t(metricLabel(value, props.family))}
                onChange={props.onMetricChange}
              />
            </div>
          </>
        )}
      </CardHeader>
      <CardContent className='px-2 py-4 sm:px-6'>
        {props.data.length ? (
          <ChartContainer
            config={CONSUMPTION_CHART_CONFIG}
            className='aspect-auto h-[240px] w-full min-[420px]:h-[260px] sm:h-[280px]'
          >
            <BarChart
              accessibilityLayer
              data={props.data}
              margin={{ top: 8, right: 8, left: -6, bottom: 4 }}
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
                width={48}
                tickFormatter={(value: number) =>
                  formatUsageMetric(props.metric, value, props.family)
                }
              />
              <ChartTooltip
                cursor={{ fill: 'var(--color-muted)', opacity: 0.45 }}
                content={
                  <ChartTooltipContent
                    className='max-w-[calc(100vw-2rem)]'
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
        <div className='relative mx-auto size-40 sm:size-[180px]'>
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
                <span className='text-muted-foreground break-words'>
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
  const showsAccountMetrics =
    family === 'conductor' ||
    family === 'sub2api' ||
    family === 'claude_gateway'
  const sortedRows = [...rows]
    .sort((a, b) => (b[sortMetric] ?? -1) - (a[sortMetric] ?? -1))
    .slice(0, 12)
  return (
    <Card className={PANEL_CARD_CLASS}>
      <CardHeader
        className={cn(
          PANEL_HEADER_CLASS,
          'flex flex-col items-stretch justify-between gap-3 space-y-0 sm:flex-row sm:items-center'
        )}
      >
        <div className='min-w-0'>
          <CardTitle>{t('Instance performance')}</CardTitle>
          <p className='text-muted-foreground mt-1 text-sm break-words'>
            {t('Consumption and collection status by instance')}
          </p>
        </div>
        <Button
          variant='outline'
          size='sm'
          className='h-11 w-full sm:h-8 sm:w-auto'
          render={<Link to='/instances' />}
        >
          <Server />
          {t('Manage instances')}
        </Button>
      </CardHeader>
      <CardContent className='px-0'>
        <MobilePerformanceList
          family={family}
          rows={sortedRows}
          className='md:hidden'
        />
        <div className='hidden overflow-x-auto md:block'>
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
                {family === 'claude_gateway' && (
                  <TableHead className='text-right'>
                    {t('Success rate')}
                  </TableHead>
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
                  {family === 'claude_gateway' && (
                    <TableCell className='text-right font-mono text-xs tabular-nums'>
                      {row.successRate == null
                        ? '--'
                        : `${(row.successRate * 100).toFixed(2)}%`}
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

function MobilePerformanceList(props: {
  family: FleetFamily
  rows: InstanceMetricRow[]
  className?: string
}) {
  const { t } = useTranslation()
  const showsAccountMetrics =
    props.family === 'conductor' ||
    props.family === 'sub2api' ||
    props.family === 'claude_gateway'

  const summaryMetrics = (row: InstanceMetricRow) => {
    if (props.family === 'new_api') {
      return [
        [t('Requests'), formatMetric(row.requests, false)],
        ['RPM', formatMetric(row.rpm, false)],
      ]
    }
    if (props.family === 'sub2api') {
      return [
        [
          t('Concurrency'),
          row.concurrencyUsed != null && row.concurrencyMax != null
            ? `${formatMetric(row.concurrencyUsed, false)} / ${formatMetric(row.concurrencyMax, false)}`
            : '-- / --',
        ],
        [
          t('Today consumption'),
          formatUsageMetric('quota', row.todayCost, props.family, false),
        ],
      ]
    }
    if (props.family === 'claude_gateway') {
      return [
        ['RPM', formatMetric(row.rpm, false)],
        [
          t('Success rate'),
          row.successRate == null
            ? '--'
            : `${(row.successRate * 100).toFixed(2)}%`,
        ],
      ]
    }
    return [
      ['RPM', formatMetric(row.rpm, false)],
      [t('Available accounts'), formatMetric(row.accountsAvailable, false)],
    ]
  }

  const detailMetrics = (row: InstanceMetricRow) => {
    const details: { label: string; value: string }[] = []
    if (props.family === 'new_api') {
      details.push(
        { label: t('Requests'), value: formatMetric(row.requests, false) },
        { label: t('Tokens'), value: formatMetric(row.tokens, false) }
      )
    }
    if (props.family === 'sub2api') {
      details.push({
        label: t('Concurrency'),
        value:
          row.concurrencyUsed != null && row.concurrencyMax != null
            ? `${formatMetric(row.concurrencyUsed, false)} / ${formatMetric(row.concurrencyMax, false)}`
            : '-- / --',
      })
    }
    if (props.family !== 'sub2api') {
      details.push({ label: 'RPM', value: formatMetric(row.rpm, false) })
    }
    if (props.family === 'claude_gateway') {
      details.push({
        label: t('Success rate'),
        value:
          row.successRate == null
            ? '--'
            : `${(row.successRate * 100).toFixed(2)}%`,
      })
    }
    if (showsAccountMetrics) {
      details.push(
        {
          label: t('Available accounts'),
          value: formatMetric(row.accountsAvailable, false),
        },
        {
          label: t('Total accounts'),
          value: formatMetric(row.accountsTotal, false),
        }
      )
    }
    details.push({
      label: t(metricLabel('quota', props.family)),
      value: formatUsageMetric('quota', row.quota, props.family, false),
    })
    if (props.family !== 'new_api') {
      details.push({
        label: t('Today consumption'),
        value: formatUsageMetric('quota', row.todayCost, props.family, false),
      })
    }
    details.push(
      { label: t('Version'), value: row.instance.version || '--' },
      {
        label: t('Last seen'),
        value: row.instance.last_seen_at
          ? new Date(row.instance.last_seen_at * 1000).toLocaleString()
          : '--',
      },
      { label: 'URL', value: row.instance.base_url }
    )
    return details
  }

  return (
    <Accordion className={props.className}>
      {props.rows.map((row) => (
        <AccordionItem key={row.instance.id} value={String(row.instance.id)}>
          <AccordionTrigger className='min-h-20 gap-3 rounded-none px-4 py-3 hover:no-underline'>
            <div className='min-w-0 flex-1 space-y-2'>
              <div className='flex min-w-0 flex-wrap items-center gap-2'>
                <span className='min-w-0 font-medium break-words'>
                  {row.instance.name}
                </span>
                <StatusBadge status={row.instance.status} />
              </div>
              <div className='grid grid-cols-2 gap-x-4 gap-y-1'>
                {summaryMetrics(row).map(([label, value]) => (
                  <span key={label} className='min-w-0 text-xs'>
                    <span className='text-muted-foreground'>{label}</span>
                    <span className='ml-1 font-mono break-words tabular-nums'>
                      {value}
                    </span>
                  </span>
                ))}
              </div>
            </div>
          </AccordionTrigger>
          <AccordionContent className='px-4 pb-4'>
            <div className='bg-muted/35 grid grid-cols-2 gap-x-4 gap-y-3 rounded-md p-3 min-[420px]:grid-cols-3'>
              {detailMetrics(row).map((detail) => (
                <div key={detail.label} className='min-w-0'>
                  <p className='text-muted-foreground mb-1 text-xs'>
                    {detail.label}
                  </p>
                  <p className='font-mono text-xs leading-5 break-all tabular-nums'>
                    {detail.value}
                  </p>
                </div>
              ))}
            </div>
            <Button
              variant='outline'
              className='mt-3 h-11 w-full'
              render={
                <Link
                  to='/instances/$id'
                  params={{ id: String(row.instance.id) }}
                />
              }
            >
              {t('Details')}
            </Button>
          </AccordionContent>
        </AccordionItem>
      ))}
    </Accordion>
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
  const compactValue = String(props.value).length > 14
  const toneClass: Record<MetricCardTone, string> = {
    success: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
    blue: 'bg-sky-500/10 text-sky-600 dark:text-sky-400',
    violet: 'bg-violet-500/10 text-violet-600 dark:text-violet-400',
    amber: 'bg-amber-500/10 text-amber-600 dark:text-amber-400',
    danger: 'bg-red-500/10 text-red-600 dark:text-red-400',
    neutral: 'bg-muted text-muted-foreground',
  }
  return (
    <div className='bg-card min-h-32 min-w-0 px-3 py-3.5 sm:min-h-28 sm:px-4 sm:py-4'>
      <div className='flex min-w-0 items-center justify-between gap-2'>
        <p className='text-muted-foreground min-w-0 text-xs leading-4 font-medium break-words'>
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
                className={cn(
                  'focus-visible:ring-ring mt-2 block max-w-full cursor-help break-words text-left font-mono leading-tight font-semibold tracking-tight tabular-nums outline-none focus-visible:ring-2',
                  'min-h-11 py-1',
                  compactValue ? 'text-lg sm:text-xl' : 'text-xl sm:text-2xl'
                )}
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
        <p
          className={cn(
            'mt-2 break-words font-mono leading-tight font-semibold tracking-tight tabular-nums',
            compactValue ? 'text-lg sm:text-xl' : 'text-xl sm:text-2xl'
          )}
        >
          {props.value}
        </p>
      )}
      <p className='text-muted-foreground mt-1 text-xs leading-4 break-words'>
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

function InstanceSelect(props: {
  instances: ManagedInstance[]
  value: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const selected = props.instances.find(
    (instance) => String(instance.id) === props.value
  )
  const selectedFamily = selected ? instanceFamily(selected) : null
  const statusLabel = (instance: ManagedInstance) => {
    return t(
      instance.status
        .split('_')
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(' ')
    )
  }
  const statusDot = (instance: ManagedInstance) =>
    cn(
      'size-2 shrink-0 rounded-full',
      instance.status === 'healthy' && 'bg-emerald-500',
      instance.status === 'degraded' && 'bg-amber-500',
      instance.status === 'offline' && 'bg-red-500',
      instance.status === 'auth_failed' && 'bg-fuchsia-500',
      instance.status === 'unknown' && 'bg-muted-foreground/50'
    )

  return (
    <Select
      items={props.instances.map((instance) => ({
        value: String(instance.id),
        label: instance.name,
      }))}
      value={props.value}
      onValueChange={(value) => value && props.onChange(value)}
    >
      <SelectTrigger
        className='h-auto min-h-14 w-full min-w-0 px-3 py-2 whitespace-normal'
        aria-label={t('Select site')}
      >
        {selected ? (
          <span className='grid min-w-0 flex-1 gap-0.5 text-left'>
            <span className='min-w-0 leading-5 font-medium break-words'>
              {selected.name}
            </span>
            <span className='text-muted-foreground flex min-w-0 flex-wrap items-center gap-1.5 text-xs'>
              <span className={statusDot(selected)} aria-hidden='true' />
              <span>
                {selectedFamily
                  ? t(familyLabel(selectedFamily))
                  : selected.kind}
              </span>
              <span aria-hidden='true'>·</span>
              <span>{statusLabel(selected)}</span>
            </span>
          </span>
        ) : (
          <span className='text-muted-foreground'>{t('Select site')}</span>
        )}
      </SelectTrigger>
      <SelectContent align='start' className='max-h-[min(60vh,24rem)]'>
        {props.instances.map((instance) => {
          const family = instanceFamily(instance)
          return (
            <SelectItem
              key={instance.id}
              value={String(instance.id)}
              className='min-h-12 whitespace-normal'
            >
              <span className='grid min-w-0 flex-1 gap-0.5'>
                <span className='leading-5 font-medium break-words'>
                  {instance.name}
                </span>
                <span className='text-muted-foreground flex flex-wrap items-center gap-1.5 text-xs'>
                  <span className={statusDot(instance)} aria-hidden='true' />
                  <span>{family ? t(familyLabel(family)) : instance.kind}</span>
                  <span aria-hidden='true'>·</span>
                  <span>{statusLabel(instance)}</span>
                </span>
              </span>
            </SelectItem>
          )
        })}
      </SelectContent>
    </Select>
  )
}

function InstanceTabs(props: {
  instances: ManagedInstance[]
  value: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='w-full [scrollbar-width:thin] overflow-x-auto'>
      <div
        role='tablist'
        aria-label={t('Select site')}
        className='bg-muted flex h-9 w-max min-w-full items-center gap-0.5 rounded-md p-0.5'
      >
        {props.instances.map((instance) => {
          const family = instanceFamily(instance)
          const selected = props.value === String(instance.id)
          return (
            <button
              key={instance.id}
              type='button'
              role='tab'
              aria-selected={selected}
              title={`${instance.name} · ${family ? t(familyLabel(family)) : instance.kind}`}
              className={cn(
                'focus-visible:ring-ring flex h-8 max-w-56 shrink-0 items-center gap-2 rounded-sm px-3 text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none',
                selected
                  ? 'bg-background text-foreground shadow-xs'
                  : 'text-muted-foreground hover:bg-background/60 hover:text-foreground'
              )}
              onClick={() => props.onChange(String(instance.id))}
            >
              <span
                className={cn(
                  'size-1.5 shrink-0 rounded-full',
                  instance.status === 'healthy' && 'bg-emerald-500',
                  instance.status === 'degraded' && 'bg-amber-500',
                  instance.status === 'offline' && 'bg-red-500',
                  instance.status === 'auth_failed' && 'bg-fuchsia-500',
                  instance.status === 'unknown' && 'bg-muted-foreground/50'
                )}
                aria-hidden='true'
              />
              <span className='truncate'>{instance.name}</span>
              {family && (
                <span className='text-muted-foreground/80 shrink-0 text-[10px] font-normal'>
                  {t(familyLabel(family))}
                </span>
              )}
            </button>
          )
        })}
      </div>
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
      <div className='bg-border border-border/80 grid grid-cols-1 gap-px overflow-hidden rounded-lg border min-[420px]:grid-cols-2 md:grid-cols-3 xl:grid-cols-6'>
        {KPI_SKELETON_KEYS.map((key) => (
          <Skeleton key={key} className='h-32 rounded-none sm:h-28' />
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
