import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { adminApi } from '../admin-api'
import { auditLabelKey } from '../lib/display'
import { policyGroups } from '../lib/permissions'
import type { Audit } from '../types'
import { Empty, Pagination, QueryState, Time } from './common'

export function Audits(props: { supplierId: number }) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const query = useQuery({
    queryKey: ['supplier-admin', props.supplierId, 'audits', page],
    queryFn: () => adminApi.audits(props.supplierId, page),
  })
  const result = (item: Audit) => (
    <Badge variant={item.status_code < 400 ? 'secondary' : 'destructive'}>
      {item.status_code < 400 ? t('supplier.success') : t('supplier.failed')} (
      {item.status_code})
    </Badge>
  )
  const errorCode = (item: Audit) =>
    item.error_code &&
    /^[a-zA-Z][a-zA-Z0-9_]{0,95}$/.test(item.error_code) && (
      <div className='text-muted-foreground mt-1 font-mono text-xs break-all'>
        {item.error_code}
      </div>
    )
  return (
    <div className='grid min-w-0 gap-4'>
      <QueryState
        pending={query.isPending}
        error={query.error}
        hasData={query.data !== undefined}
        retry={() => void query.refetch()}
      >
        {!query.data?.items.length ? (
          <Empty />
        ) : (
          <>
            <div className='grid gap-3 md:hidden'>
              {query.data.items.map((item) => (
                <article
                  key={item.id}
                  className='min-w-0 rounded-md border p-3'
                >
                  <div className='flex flex-wrap items-start justify-between gap-2'>
                    <h3 className='min-w-0 flex-1 text-sm font-semibold [overflow-wrap:anywhere]'>
                      {t(auditLabelKey(item.action))}
                    </h3>
                    {result(item)}
                  </div>
                  {errorCode(item)}
                  <PolicyChanges value={item.policy_changes} />
                  <NamingChanges value={item.naming_changes} />
                  <dl className='mt-3 grid grid-cols-2 gap-3 text-xs'>
                    <div className='col-span-2 min-w-0'>
                      <dt className='text-muted-foreground'>
                        {t('supplier.createdAt')}
                      </dt>
                      <dd className='mt-1'>
                        <Time value={item.created_at} />
                      </dd>
                    </div>
                    {item.ip_address && (
                      <div className='col-span-2 min-w-0'>
                        <dt className='text-muted-foreground'>IP</dt>
                        <dd className='mt-1 break-all'>{item.ip_address}</dd>
                      </div>
                    )}
                    <div>
                      <dt className='text-muted-foreground'>
                        {t('supplier.admin')}
                      </dt>
                      <dd className='mt-1 break-all'>
                        {item.admin_id || '--'}
                      </dd>
                    </div>
                    <div>
                      <dt className='text-muted-foreground'>
                        {t('supplier.binding')}
                      </dt>
                      <dd className='mt-1 break-all'>
                        {item.binding_id || '--'}
                      </dd>
                    </div>
                    <div className='col-span-2'>
                      <dt className='text-muted-foreground'>
                        {t('supplier.duration')}
                      </dt>
                      <dd className='mt-1'>{item.duration_ms} ms</dd>
                    </div>
                  </dl>
                </article>
              ))}
            </div>
            <div className='hidden md:block'>
              <Table>
                <TableHeader>
                  <TableRow>
                    {[
                      'createdAt',
                      'auditAction',
                      'admin',
                      'binding',
                      'result',
                      'duration',
                    ].map((key) => (
                      <TableHead key={key}>{t(`supplier.${key}`)}</TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {query.data.items.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell>
                        <Time value={item.created_at} />
                        {item.ip_address && (
                          <div className='text-muted-foreground mt-1 max-w-48 text-xs break-all whitespace-normal'>
                            IP: {item.ip_address}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className='max-w-72 [overflow-wrap:anywhere] whitespace-normal'>
                        {t(auditLabelKey(item.action))}
                        {errorCode(item)}
                        <PolicyChanges value={item.policy_changes} />
                        <NamingChanges value={item.naming_changes} />
                      </TableCell>
                      <TableCell>{item.admin_id || '--'}</TableCell>
                      <TableCell>{item.binding_id || '--'}</TableCell>
                      <TableCell>{result(item)}</TableCell>
                      <TableCell>{item.duration_ms} ms</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </>
        )}
        <Pagination
          page={page}
          size={20}
          total={query.data?.total ?? 0}
          onPage={setPage}
          pending={query.isFetching}
        />
      </QueryState>
    </div>
  )
}

function PolicyChanges(props: { value?: string }) {
  const { t } = useTranslation()
  let changes: Record<
    string,
    { before?: boolean | null; after?: boolean | null }
  > = {}
  try {
    const value = JSON.parse(props.value ?? '{}')
    if (value && typeof value === 'object') changes = value
  } catch {
    return null
  }
  const keys = policyGroups
    .flatMap((group) => [...group.keys])
    .filter(
      (key) =>
        (changes[key] &&
          ['boolean', 'undefined'].includes(typeof changes[key].before)) ||
        changes[key]?.before === null
    )
  if (!keys.length) return null
  const label = (value: unknown) => {
    if (value === true) return t('supplier.policyAllow')
    if (value === false) return t('supplier.policyDeny')
    return t('supplier.policyInherit')
  }
  return (
    <details className='mt-2 text-xs'>
      <summary className='cursor-pointer font-medium'>
        {t('supplier.permissions')}
      </summary>
      <dl className='mt-2 grid gap-2'>
        {keys.map((key) => (
          <div key={key}>
            <dt>{t(`supplier.policyField_${key.replaceAll('.', '_')}`)}</dt>
            <dd>
              {label(changes[key].before)} → {label(changes[key].after)}
            </dd>
          </div>
        ))}
      </dl>
    </details>
  )
}

function NamingChanges({ value }: { value?: string }) {
  const { t } = useTranslation()
  if (!value) return null
  let changes: Record<string, unknown>
  try {
    changes = JSON.parse(value)
    if (!changes || typeof changes !== 'object') return null
  } catch {
    return null
  }
  const label = (rule: unknown) => {
    if (rule === null) return t('supplier.namingInherit')
    if (!rule || typeof rule !== 'object') return '--'
    const { prefix, suffix } = rule as Record<string, unknown>
    if (typeof prefix !== 'string' || typeof suffix !== 'string') return '--'
    return `${t('supplier.naming_prefix')}: ${prefix || t('supplier.none')}; ${t('supplier.naming_suffix')}: ${suffix || t('supplier.none')}`
  }
  return (
    <details className='mt-2 text-xs [overflow-wrap:anywhere]'>
      <summary className='cursor-pointer font-medium'>
        {t('supplier.namingTitle')}
      </summary>
      <dl className='mt-2 grid gap-2'>
        <div>
          <dt>{t('supplier.namingBefore')}</dt>
          <dd>{label(changes.before)}</dd>
        </div>
        <div>
          <dt>{t('supplier.namingAfter')}</dt>
          <dd>{label(changes.after)}</dd>
        </div>
      </dl>
    </details>
  )
}
