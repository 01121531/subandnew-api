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
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { LongText } from '@/components/long-text'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Checkbox } from '@/components/ui/checkbox'
import { formatTimestamp } from '@/lib/format'

import {
  USER_ROLES,
  USER_STATUS,
  USER_STATUSES,
  isUserDeleted,
} from '../constants'
import type { User } from '../types'
import { DataTableRowActions } from './data-table-row-actions'

export function useUsersColumns(): ColumnDef<User>[] {
  const { t } = useTranslation()
  return [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={table.getIsAllPageRowsSelected()}
          indeterminate={table.getIsSomePageRowsSelected()}
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          aria-label={t('Select all')}
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label={t('Select row')}
        />
      ),
      enableSorting: false,
      enableHiding: false,
      size: 40,
    },
    {
      accessorKey: 'id',
      header: t('ID'),
      cell: ({ row }) => (
        <TableId value={row.original.id} className='w-[60px] text-sm' />
      ),
      size: 80,
    },
    {
      accessorKey: 'username',
      header: t('Username'),
      cell: ({ row }) => (
        <div className='flex min-w-[180px] flex-col gap-1'>
          <LongText className='max-w-[180px] font-medium'>
            {row.original.username}
          </LongText>
          {row.original.display_name !== row.original.username && (
            <LongText className='text-muted-foreground max-w-[180px] text-xs'>
              {row.original.display_name}
            </LongText>
          )}
        </div>
      ),
      enableHiding: false,
      size: 220,
      meta: { mobileTitle: true },
    },
    {
      accessorKey: 'email',
      header: t('Email'),
      cell: ({ row }) => (
        <LongText className='max-w-[220px]'>
          {row.original.email || '-'}
        </LongText>
      ),
      size: 240,
    },
    {
      accessorKey: 'status',
      header: t('Status'),
      cell: ({ row }) => {
        const config = isUserDeleted(row.original)
          ? USER_STATUSES[USER_STATUS.DELETED]
          : USER_STATUSES[row.original.status as keyof typeof USER_STATUSES]
        return config ? (
          <StatusBadge
            label={t(config.labelKey)}
            variant={config.variant}
            copyable={false}
          />
        ) : null
      },
      filterFn: (row, id, value) => value.includes(String(row.getValue(id))),
      size: 120,
    },
    {
      accessorKey: 'role',
      header: t('Role'),
      cell: ({ row }) => {
        const config = USER_ROLES[row.original.role as keyof typeof USER_ROLES]
        return config ? (
          <span className='text-sm'>{t(config.labelKey)}</span>
        ) : (
          '-'
        )
      },
      filterFn: (row, id, value) => value.includes(String(row.getValue(id))),
      size: 120,
    },
    {
      accessorKey: 'created_at',
      header: t('Created At'),
      cell: ({ row }) => (
        <span className='text-muted-foreground text-sm'>
          {row.original.created_at
            ? formatTimestamp(row.original.created_at)
            : '-'}
        </span>
      ),
      size: 180,
      meta: { mobileHidden: true },
    },
    {
      accessorKey: 'last_login_at',
      header: t('Last Login'),
      cell: ({ row }) => (
        <span className='text-muted-foreground text-sm'>
          {row.original.last_login_at
            ? formatTimestamp(row.original.last_login_at)
            : '-'}
        </span>
      ),
      size: 180,
      meta: { mobileHidden: true },
    },
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => <DataTableRowActions row={row} />,
      meta: { pinned: 'right' as const },
    },
  ]
}
