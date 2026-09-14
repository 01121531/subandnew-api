import type { QueryClient } from '@tanstack/react-query'

import { useAuthStore } from '@/stores/auth-store'

import { adminDataAuthorizationKey } from './admin-data-policy'

export function installAdminAuthCacheBoundary(
  queryClient: QueryClient,
  onSignedOut?: () => void
): () => void {
  return useAuthStore.subscribe((state, previous) => {
    if (
      adminDataAuthorizationKey(state.auth.user) ===
      adminDataAuthorizationKey(previous.auth.user)
    ) {
      return
    }
    // clear also cancels in-flight queries, so a pre-revocation response cannot repopulate the cache.
    queryClient.clear()
    if (typeof window !== 'undefined') {
      window.sessionStorage.removeItem('managed-account-api-draft')
    }
    if (!state.auth.user) onSignedOut?.()
  })
}
