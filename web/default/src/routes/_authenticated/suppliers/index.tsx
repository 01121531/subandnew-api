import { createFileRoute, redirect } from '@tanstack/react-router'

import { Suppliers } from '@/features/suppliers'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/suppliers/')({
  beforeLoad: () => {
    if (!hasPermission(useAuthStore.getState().auth.user, 'supplier', 'view')) {
      throw redirect({ to: '/403' })
    }
  },
  component: Suppliers,
})
