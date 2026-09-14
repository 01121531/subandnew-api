import type { ComponentProps, ReactNode } from 'react'

import { useAdminDataAccess } from '@/hooks/use-admin-data-access'
import type { AdminDataFieldKey } from '@/lib/admin-data-policy'

import { TableCell, TableHead } from './ui/table'

type FieldProps = {
  fields?: AdminDataFieldKey[]
  anyFields?: AdminDataFieldKey[]
}

function useVisible(props: FieldProps) {
  const access = useAdminDataAccess()
  return (
    (props.fields ?? []).every(access.canView) &&
    (!props.anyFields || props.anyFields.some(access.canView))
  )
}

export function AdminDataField(props: FieldProps & { children: ReactNode }) {
  return useVisible(props) ? props.children : null
}

export function AdminTableCell({
  fields,
  anyFields,
  ...props
}: ComponentProps<typeof TableCell> & FieldProps) {
  return useVisible({ fields, anyFields }) ? <TableCell {...props} /> : null
}

export function AdminTableHead({
  fields,
  anyFields,
  ...props
}: ComponentProps<typeof TableHead> & FieldProps) {
  return useVisible({ fields, anyFields }) ? <TableHead {...props} /> : null
}
