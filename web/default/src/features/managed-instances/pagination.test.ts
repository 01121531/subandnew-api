import { describe, expect, test } from 'bun:test'

import { fetchAllManagedInstances } from './pagination'
import type { ApiResponse, ManagedInstance, ManagedInstanceList } from './types'

function page(
  ids: number[],
  metadata: Partial<ManagedInstanceList> = {}
): ApiResponse<ManagedInstanceList> {
  return {
    success: true,
    message: '',
    data: {
      items: ids.map(
        (id) => ({ id, name: `instance-${id}` }) as ManagedInstance
      ),
      page: 1,
      page_size: 100,
      ...metadata,
    },
  }
}

describe('authorized instance pagination', () => {
  test('uses has_more without reconstructing a redacted total', async () => {
    const pages = [
      page([9, 4], { has_more: true }),
      page([7], { has_more: false }),
    ]
    const requests: number[] = []
    const result = await fetchAllManagedInstances(async (index, size) => {
      requests.push(index)
      expect(size).toBe(100)
      return pages[index - 1]
    })
    expect(requests).toEqual([1, 2])
    expect(result.data.items.map((item) => item.id)).toEqual([9, 4, 7])
    expect(result.data).not.toHaveProperty('total')
    expect(result.data.has_more).toBe(false)
  })

  test('falls back to page length for older servers with no count or has_more', async () => {
    const ids = Array.from({ length: 200 }, (_, index) => 201 - index)
    const requests: number[] = []
    const result = await fetchAllManagedInstances(async (index, size) => {
      requests.push(index)
      return page(ids.slice((index - 1) * size, index * size))
    })
    expect(requests).toEqual([1, 2, 3])
    expect(result.data.items.map((item) => item.id)).toEqual(ids)
    expect(result.data.total).toBeUndefined()
  })

  test('stops on explicit has_more=false even for a full page and retains known total', async () => {
    let calls = 0
    const result = await fetchAllManagedInstances(async () => {
      calls += 1
      return page(
        Array.from({ length: 100 }, (_, index) => index + 1),
        { total: 100, has_more: false }
      )
    })
    expect(calls).toBe(1)
    expect(result.data.total).toBe(100)
  })

  test('empty restricted results have no fabricated count', async () => {
    const result = await fetchAllManagedInstances(async () =>
      page([], { has_more: false })
    )
    expect(result.data.items).toEqual([])
    expect(result.data).not.toHaveProperty('total')
  })

  test('deduplicates overlapping pages while preserving server order', async () => {
    const pages = [
      page([3, 2], { has_more: true }),
      page([2, 1], { has_more: false }),
    ]
    const result = await fetchAllManagedInstances(
      async (index) => pages[index - 1]
    )
    expect(result.data.items.map((item) => item.id)).toEqual([3, 2, 1])
  })

  test('does not return a partial list after a later page fails', async () => {
    await expect(
      fetchAllManagedInstances(async (index) =>
        index === 1
          ? page([1], { has_more: true })
          : { ...page([]), success: false, message: 'Access changed' }
      )
    ).rejects.toThrow('Access changed')
  })

  test('rejects a server that keeps returning the same unfinished page', async () => {
    await expect(
      fetchAllManagedInstances(async () => page([1], { has_more: true }))
    ).rejects.toThrow('Instance pagination did not advance')
  })
})
