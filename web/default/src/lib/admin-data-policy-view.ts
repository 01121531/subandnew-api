import type { AdminDataFieldKey } from './admin-data-policy'

type AdminDataRecord = Record<string, unknown>
export type AdminDataDefinition = {
  key: string
  label: string
  fields: AdminDataFieldKey[]
  paths?: string[]
}

export function adminDataValue(row: unknown, ...paths: string[]): unknown {
  for (const path of paths) {
    let value = row
    for (const key of path.split('.')) {
      value =
        value && typeof value === 'object'
          ? (value as AdminDataRecord)[key]
          : undefined
    }
    if (value !== undefined && value !== null) return value
  }
  return undefined
}

export function formatAdminDataValue(value: unknown): string {
  if (value == null || value === '') return '--'
  if (typeof value === 'number') {
    return Number.isFinite(value)
      ? value.toLocaleString(undefined, { maximumFractionDigits: 8 })
      : '--'
  }
  if (typeof value === 'string') return value
  return '--'
}

export function visibleAdminDefinitions(
  definitions: AdminDataDefinition[],
  canView: (field: AdminDataFieldKey) => boolean
) {
  return definitions.filter((definition) => definition.fields.every(canView))
}

export function sortAdminDataRows<T>(
  rows: T[],
  key: string,
  direction: 'asc' | 'desc',
  value: (row: T, key: string) => unknown
): T[] {
  return [...rows].sort((left, right) => {
    const a = value(left, key)
    const b = value(right, key)
    if (a == null) return b == null ? 0 : 1
    if (b == null) return -1
    const comparison =
      typeof a === 'number' && typeof b === 'number'
        ? a - b
        : String(a).localeCompare(String(b), undefined, { numeric: true })
    return direction === 'asc' ? comparison : -comparison
  })
}

export function adminDataPagination(
  result:
    | {
        total?: number
        total_is_exact?: boolean
        has_more?: boolean
        page?: number
      }
    | undefined,
  page: number,
  pageSize: number,
  showCount: boolean
) {
  const exactTotal =
    showCount &&
    result?.total_is_exact === true &&
    typeof result.total === 'number'
      ? result.total
      : undefined
  return {
    exactTotal,
    hasMore:
      result?.page !== undefined && result.page !== page
        ? false
        : (result?.has_more ??
          (exactTotal !== undefined && page * pageSize < exactTotal)),
  }
}
