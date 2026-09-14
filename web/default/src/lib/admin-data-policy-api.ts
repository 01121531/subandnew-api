import type { ManagedInstance } from '@/features/managed-instances/types'

import { api } from './api'

// Preserve the global server order, including when the count is redacted.
export async function getAdminDataInstances(
  signal?: AbortSignal
): Promise<ManagedInstance[]> {
  const items: ManagedInstance[] = []
  const seen = new Set<number>()
  for (let page = 1; ; page += 1) {
    const response = await api.get<{
      success: boolean
      message: string
      data: { items: ManagedInstance[]; has_more?: boolean; total?: number }
    }>('/api/managed-instances', {
      params: { page, page_size: 100 },
      signal,
      disableDuplicate: true,
    })
    if (!response.data.success) throw new Error(response.data.message)
    const data = response.data.data
    const fresh = data.items.filter((item) => !seen.has(item.id))
    fresh.forEach((item) => seen.add(item.id))
    items.push(...fresh)
    if (data.has_more === false || fresh.length === 0) return items
    if (
      data.has_more === undefined &&
      (data.items.length < 100 ||
        (data.total !== undefined && items.length >= data.total))
    ) {
      return items
    }
  }
}
