import { useMemo } from 'react'

import {
  ADMIN_DATA_FIELD_KEYS,
  adminDataAuthorizationKey,
  canViewAdminDataField,
  canViewAdminInstance,
  visibleAdminDataKey,
} from '@/lib/admin-data-policy'
import { useAuthStore } from '@/stores/auth-store'

export function useAdminDataAccess() {
  const user = useAuthStore((state) => state.auth.user)
  return useMemo(
    () => ({
      key: adminDataAuthorizationKey(user),
      full: ADMIN_DATA_FIELD_KEYS.every((field) =>
        canViewAdminDataField(user, field)
      ),
      canView: (field: (typeof ADMIN_DATA_FIELD_KEYS)[number]) =>
        canViewAdminDataField(user, field),
      allows: (key: string) => visibleAdminDataKey(user, key),
      hasInstance: (id: number) => canViewAdminInstance(user, id),
    }),
    [user]
  )
}
