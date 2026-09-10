import { RotateCcw } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { usePortalQuery } from '../hooks/use-portal-query'
import { formatNumber } from '../lib/display'
import { portalApi } from '../portal-api'
import type { AccountQuery } from '../types'
import { AccountCards } from './account-cards'
import {
  Empty,
  Field,
  Freshness,
  Pagination,
  QueryState,
  SelectField,
  Time,
} from './common'
import { CostValue } from './cost-value'

export function Accounts(props: { bindingId: number }) {
  const { t } = useTranslation()
  const [searchDraft, setSearchDraft] = useState('')
  const [filters, setFilters] = useState<AccountQuery>({
    binding_id: props.bindingId,
    page: 1,
    page_size: 20,
    search: '',
    status: '',
    recovery_window: '',
    sort: 'created_at',
    direction: 'desc',
  })
  const query = usePortalQuery(['accounts', filters], (signal, refresh) =>
    portalApi.accounts(filters, signal, refresh)
  )
  useEffect(() => {
    const timer = window.setTimeout(
      () =>
        setFilters((old) =>
          old.search === searchDraft
            ? old
            : { ...old, search: searchDraft, page: 1 }
        ),
      300
    )
    return () => window.clearTimeout(timer)
  }, [searchDraft])
  const summary = usePortalQuery(
    ['summary', props.bindingId],
    (signal, refresh) => portalApi.summary(props.bindingId, signal, refresh)
  )
  const update = (values: Partial<AccountQuery>) =>
    setFilters((old) => ({ ...old, ...values, page: 1 }))
  const canReset =
    searchDraft !== '' ||
    filters.search !== '' ||
    filters.status !== '' ||
    filters.recovery_window !== '' ||
    filters.sort !== 'created_at' ||
    filters.direction !== 'desc' ||
    filters.page_size !== 20 ||
    filters.page !== 1
  const reset = () => {
    setSearchDraft('')
    setFilters({
      binding_id: props.bindingId,
      page: 1,
      page_size: 20,
      search: '',
      status: '',
      recovery_window: '',
      sort: 'created_at',
      direction: 'desc',
    })
  }
  return (
    <div className='supplier-portal grid min-w-0 gap-4'>
      <QueryState
        pending={summary.isPending}
        error={summary.error}
        retry={summary.refresh}
        hasData={!!summary.data}
      >
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <h2 className='text-sm font-semibold'>
            {t('supplier.ui_binding_account_summary')}
          </h2>
          <Freshness
            data={summary.data}
            pending={summary.isFetching}
            refresh={summary.refresh}
          />
        </div>
        <dl className='supplier-summary-band grid gap-3 min-[420px]:grid-cols-3'>
          <Metric
            label={t('supplier.totalAccounts')}
            value={formatNumber(summary.data?.total_accounts)}
          />
          <Metric
            label={t('supplier.availableAccounts')}
            value={formatNumber(summary.data?.available_accounts)}
          />
          <Metric
            label={t('supplier.rpm')}
            value={formatNumber(summary.data?.rpm)}
          />
        </dl>
        {(summary.data?.pool_rpm !== undefined ||
          summary.data?.pool_concurrent !== undefined ||
          summary.data?.pool_available_accounts !== undefined) && (
          <dl className='supplier-summary-band grid gap-3 min-[420px]:grid-cols-3'>
            {summary.data?.pool_rpm !== undefined && (
              <Metric
                label={t('supplier.poolRpm')}
                value={formatNumber(summary.data.pool_rpm)}
              />
            )}
            {summary.data?.pool_concurrent !== undefined && (
              <Metric
                label={t('supplier.poolConcurrent')}
                value={formatNumber(summary.data.pool_concurrent)}
              />
            )}
            {summary.data?.pool_available_accounts !== undefined && (
              <Metric
                label={t('supplier.poolAvailableAccounts')}
                value={formatNumber(summary.data.pool_available_accounts)}
              />
            )}
          </dl>
        )}
      </QueryState>
      <section
        aria-label={t('supplier.ui_account_filters')}
        className='supplier-filter-band grid min-w-0 gap-3'
      >
        <div className='flex items-center justify-between gap-3'>
          <h2 className='text-sm font-semibold'>
            {t('supplier.ui_account_filters')}
          </h2>
          <Button
            variant='ghost'
            size='icon'
            title={t('supplier.ui_reset_filters')}
            aria-label={t('supplier.ui_reset_filters')}
            disabled={!canReset}
            onClick={reset}
          >
            <RotateCcw aria-hidden='true' />
          </Button>
        </div>
        <div className='grid min-w-0 grid-cols-2 items-end gap-3 md:grid-cols-3 xl:grid-cols-[minmax(12rem,2fr)_repeat(5,minmax(0,1fr))]'>
          <div className='col-span-2 min-w-0 md:col-span-1'>
            <Field id='account-search' label={t('supplier.search')}>
              <Input
                id='account-search'
                type='search'
                value={searchDraft}
                onChange={(event) => setSearchDraft(event.target.value)}
              />
            </Field>
          </div>
          <SelectField
            id='account-status'
            label={t('supplier.status')}
            value={filters.status}
            onChange={(status) => update({ status })}
          >
            <NativeSelectOption value=''>
              {t('supplier.all')}
            </NativeSelectOption>
            {['available', 'active', 'cooldown', 'disabled'].map((status) => (
              <NativeSelectOption key={status} value={status}>
                {t(`supplier.${status}`)}
              </NativeSelectOption>
            ))}
          </SelectField>
          <SelectField
            id='account-recovery'
            label={t('supplier.recovery')}
            value={filters.recovery_window}
            onChange={(recovery_window) => update({ recovery_window })}
          >
            <NativeSelectOption value=''>
              {t('supplier.all')}
            </NativeSelectOption>
            <NativeSelectOption value='15m'>
              {t('supplier.minutes', { count: 15 })}
            </NativeSelectOption>
            {[1, 2, 3, 4, 5].map((hours) => (
              <NativeSelectOption key={hours} value={`${hours}h`}>
                {t('supplier.hours', { count: hours })}
              </NativeSelectOption>
            ))}
          </SelectField>
          <SelectField
            id='account-sort'
            label={t('supplier.sort')}
            value={filters.sort}
            onChange={(sort) => update({ sort })}
          >
            <NativeSelectOption value='created_at'>
              {t('supplier.createdAt')}
            </NativeSelectOption>
            <NativeSelectOption value='name'>
              {t('supplier.name')}
            </NativeSelectOption>
            <NativeSelectOption value='total_cost'>
              {t('supplier.totalCost')}
            </NativeSelectOption>
            <NativeSelectOption value='today_cost'>
              {t('supplier.todayCost')}
            </NativeSelectOption>
            <NativeSelectOption value='total_requests'>
              {t('supplier.requests')}
            </NativeSelectOption>
            <NativeSelectOption value='total_tokens'>
              {t('supplier.tokens')}
            </NativeSelectOption>
          </SelectField>
          <SelectField
            id='account-direction'
            label={t('supplier.direction')}
            value={filters.direction}
            onChange={(value) =>
              update({ direction: value === 'asc' ? 'asc' : 'desc' })
            }
          >
            <NativeSelectOption value='desc'>
              {t('supplier.desc')}
            </NativeSelectOption>
            <NativeSelectOption value='asc'>
              {t('supplier.asc')}
            </NativeSelectOption>
          </SelectField>
          <SelectField
            id='account-page-size'
            label={t('supplier.pageSize')}
            value={filters.page_size}
            onChange={(value) => update({ page_size: Number(value) })}
          >
            {[20, 50, 100].map((size) => (
              <NativeSelectOption key={size} value={size}>
                {size}
              </NativeSelectOption>
            ))}
          </SelectField>
        </div>
      </section>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={query.refresh}
        hasData={!!query.data}
      >
        <div className='flex flex-wrap items-center justify-between gap-3 border-t pt-3'>
          <p className='text-sm font-medium' role='status' aria-atomic='true'>
            {t('supplier.ui_filtered_accounts')}:{' '}
            {formatNumber(query.data?.total)}
          </p>
          <Freshness
            data={query.data}
            pending={query.isFetching}
            refresh={query.refresh}
          />
        </div>
        {!query.data?.items.length ? (
          <Empty
            message={t(
              filters.search || filters.status || filters.recovery_window
                ? 'supplier.ui_no_account_matches'
                : 'supplier.ui_no_accounts'
            )}
            action={
              canReset ? (
                <Button variant='outline' onClick={reset}>
                  <RotateCcw aria-hidden='true' />
                  {t('supplier.ui_reset_filters')}
                </Button>
              ) : undefined
            }
          />
        ) : (
          <>
            <AccountCards accounts={query.data.items} />
            <div className='hidden min-w-0 md:block'>
              <Table aria-label={t('supplier.ui_filtered_accounts')}>
                <TableHeader>
                  <TableRow>
                    {[
                      'name',
                      'email',
                      'status',
                      'group',
                      'todayCost',
                      'totalCost',
                      'requests',
                      'tokens',
                      'createdAt',
                    ].map((key) => (
                      <TableHead
                        key={key}
                        scope='col'
                        className={
                          [
                            'todayCost',
                            'totalCost',
                            'requests',
                            'tokens',
                          ].includes(key)
                            ? 'text-right'
                            : undefined
                        }
                      >
                        {t(`supplier.${key}`)}
                      </TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {query.data.items.map((account) => (
                    <TableRow key={account.id}>
                      <TableCell
                        className='max-w-56 min-w-36 font-medium break-all whitespace-normal'
                        title={account.name}
                      >
                        {account.name || '--'}
                        <span className='text-muted-foreground mt-1 block font-mono text-xs font-normal break-all'>
                          UUID: {account.id}
                        </span>
                      </TableCell>
                      <TableCell
                        className='max-w-64 min-w-40 break-all whitespace-normal'
                        title={account.email}
                      >
                        {account.email || '--'}
                      </TableCell>
                      <TableCell className='max-w-40 min-w-20 break-all whitespace-normal'>
                        {t(`supplier.${account.status}`, {
                          defaultValue: account.status ?? '--',
                        })}
                      </TableCell>
                      <TableCell className='max-w-48 min-w-32 break-all whitespace-normal'>
                        {account.group_name || '--'}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        <CostValue value={account.today_cost} />
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        <CostValue value={account.total_cost} />
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {formatNumber(account.total_requests)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {formatNumber(account.total_tokens)}
                      </TableCell>
                      <TableCell>
                        <Time value={account.created_at} />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </>
        )}
        <Pagination
          page={filters.page}
          size={filters.page_size}
          total={query.data?.total ?? 0}
          pending={query.isFetching}
          onPage={(page) => setFilters((old) => ({ ...old, page }))}
        />
      </QueryState>
    </div>
  )
}

export function Metric(props: {
  label: string
  value?: ReactNode
  note?: string
}) {
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground mb-1 text-xs [overflow-wrap:anywhere]'>
        {props.label}
      </dt>
      <dd className='text-xl font-semibold [overflow-wrap:anywhere] tabular-nums'>
        {props.value ?? '--'}
        {props.note && (
          <small className='mt-1 block text-xs font-normal text-amber-700 dark:text-amber-400'>
            {props.note}
          </small>
        )}
      </dd>
    </div>
  )
}
