import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

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
import { Empty, Pagination, QueryState, Time } from './common'

export function Audits(props: { supplierId: number }) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const query = useQuery({
    queryKey: ['supplier-admin', props.supplierId, 'audits', page],
    queryFn: () => adminApi.audits(props.supplierId, page),
  })
  return (
    <QueryState
      pending={query.isPending}
      error={query.error}
      retry={() => void query.refetch()}
    >
      {!query.data?.items.length ? (
        <Empty />
      ) : (
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
                <TableCell className='max-w-72 whitespace-normal'>
                  {t(auditLabelKey(item.action))}
                  {item.error_code &&
                    /^[a-zA-Z][a-zA-Z0-9_]{0,95}$/.test(item.error_code) && (
                      <div className='text-muted-foreground mt-1 font-mono text-xs break-all'>
                        {item.error_code}
                      </div>
                    )}
                </TableCell>
                <TableCell>{item.admin_id || '--'}</TableCell>
                <TableCell>{item.binding_id || '--'}</TableCell>
                <TableCell>
                  {item.status_code < 400
                    ? t('supplier.success')
                    : t('supplier.failed')}{' '}
                  ({item.status_code})
                </TableCell>
                <TableCell>{item.duration_ms} ms</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <Pagination
        page={page}
        size={20}
        total={query.data?.total ?? 0}
        onPage={setPage}
        pending={query.isFetching}
      />
    </QueryState>
  )
}
