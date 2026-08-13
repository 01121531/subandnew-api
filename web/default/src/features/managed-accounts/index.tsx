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
  CheckCircle2,
  CircleHelp,
  CircleDollarSign,
  DatabaseZap,
  RefreshCw,
  Search,
  Server,
  UserPlus,
  Users,
  XCircle,
} from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
  getManagedInstanceInventory,
  getManagedInstanceAccountOutput,
  getManagedInstances,
} from '@/features/managed-instances/api'
import { InstanceConnectionAlert } from '@/features/managed-instances/components/instance-connection-alert'
import { isInstanceConnectionError } from '@/features/managed-instances/errors'
import type {
  ManagedInstance,
  ManagedInstanceAccountOutputItem,
  ManagedInstanceInventoryItem,
} from '@/features/managed-instances/types'
import { cn } from '@/lib/utils'

type AccountFamily = 'new_api' | 'sub2api' | 'conductor'
type ResourceRow = {
  instance: ManagedInstance
  item: ManagedInstanceInventoryItem
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

function formatTimestamp(value?: number) {
  if (!value) return '--'
  const date = new Date(value * 1000)
  return Number.isNaN(date.getTime()) ? '--' : accountDateTime.format(date)
}

function getSurvivalSeconds(item: ManagedInstanceInventoryItem) {
  if (!item.created_at) return null

  let end: number | undefined
  if (item.enabled === true) {
    end = Math.floor(Date.now() / 1000)
  } else if (item.enabled === false) {
    end = item.last_activity_at
  }
  if (!end || end < item.created_at) return null
  return end - item.created_at
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

  const inventoryQueries = useQueries({
    queries: instances.map((instance) => ({
      queryKey: ['managed-account-inventory', instance.id],
      queryFn: async () => {
        const response = await getManagedInstanceInventory(
          instance.id,
          'auto',
          '',
          { silent: true }
        )
        const observation = response.data
        if (
          observation.collection_status !== 'succeeded' ||
          !observation.data
        ) {
          throw new Error(observation.error_code || 'collection_failed')
        }
        return response
      },
      retry: 1,
      retryDelay: FAILED_REFRESH_RETRY_MS,
      staleTime: INVENTORY_REFRESH_MS / 2,
      refetchInterval: INVENTORY_REFRESH_MS,
    })),
  })
  const outputQueries = useQueries({
    queries: instances.map((instance, index) => ({
      queryKey: [
        'managed-account-output',
        instance.id,
        timeRange.presetDays ?? 'custom',
        timeRange.presetDays ? null : timeRange.start.getTime(),
        timeRange.presetDays ? null : timeRange.end.getTime(),
      ],
      queryFn: async () => {
        const resolved = resolveFleetTimeRange(timeRange)
        const response = await getManagedInstanceAccountOutput(
          instance.id,
          {
            start: Math.floor(resolved.start.getTime() / 1000),
            end: Math.floor(resolved.end.getTime() / 1000),
          },
          { silent: true }
        )
        if (
          response.data.collection_status !== 'succeeded' ||
          !response.data.data
        ) {
          throw new Error(response.data.error_code || 'collection_failed')
        }
        return response
      },
      retry: 1,
      retryDelay: FAILED_REFRESH_RETRY_MS,
      staleTime: INVENTORY_REFRESH_MS / 2,
      refetchInterval: INVENTORY_REFRESH_MS,
      enabled:
        inventoryQueries[index]?.data?.data.collection_status === 'succeeded',
    })),
  })
  const rows = useMemo<ResourceRow[]>(
    () =>
      instances.flatMap((instance, index) => {
        const observation = inventoryQueries[index]?.data?.data
        if (
          observation?.collection_status !== 'succeeded' ||
          !observation.data
        ) {
          return []
        }
        return observation.data.items.map((item) => ({ instance, item }))
      }),
    [instances, inventoryQueries]
  )
  const outputRows = useMemo<OutputRow[]>(
    () =>
      instances.flatMap((instance, index) =>
        (outputQueries[index]?.data?.data.data?.items ?? []).map((output) => ({
          instance,
          output,
        }))
      ),
    [instances, outputQueries]
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
      ? rows.filter(({ instance, item }) =>
          [
            item.id,
            item.name,
            item.platform,
            item.type,
            item.group,
            item.status,
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
  const loading = inventoryQueries.some((query) => query.isPending)
  const error = inventoryQueries.some((query) => {
    const observation = query.data?.data
    return (
      query.isError ||
      (observation != null && observation.collection_status !== 'succeeded')
    )
  })
  const connectionFailed = [...inventoryQueries, ...outputQueries].some(
    (query) => isInstanceConnectionError(query.error)
  )
  const collectedInstances = inventoryQueries.filter(
    (query) => query.data?.data.collection_status === 'succeeded'
  ).length
  const available = rows.filter((row) => row.item.enabled === true).length
  const unavailable = rows.filter((row) => row.item.enabled === false).length
  const unknown = rows.length - available - unavailable
  const coverage = instances.length
    ? Math.round((collectedInstances / instances.length) * 100)
    : 0
  const isRefreshing =
    instancesQuery.isFetching ||
    inventoryQueries.some((query) => query.isFetching) ||
    outputQueries.some((query) => query.isFetching)

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

  const refresh = () => {
    void instancesQuery.refetch()
    for (const query of inventoryQueries) void query.refetch()
    for (const query of outputQueries) void query.refetch()
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
        <AccountSummary
          total={rows.length}
          available={available}
          unavailable={unavailable}
          unknown={unknown}
          coverage={coverage}
        />
        <AccountOutputPanel
          family={family}
          rows={outputRows}
          totals={outputTotals}
          loading={outputQueries.some((query) => query.isPending)}
          error={outputQueries.some((query) => query.isError)}
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

function AccountSummary(props: {
  total: number
  available: number
  unavailable: number
  unknown: number
  coverage: number
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
  return (
    <section className='grid grid-cols-2 gap-3 xl:grid-cols-4'>
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
  const unsupported = props.family === 'conductor'
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
  if (unsupported) {
    detailContent = (
      <PanelEmpty
        text={t('Conductor does not expose usage by upstream account')}
      />
    )
  } else if (props.loading && props.rows.length === 0) {
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
                {unsupported && item.key !== 'added' ? '--' : item.value}
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
  const isChannel = props.family === 'new_api'
  const isConductor = props.family === 'conductor'
  const showsSurvival = !isChannel && !isConductor
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
  let content: ReactNode
  if (props.loading && props.total === 0) {
    content = <TableSkeleton wide={!isChannel} />
  } else if (props.rows.length === 0) {
    content = <PanelEmpty text={emptyText} />
  } else {
    content = (
      <Table className={cn(isChannel ? 'min-w-[980px]' : 'min-w-[1140px]')}>
        <TableHeader className='bg-muted/35'>
          <TableRow>
            <TableHead className='ps-6'>
              {t(isChannel ? 'Channel' : 'Account')}
            </TableHead>
            <TableHead>{t('Instance')}</TableHead>
            <TableHead>
              {t('Platform')} / {t('Type')}
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
          {props.rows.map(({ instance, item }) => {
            const descriptors = [item.platform, item.type, item.group].filter(
              (value, index, values): value is string =>
                Boolean(value) && values.indexOf(value) === index
            )
            const survivalSeconds = showsSurvival
              ? getSurvivalSeconds(item)
              : null
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
                  <span className='block max-w-44 truncate text-sm'>
                    {descriptors.join(' / ') || '--'}
                  </span>
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
                  <AvailabilityBadge enabled={item.enabled} />
                  {item.error_message && (
                    <p
                      className='text-destructive ms-auto mt-1 max-w-40 truncate text-xs'
                      title={item.error_message}
                    >
                      {item.error_message}
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
    </Card>
  )
}

function formatOptionalNumber(value?: number) {
  return value == null ? '--' : compactNumber.format(value)
}

function AvailabilityBadge({ enabled }: { enabled?: boolean }) {
  const { t } = useTranslation()
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
