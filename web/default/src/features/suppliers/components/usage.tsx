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
    <div className='grid min-w-0 gap-5'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
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
      >
        <dl className='grid gap-4 border-y py-5 min-[420px]:grid-cols-3'>
          <Metric
            label={t('supplier.cost')}
            value={formatCost(total.cost.value)}
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
          <h2 className='font-semibold'>{t('supplier.trend')}</h2>
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
            <TabsList>
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
            role='img'
            aria-label={t('supplier.trend')}
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
                    borderRadius: 6,
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
        <h2 className='font-semibold'>{t('supplier.accountUsage')}</h2>
        {!query.data?.accounts?.length ? (
          <Empty />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                {['name', 'requests', 'tokens', 'cost'].map((key) => (
                  <TableHead key={key}>{t(`supplier.${key}`)}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.data.accounts.map((account) => (
                <TableRow key={account.id}>
                  <TableCell className='max-w-64 truncate'>
                    {account.name}
                  </TableCell>
                  <TableCell>{formatNumber(account.requests)}</TableCell>
                  <TableCell>{formatNumber(account.tokens)}</TableCell>
                  <TableCell>{formatCost(account.cost)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </QueryState>
    </div>
  )
}
