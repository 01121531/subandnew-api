import { afterEach, expect, test } from 'bun:test'

import { QueryClient } from '@tanstack/react-query'
import { AxiosError } from 'axios'

import { useAuthStore } from '@/stores/auth-store'

import { installAdminAuthCacheBoundary } from './admin-auth-cache'
import { createDefaultAdminDataPolicy } from './admin-data-policy'
import { api } from './api'

const principal = {
  id: 2,
  username: 'admin',
  role: 10,
  admin_data_policy: createDefaultAdminDataPolicy(),
}
let dispose: (() => void) | undefined
afterEach(() => {
  dispose?.()
  useAuthStore.getState().auth.reset()
})

test('policy and function changes clear private cache and cancel pending responses', async () => {
  useAuthStore.getState().auth.setUser(principal)
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  dispose = installAdminAuthCacheBoundary(client)
  client.setQueryData(['private'], { amount: 999 })
  let finish: (value: unknown) => void = () => {}
  const pending = client
    .fetchQuery({
      queryKey: ['pending-private'],
      queryFn: () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    })
    .catch(() => undefined)
  useAuthStore.getState().auth.setUser({
    ...principal,
    permissions: { admin_permissions: { managed_instance: { view: false } } },
  })
  finish({ amount: 999 })
  await pending
  expect(client.getQueryCache().getAll()).toHaveLength(0)
  client.setQueryData(['private'], { email: 'private@example.com' })
  useAuthStore.getState().auth.setUser({
    ...principal,
    admin_data_policy: { ...principal.admin_data_policy, revision: 2 },
  })
  expect(client.getQueryCache().getAll()).toHaveLength(0)
})

test('authorization-changed 401 clears cache and signs out even with local error handling', async () => {
  useAuthStore.getState().auth.setUser(principal)
  const client = new QueryClient()
  let signedOut = false
  dispose = installAdminAuthCacheBoundary(client, () => {
    signedOut = true
  })
  client.setQueryData(['private'], { amount: 999 })
  await expect(
    api.get('/api/user/self', {
      disableDuplicate: true,
      skipErrorHandler: true,
      adapter: async (config) => {
        throw new AxiosError('changed', 'ERR_BAD_REQUEST', config, undefined, {
          config,
          status: 401,
          statusText: 'Unauthorized',
          headers: {},
          data: { message: 'admin_authorization_changed' },
        })
      },
    })
  ).rejects.toThrow('changed')
  expect(useAuthStore.getState().auth.user).toBeNull()
  expect(client.getQueryCache().getAll()).toHaveLength(0)
  expect(signedOut).toBe(true)
})

test('an old private response cannot complete after authorization changes', async () => {
  useAuthStore.getState().auth.setUser(principal)
  await expect(
    api.get('/api/private', {
      disableDuplicate: true,
      adapter: async (config) => {
        useAuthStore.getState().auth.reset()
        return {
          config,
          status: 200,
          statusText: 'OK',
          headers: {},
          data: { amount: 999 },
        }
      },
    })
  ).rejects.toThrow('Authorization changed')
})
