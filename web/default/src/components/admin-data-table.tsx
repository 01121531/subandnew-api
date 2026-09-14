import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { useAdminDataAccess } from '@/hooks/use-admin-data-access'
import type { AdminDataFieldKey } from '@/lib/admin-data-policy'

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from './ui/table'

export type AdminDataColumn<T> = {
  key: string
  label: ReactNode
  fields?: AdminDataFieldKey[]
  render: (row: T) => ReactNode
}

export function AdminDataTable<T>(props: {
  rows: T[]
  rowKey: (row: T, index: number) => string
  columns: AdminDataColumn<T>[]
}) {
  const { t } = useTranslation()
  const access = useAdminDataAccess()
  const columns = props.columns.filter((column) =>
    (column.fields ?? []).every(access.canView)
  )
  return (
    <div className='max-w-full overflow-x-auto'>
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((column) => (
              <TableHead key={column.key}>{column.label}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.rows.length === 0 && (
            <TableRow>
              <TableCell colSpan={Math.max(1, columns.length)}>
                {t('No results.')}
              </TableCell>
            </TableRow>
          )}
          {props.rows.map((row, index) => (
            <TableRow key={props.rowKey(row, index)}>
              {columns.map((column) => (
                <TableCell key={column.key} className='max-w-64 break-words'>
                  {column.render(row)}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
