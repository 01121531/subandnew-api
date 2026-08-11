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
import {
  Activity,
  AlertTriangle,
  Building2,
  CheckCircle2,
  CircleDollarSign,
  DatabaseZap,
  Gauge,
  Radio,
  RefreshCw,
  Server,
  ServerOff,
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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  ChartContainer,
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
import { cn } from '@/lib/utils'

import {
  getManagedAlerts,
  getManagedInstanceMetrics,
  getManagedInstances,
} from '../managed-instances/api'
import { StatusBadge } from '../managed-instances/components/status-badge'
import type {
  ManagedInstance,
  ManagedInstanceAlert,
  ManagedInstanceSummary,
} from '../managed-instances/types'
import {
  createFleetPresetRange,
  FLEET_TIME_PRESETS,
  resolveFleetTimeRange,
  type FleetPresetDays,
  type FleetTimeRange,
} from './time-range'
import { FleetTimeRangeFilter } from './time-range-filter'

type MetricKey = 'requests' | 'tokens' | 'quota'
type FleetFamily = 'new_api' | 'sub2api' | 'conductor'

type InstanceMetricRow = {
  instance: ManagedInstance
  summary?: ManagedInstanceSummary
  observedAt: number
  collected: boolean
  requests: number | null
  tokens: number | null
  quota: number | null
}

type DailyUsageData = {
  date: string
  value: number
}

type HealthData = {
  key: string
  label: string
  color: string
  value: number
}

const LIVE_REFRESH_MS = 15_000
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
const EMPTY_ALERTS: ManagedInstanceAlert[] = []
const KPI_SKELETON_KEYS = [
  'availability',
  'requests',
  'tokens',
  'quota',
  'alerts',
  'coverage',
]

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
const exactCurrency = new Intl.NumberFormat(undefined, {
  style: 'currency',
  currency: 'USD',
  maximumFractionDigits: 4,
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

const CONSUMPTION_CHART_CONFIG = {
  value: { label: 'Value', color: 'var(--chart-1)' },
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
  metric: MetricKey
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
    metric: 'requests',
  }
}

function isFleetFamily(value: unknown): value is FleetFamily {
  return FLEET_FAMILIES.some((family) => family === value)
}

function isMetricKey(value: unknown): value is MetricKey {
  return value === 'requests' || value === 'tokens' || value === 'quota'
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
      metric: isMetricKey(parsed.metric) ? parsed.metric : fallback.metric,
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
  if (metric === 'quota' && family === 'sub2api') {
    return (compact ? compactCurrency : exactCurrency).format(value)
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
      return family === 'sub2api' ? 'Actual cost' : 'Quota'
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
  const [metric, setMetric] = useState<MetricKey>(initialPreferences.metric)

  const instancesQuery = useQuery({
    queryKey: ['fleet-dashboard-instances'],
    queryFn: () => getManagedInstances({ search: '', kind: '', status: '' }),
    refetchInterval: 30_000,
  })
  const alertsQuery = useQuery({
    queryKey: ['fleet-dashboard-alerts'],
    queryFn: getManagedAlerts,
    refetchInterval: 30_000,
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
  const alerts = alertsQuery.data?.data.items ?? EMPTY_ALERTS
  const metricQueries = useQueries({
    queries: instances.map((instance) => ({
      queryKey: [
        'fleet-dashboard-metric',
        instance.id,
        timeRange.presetDays ?? 'custom',
        timeRange.presetDays ? null : timeRange.start.getTime(),
        timeRange.presetDays ? null : timeRange.end.getTime(),
      ],
      queryFn: () => {
        const resolvedRange = resolveFleetTimeRange(timeRange)
        return getManagedInstanceMetrics(
          instance.id,
          {
            start: Math.floor(resolvedRange.start.getTime() / 1000),
            end: Math.floor(resolvedRange.end.getTime() / 1000),
          },
          { silent: true }
        )
      },
      retry: false,
      staleTime: LIVE_REFRESH_MS / 2,
      refetchInterval: LIVE_REFRESH_MS,
      refetchIntervalInBackground: true,
    })),
  })
  const rows = useMemo<InstanceMetricRow[]>(
    () =>
      instances.map((instance, index) => {
        const observation = metricQueries[index]?.data?.data
        const summary = observation?.data
        return {
          instance,
          summary,
          observedAt: observation?.observed_at ?? 0,
          collected: observation?.collection_status === 'succeeded',
          requests: metricValue(summary?.requests),
          tokens: metricValue(summary?.tokens),
          quota: metricValue(summary?.cost),
        }
      }),
    [instances, metricQueries]
  )

  const totals = useMemo(
    () => ({
      requests: rows.reduce((sum, row) => sum + (row.requests ?? 0), 0),
      tokens: rows.reduce((sum, row) => sum + (row.tokens ?? 0), 0),
      quota: rows.reduce((sum, row) => sum + (row.quota ?? 0), 0),
      collected: rows.filter((row) => row.collected).length,
      metricReady: rows.filter(
        (row) => row.requests != null || row.tokens != null || row.quota != null
      ).length,
      requestsReady: rows.filter((row) => row.requests != null).length,
      tokensReady: rows.filter((row) => row.tokens != null).length,
      quotaReady: rows.filter((row) => row.quota != null).length,
      healthy: instances.filter((item) => item.status === 'healthy').length,
      abnormal: instances.filter((item) =>
        ['degraded', 'offline', 'auth_failed'].includes(item.status)
      ).length,
    }),
    [instances, rows]
  )
  const instanceIDs = new Set(instances.map((instance) => instance.id))
  const openAlerts = alerts.filter(
    (alert) => alert.status === 'open' && instanceIDs.has(alert.instance_id)
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
        .filter((row) => row[metric] != null)
        .sort((a, b) => (b[metric] ?? 0) - (a[metric] ?? 0))
        .slice(0, 10)
        .map((row) => ({ name: row.instance.name, value: row[metric] ?? 0 })),
    [metric, rows]
  )
  const dailyUsageData = useMemo<DailyUsageData[]>(() => {
    const values = new Map<string, number>()
    for (const row of rows) {
      for (const point of row.summary?.trend ?? []) {
        values.set(
          point.date,
          (values.get(point.date) ?? 0) + trendMetricValue(point, metric)
        )
      }
    }
    return [...values.entries()]
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([date, value]) => ({ date, value }))
  }, [metric, rows])
  const isRefreshing =
    instancesQuery.isFetching ||
    alertsQuery.isFetching ||
    metricQueries.some((query) => query.isFetching)
  const coverage = instances.length
    ? Math.round((totals.collected / instances.length) * 100)
    : 0
  const metricCoverage = instances.length
    ? Math.round((totals.metricReady / instances.length) * 100)
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
        metric,
      })
    )
  }, [family, metric, selectedInstances, timeRange])

  const lastObservedAt = Math.max(0, ...rows.map((row) => row.observedAt))

  const refresh = () => {
    void instancesQuery.refetch()
    void alertsQuery.refetch()
    for (const query of metricQueries) void query.refetch()
  }

  const handleFamilyChange = (nextFamily: FleetFamily) => {
    setFamily(nextFamily)
  }

  let content: ReactNode
  if (instancesQuery.isLoading) {
    content = <DashboardSkeleton />
  } else if (instancesQuery.isError) {
    content = <DashboardError onRetry={refresh} />
  } else if (instances.length === 0) {
    content = <EmptyFleet family={family} />
  } else {
    content = (
      <DashboardContent
        instances={instances}
        family={family}
        rows={rows}
        totals={totals}
        openAlerts={openAlerts}
        alertsError={alertsQuery.isError}
        healthData={healthData}
        chartData={chartData}
        dailyUsageData={dailyUsageData}
        healthRate={healthRate}
        coverage={coverage}
        metricCoverage={metricCoverage}
        metric={metric}
        onMetricChange={setMetric}
      />
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
                {t('Auto-refreshing every {{seconds}}s', { seconds: 15 })}
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
    tokens: number
    quota: number
    collected: number
    metricReady: number
    requestsReady: number
    tokensReady: number
    quotaReady: number
    healthy: number
    abnormal: number
  }
  openAlerts: ManagedInstanceAlert[]
  alertsError: boolean
  healthData: HealthData[]
  chartData: { name: string; value: number }[]
  dailyUsageData: DailyUsageData[]
  healthRate: number
  coverage: number
  metricCoverage: number
  metric: MetricKey
  onMetricChange: (metric: MetricKey) => void
}

function DashboardContent(props: DashboardContentProps) {
  return (
    <div className='grid gap-4 pb-6'>
      <SummaryGrid {...props} />
      <DailyUsagePanel
        family={props.family}
        metric={props.metric}
        data={props.dailyUsageData}
        onMetricChange={props.onMetricChange}
      />
      <section className='grid gap-4 xl:grid-cols-[minmax(0,1.7fr)_minmax(300px,0.8fr)]'>
        <ConsumptionPanel
          family={props.family}
          metric={props.metric}
          data={props.chartData}
          onMetricChange={props.onMetricChange}
        />
        <HealthPanel data={props.healthData} total={props.instances.length} />
      </section>
      <AlertsPanel
        alerts={props.openAlerts}
        instances={props.instances}
        error={props.alertsError}
      />
      <PerformanceTable family={props.family} rows={props.rows} />
    </div>
  )
}

function DailyUsagePanel(props: {
  family: FleetFamily
  metric: MetricKey
  data: DailyUsageData[]
  onMetricChange: (metric: MetricKey) => void
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
          <CardTitle>{t('Daily usage trend')}</CardTitle>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Daily totals in the selected period')}
          </p>
        </div>
        <SegmentedControl
          value={props.metric}
          options={['requests', 'tokens', 'quota']}
          getLabel={(value) => t(metricLabel(value, props.family))}
          onChange={props.onMetricChange}
        />
      </CardHeader>
      <CardContent className='py-4'>
        {props.data.length ? (
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
                  formatUsageMetric(props.metric, value, props.family)
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
                      <span className='font-mono font-medium tabular-nums'>
                        {formatUsageMetric(
                          props.metric,
                          Number(value),
                          props.family,
                          false
                        )}
                      </span>
                    )}
                  />
                }
              />
              <Line
                type='monotone'
                dataKey='value'
                name={t(metricLabel(props.metric, props.family))}
                stroke='var(--color-value)'
                strokeWidth={2.25}
                dot={props.data.length <= 14}
                activeDot={{ r: 4 }}
              />
            </LineChart>
          </ChartContainer>
        ) : (
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
    props.family === 'sub2api' ? t('Actual cost') : t('Quota usage')
  const costDetail =
    props.family === 'sub2api'
      ? t('Actual cost reported by Sub2API')
      : t('Remote quota units')

  return (
    <section
      aria-label={t('Fleet summary')}
      className='bg-border border-border/80 grid grid-cols-2 gap-px overflow-hidden rounded-lg border shadow-xs sm:grid-cols-3 xl:grid-cols-6'
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
      <MetricCard
        icon={CircleDollarSign}
        label={costLabel}
        value={formatUsageMetric(
          'quota',
          props.totals.quotaReady ? props.totals.quota : null,
          props.family
        )}
        detail={
          props.totals.quotaReady ? costDetail : t('No metric data available')
        }
        tone='amber'
      />
      <MetricCard
        icon={AlertTriangle}
        label={t('Open alerts')}
        value={props.openAlerts.length}
        detail={t('{{count}} abnormal instances', {
          count: props.totals.abnormal,
        })}
        tone={props.totals.abnormal ? 'danger' : 'success'}
      />
      <MetricCard
        icon={Gauge}
        label={t('Metric coverage')}
        value={`${props.metricCoverage}%`}
        detail={t('{{count}} metrics available', {
          count: props.totals.metricReady,
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
        <SegmentedControl
          value={props.metric}
          options={['requests', 'tokens', 'quota']}
          getLabel={(value) => t(metricLabel(value, props.family))}
          onChange={props.onMetricChange}
        />
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
                      <span className='font-mono font-medium tabular-nums'>
                        {formatUsageMetric(
                          props.metric,
                          Number(value),
                          props.family,
                          false
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

function AlertsPanel(props: {
  alerts: ManagedInstanceAlert[]
  instances: ManagedInstance[]
  error: boolean
}) {
  const { t } = useTranslation()
  let content: ReactNode
  if (props.error) {
    content = <PanelEmpty text={t('Failed to load alerts')} compact />
  } else if (props.alerts.length === 0) {
    content = (
      <div className='flex min-h-40 flex-col items-center justify-center text-center'>
        <CheckCircle2 className='text-success mb-2 size-8' />
        <p className='text-sm font-medium'>{t('All clear')}</p>
        <p className='text-muted-foreground text-xs'>{t('No active alerts')}</p>
      </div>
    )
  } else {
    content = (
      <div className='divide-border divide-y'>
        {props.alerts.slice(0, 5).map((alert) => {
          const instance = props.instances.find(
            (item) => item.id === alert.instance_id
          )
          return (
            <div
              key={alert.id}
              className='flex items-start gap-3 py-2.5 first:pt-0 last:pb-0'
            >
              <span className='bg-destructive/10 text-destructive mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md'>
                <ServerOff className='size-3.5' />
              </span>
              <div className='min-w-0 flex-1'>
                <p className='truncate text-sm font-medium'>
                  {instance?.name ?? `#${alert.instance_id}`}
                </p>
                <p className='text-muted-foreground truncate text-xs'>
                  {t(alert.error_code || alert.alert_type)} ·{' '}
                  {t('{{count}} occurrences', { count: alert.occurrences })}
                </p>
              </div>
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <Card className={PANEL_CARD_CLASS}>
      <CardHeader
        className={cn(
          PANEL_HEADER_CLASS,
          'flex-row items-center justify-between space-y-0'
        )}
      >
        <CardTitle className='flex items-center gap-2'>
          <AlertTriangle className='text-muted-foreground size-4' />
          {t('Active alerts')}
        </CardTitle>
        <Badge variant={props.alerts.length ? 'destructive' : 'secondary'}>
          {props.alerts.length}
        </Badge>
      </CardHeader>
      <CardContent className='py-4'>{content}</CardContent>
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
  const sortedRows = [...rows]
    .sort((a, b) => (b.requests ?? -1) - (a.requests ?? -1))
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
                <TableHead className='text-right'>{t('Requests')}</TableHead>
                <TableHead className='text-right'>{t('Tokens')}</TableHead>
                <TableHead className='text-right'>
                  {t(metricLabel('quota', family))}
                </TableHead>
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
                  <TableCell className='text-right font-mono text-xs tabular-nums'>
                    {formatMetric(row.requests, false)}
                  </TableCell>
                  <TableCell className='text-right font-mono text-xs tabular-nums'>
                    {formatMetric(row.tokens, false)}
                  </TableCell>
                  <TableCell className='text-right font-mono text-xs tabular-nums'>
                    {formatUsageMetric('quota', row.quota, family, false)}
                  </TableCell>
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
}) {
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
        <span
          className={cn(
            'flex size-7 shrink-0 items-center justify-center rounded-md',
            toneClass[props.tone]
          )}
        >
          <props.icon className='size-3.5' aria-hidden='true' />
        </span>
      </div>
      <p className='mt-2 font-mono text-xl font-semibold tracking-tight tabular-nums sm:text-2xl'>
        {props.value}
      </p>
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
