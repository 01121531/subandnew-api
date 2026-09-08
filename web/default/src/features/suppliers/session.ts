import { QueryCache, QueryClient, queryOptions } from '@tanstack/react-query'

import { sessionExpired } from './lib/errors'
import { portalApi } from './portal-api'
import type { Session } from './types'

let currentSupplierId: number | null = null

// This cache never shares console credentials, auth redirects, or supplier data with the console client.
export const supplierClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: 15_000, refetchOnWindowFocus: false },
    mutations: { retry: false, gcTime: 0 },
  },
  queryCache: new QueryCache({
    onError: (error) => {
      if (sessionExpired(error)) {
        clearSupplierSession()
      }
    },
    onSuccess: (data, query) => {
      if (query.queryKey[0] !== 'supplier-session') return
      const session = data as Session
      const id = session.authenticated ? session.supplier.id : null
      if (id !== currentSupplierId || !session.authenticated) {
        void supplierClient.cancelQueries({ queryKey: ['supplier'] })
        supplierClient.removeQueries({ queryKey: ['supplier'] })
        supplierClient.getMutationCache().clear()
      }
      currentSupplierId = id
    },
  }),
})
export const sessionOptions = queryOptions({
  queryKey: ['supplier-session'],
  queryFn: ({ signal }) => portalApi.session(signal),
  staleTime: 0,
  refetchOnWindowFocus: true,
  refetchInterval: 60_000,
})

export function clearSupplierSession(): void {
  currentSupplierId = null
  void supplierClient.cancelQueries()
  supplierClient.removeQueries({ queryKey: ['supplier'] })
  supplierClient.setQueryData(sessionOptions.queryKey, { authenticated: false })
  supplierClient.getMutationCache().clear()
}
