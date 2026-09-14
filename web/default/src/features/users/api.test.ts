import { afterEach, expect, spyOn, test } from 'bun:test'

import { api } from '@/lib/api'

import { getPermissionCatalog } from './api'

let request: ReturnType<typeof spyOn<typeof api, 'get'>> | undefined
afterEach(() => request?.mockRestore())

test('permission catalog retains backend data field definitions', async () => {
  const fields = [{ key: 'amount', label_key: 'adminData.fields.amount' }]
  request = spyOn(api, 'get').mockResolvedValueOnce({
    data: {
      success: true,
      data: { resources: [], roles: [], data_fields: fields },
    },
  })
  expect((await getPermissionCatalog()).data_fields).toEqual(fields)
  expect(request).toHaveBeenCalledWith('/api/authz/catalog')
})

test('legacy catalog has no implicit data grants', async () => {
  request = spyOn(api, 'get').mockResolvedValueOnce({
    data: {
      success: true,
      data: { resources: [], roles: [] },
    },
  })
  expect((await getPermissionCatalog()).data_fields).toEqual([])
})
