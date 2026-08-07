import { createFileRoute, redirect } from '@tanstack/react-router'

import { ManagedInstanceDetail } from '@/features/managed-instances/detail'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/instances/$id')({
  beforeLoad: () => {
    const user = useAuthStore.getState().auth.user
    if (
      !hasPermission(
        user,
        ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
        ADMIN_PERMISSION_ACTIONS.VIEW
      )
    ) {
      throw redirect({ to: '/403' })
    }
  },
  component: InstanceDetailRoute,
})

function InstanceDetailRoute() {
  const { id } = Route.useParams()
  return <ManagedInstanceDetail instanceId={Number(id)} />
}
