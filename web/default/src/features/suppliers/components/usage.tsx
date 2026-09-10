import { ChevronDown } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { usePortalQuery } from '../hooks/use-portal-query'
import { formatNumber } from '../lib/display'
import { aggregateUsage, formatCost } from '../lib/usage'
import { portalApi } from '../portal-api'
import type { UsageMetrics } from '../types'
import { Metric } from './accounts'
import { Empty, Freshness, QueryState } from './common'
import { CostValue } from './cost-value'

export function UsageView(props: { bindingId: number }) {
  const { t } = useTranslation()
  const [days, setDays] = useState(7)
  const [metric, setMetric] = useState<keyof UsageMetrics>('cost')
  const query = usePortalQuery(
    ['usage', props.bindingId, days],
    (signal, refresh) => portalApi.usage(props.bindingId, days, signal, refresh)
  )
  const total = aggregateUsage(query.data?.days)
  const colors = { cost: '#059669', requests: '#0284c7', tokens: '#d97706' }
  return (
    <div className='supplier-portal grid min-w-0 gap-4'>
      <div className='supplier-filter-band flex flex-wrap items-center justify-between gap-3'>
        <Tabs
          value={String(days)}
          onValueChange={(value) => setDays(Number(value))}
        >
          <TabsList aria-label={t('supplier.period')}>
            {[1, 7, 30].map((value) => (
              <TabsTrigger key={value} value={String(value)}>
                {t('supplier.days', { count: value })}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Freshness
          data={query.data}
          pending={query.isFetching}
          refresh={query.refresh}
        />
      </div>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={query.refresh}
        hasData={!!query.data}
      >
        <h2 className='text-sm font-semibold'>
          {t('supplier.ui_period_totals', {
            period: t('supplier.days', { count: days }),
          })}
        </h2>
        <dl className='supplier-summary-band grid gap-4 min-[420px]:grid-cols-3'>
          <Metric
            label={t('supplier.cost')}
            value={<CostValue value={total.cost.value} />}
            note={total.cost.partial ? t('supplier.partialData') : undefined}
          />
          <Metric
            label={t('supplier.requests')}
            value={formatNumber(total.requests.value)}
            note={
              total.requests.partial ? t('supplier.partialData') : undefined
            }
          />
          <Metric
            label={t('supplier.tokens')}
            value={formatNumber(total.tokens.value)}
            note={total.tokens.partial ? t('supplier.partialData') : undefined}
          />
        </dl>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <h2 className='text-sm font-semibold'>{t('supplier.trend')}</h2>
          <Tabs
            value={metric}
            onValueChange={(value) => {
              if (
                value === 'cost' ||
                value === 'requests' ||
                value === 'tokens'
              ) {
                setMetric(value)
              }
            }}
          >
            <TabsList aria-label={t('supplier.trend')}>
              {(['cost', 'requests', 'tokens'] as const).map((value) => (
                <TabsTrigger key={value} value={value}>
                  {t(`supplier.${value}`)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </div>
        {!query.data?.days.length ? (
          <Empty />
        ) : (
          <div
            className='h-72 min-w-0'
            role='group'
            aria-label={`${t('supplier.trend')}: ${t(`supplier.${metric}`)}`}
          >
            <ResponsiveContainer width='100%' height='100%'>
              <BarChart
                data={query.data.days}
                margin={{ top: 12, right: 12, bottom: 8, left: 0 }}
                accessibilityLayer
              >
                <CartesianGrid stroke='var(--border)' vertical={false} />
                <XAxis
                  dataKey='date'
                  tick={{ fill: 'var(--muted-foreground)', fontSize: 11 }}
                  tickFormatter={(value: string) => value.slice(5)}
                  axisLine={false}
                  tickLine={false}
                />
                <YAxis
                  width={60}
                  tick={{ fill: 'var(--muted-foreground)', fontSize: 11 }}
                  axisLine={false}
                  tickLine={false}
                />
                <Tooltip
                  formatter={(value) =>
                    metric === 'cost'
                      ? formatCost(typeof value === 'number' ? value : null)
                      : formatNumber(typeof value === 'number' ? value : null)
                  }
                  contentStyle={{
                    backgroundColor: 'var(--popover)',
                    color: 'var(--popover-foreground)',
                    borderColor: 'var(--border)',
                    borderRadius: 8,
                  }}
                  cursor={{ fill: 'var(--muted)' }}
                />
                <Bar
                  dataKey={metric}
                  name={t(`supplier.${metric}`)}
                  fill={colors[metric]}
                  maxBarSize={48}
                  radius={[3, 3, 0, 0]}
                />
              </BarChart>
            </ResponsiveContainer>
          </div>
        )}
        {!!query.data?.days.length && (
          <details className='group min-w-0 border-y'>
            <summary className='supplier-disclosure-heading flex cursor-pointer list-none items-center justify-between gap-2 py-3 text-sm font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-600'>
              {t('supplier.ui_daily_usage')}
              <ChevronDown
                className='size-4 shrink-0 transition-transform group-open:rotate-180'
                aria-hidden='true'
              />
            </summary>
            <UsageBreakdown
              label={t('supplier.ui_daily_usage')}
              nameLabel={t('supplier.ui_date')}
              items={query.data.days.map((day) => ({
                ...day,
                id: day.date,
                label: day.date,
              }))}
            />
          </details>
        )}
        <h2 className='text-sm font-semibold'>{t('supplier.accountUsage')}</h2>
        {!query.data?.accounts?.length ? (
          <Empty />
        ) : (
          <UsageBreakdown
            label={t('supplier.accountUsage')}
            nameLabel={t('supplier.name')}
            items={query.data.accounts.map((account) => ({
              ...account,
              label: account.name || '--',
            }))}
          />
        )}
      </QueryState>
    </div>
  )
}

function UsageBreakdown(props: {
  label: string
  nameLabel: string
  items: Array<UsageMetrics & { id: string; label: string }>
}) {
  const { t } = useTranslation()
  return (
    <div className='min-w-0'>
      <div
        className='grid min-w-0 gap-3 pb-3 md:hidden'
        role='list'
        aria-label={props.label}
      >
        {props.items.map((item) => (
          <article
            key={item.id}
            role='listitem'
            className='supplier-data-item min-w-0 rounded-md border p-3'
          >
            <h3 className='text-sm font-medium [overflow-wrap:anywhere]'>
              {item.label}
            </h3>
            <dl className='mt-3 grid min-w-0 grid-cols-2 gap-3 text-sm'>
              <div className='col-span-2 min-w-0'>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.cost')}
                </dt>
                <dd className='font-medium [overflow-wrap:anywhere] tabular-nums'>
                  <CostValue value={item.cost} />
                </dd>
              </div>
              <div className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.requests')}
                </dt>
                <dd className='[overflow-wrap:anywhere] tabular-nums'>
                  {formatNumber(item.requests)}
                </dd>
              </div>
              <div className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>
                  {t('supplier.tokens')}
                </dt>
                <dd className='[overflow-wrap:anywhere] tabular-nums'>
                  {formatNumber(item.tokens)}
                </dd>
              </div>
            </dl>
          </article>
        ))}
      </div>
      <div className='hidden min-w-0 md:block'>
        <Table aria-label={props.label}>
          <TableHeader>
            <TableRow>
              <TableHead scope='col'>{props.nameLabel}</TableHead>
              {['requests', 'tokens', 'cost'].map((key) => (
                <TableHead key={key} scope='col' className='text-right'>
                  {t(`supplier.${key}`)}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className='max-w-64 font-medium break-all whitespace-normal'>
                  {item.label}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatNumber(item.requests)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatNumber(item.tokens)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  <CostValue value={item.cost} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
