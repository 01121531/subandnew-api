import { createFileRoute, redirect } from '@tanstack/react-router'

import { MailboxManagement } from '@/features/mailbox-management'
import { canAccessMailbox } from '@/features/mailbox-management/lib/permissions'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/mailbox-management/')({
  beforeLoad: () => {
    if (!canAccessMailbox(useAuthStore.getState().auth.user)) {
      throw redirect({ to: '/403' })
    }
  },
  component: MailboxManagement,
})
