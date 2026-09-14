import type { ApiResponse, ManagedInstanceList } from './types'

// Counts may be redacted independently of the authorized instance identities.
export async function fetchAllManagedInstances(
  fetchPage: (
    page: number,
    pageSize: number
  ) => Promise<ApiResponse<ManagedInstanceList>>
): Promise<ApiResponse<ManagedInstanceList>> {
  const pageSize = 100
  const items: ManagedInstanceList['items'] = []
  const seen = new Set<number>()
  let first: ApiResponse<ManagedInstanceList> | undefined
  for (let page = 1; ; page += 1) {
    const response = await fetchPage(page, pageSize)
    if (!response.success) {
      throw new Error(response.message || 'Unable to load instances')
    }
    first ??= response
    const data = response.data
    const fresh = data.items.filter((item) => {
      if (seen.has(item.id)) return false
      seen.add(item.id)
      return true
    })
    items.push(...fresh)
    const hasMore = data.has_more ?? data.items.length >= pageSize
    if (!hasMore) {
      return {
        ...first,
        data: {
          ...first.data,
          items,
          page: 1,
          page_size: items.length,
          has_more: false,
        },
      }
    }
    if (fresh.length === 0) {
      throw new Error('Instance pagination did not advance')
    }
  }
}
