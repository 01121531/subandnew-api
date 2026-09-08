import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

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
import { formatCost } from '../lib/usage'
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
  return (
    <div className='grid min-w-0 gap-5'>
      <QueryState
        pending={summary.isPending}
        error={summary.error}
        retry={summary.refresh}
      >
        <dl className='grid grid-cols-3 gap-3 border-y py-4'>
          <Metric
            label={t('supplier.totalAccounts')}
            value={summary.data?.total_accounts}
          />
          <Metric
            label={t('supplier.availableAccounts')}
            value={summary.data?.available_accounts}
          />
          <Metric label={t('supplier.rpm')} value={summary.data?.rpm} />
        </dl>
        {(summary.data?.pool_rpm !== undefined ||
          summary.data?.pool_concurrent !== undefined ||
          summary.data?.pool_available_accounts !== undefined) && (
          <dl className='grid grid-cols-3 gap-3 border-b pb-4'>
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
        <Freshness
          data={summary.data}
          pending={summary.isFetching}
          refresh={summary.refresh}
        />
      </QueryState>
      <div className='grid grid-cols-2 gap-3 lg:grid-cols-4'>
        <Field id='account-search' label={t('supplier.search')}>
          <Input
            id='account-search'
            type='search'
            value={searchDraft}
            onChange={(event) => setSearchDraft(event.target.value)}
          />
        </Field>
        <SelectField
          id='account-status'
          label={t('supplier.status')}
          value={filters.status}
          onChange={(status) => update({ status })}
        >
          <NativeSelectOption value=''>{t('supplier.all')}</NativeSelectOption>
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
          <NativeSelectOption value=''>{t('supplier.all')}</NativeSelectOption>
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
      </div>
      <div className='flex flex-wrap items-end justify-between gap-3'>
        <div className='flex gap-3'>
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
        {!query.data?.items.length ? (
          <Empty />
        ) : (
          <>
            <AccountCards accounts={query.data.items} />
            <div className='hidden min-w-0 md:block'>
              <Table>
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
                      <TableHead key={key}>{t(`supplier.${key}`)}</TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {query.data.items.map((account) => (
                    <TableRow key={account.id}>
                      <TableCell
                        className='max-w-56 truncate font-medium'
                        title={account.name}
                      >
                        {account.name}
                      </TableCell>
                      <TableCell
                        className='max-w-64 truncate'
                        title={account.email}
                      >
                        {account.email || '--'}
                      </TableCell>
                      <TableCell>
                        {t(`supplier.${account.status}`, {
                          defaultValue: account.status ?? '--',
                        })}
                      </TableCell>
                      <TableCell>{account.group_name || '--'}</TableCell>
                      <TableCell className='tabular-nums'>
                        {formatCost(account.today_cost)}
                      </TableCell>
                      <TableCell className='tabular-nums'>
                        {formatCost(account.total_cost)}
                      </TableCell>
                      <TableCell className='tabular-nums'>
                        {formatNumber(account.total_requests)}
                      </TableCell>
                      <TableCell className='tabular-nums'>
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
  value?: number | string
  note?: string
}) {
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground mb-1 text-xs'>{props.label}</dt>
      <dd className='text-2xl font-semibold break-words tabular-nums'>
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
