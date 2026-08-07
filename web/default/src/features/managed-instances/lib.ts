import type { ManagedInstance, ManagedInstanceStatus } from './types'

export const MANAGED_INSTANCE_KINDS = [
  { value: 'new_api', label: 'New API' },
  { value: 'huichuan', label: 'HUICHUAN-AI' },
  { value: 'sub2api', label: 'Sub2API' },
  { value: 'generic', label: 'Generic' },
] as const

export const MANAGED_INSTANCE_STATUSES: ManagedInstanceStatus[] = [
  'healthy',
  'degraded',
  'offline',
  'auth_failed',
  'unknown',
]

export function formatTimestamp(value: number): string {
  if (!value) return '-'
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'short',
    timeStyle: 'short',
  }).format(new Date(value * 1000))
}

export function statusCounts(instances: ManagedInstance[]) {
  return instances.reduce(
    (counts, instance) => {
      counts.total += 1
      counts[instance.status] += 1
      return counts
    },
    {
      total: 0,
      healthy: 0,
      degraded: 0,
      offline: 0,
      auth_failed: 0,
      unknown: 0,
    }
  )
}
