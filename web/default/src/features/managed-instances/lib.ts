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
import type { ManagedInstance } from './types'

export const MANAGED_INSTANCE_KINDS = [
  { value: 'new_api', label: 'New API' },
  { value: 'huichuan', label: 'HUICHUAN-AI' },
  { value: 'sub2api', label: 'Sub2API' },
  { value: 'generic', label: 'Generic' },
] as const

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
