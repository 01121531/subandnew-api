import { createFileRoute, redirect } from '@tanstack/react-router'

import { isAuthFailure } from '@/features/mailbox-portal/api'
import { QueryState } from '@/features/mailbox-portal/components/common'
import { MailboxPortal } from '@/features/mailbox-portal/portal'
import {
  mailboxClient,
  sessionOptions,
} from '@/features/mailbox-portal/session'

export const Route = createFileRoute('/mailbox/')({
  beforeLoad: async () => {
    const session = await mailboxClient
      .fetchQuery(sessionOptions)
      .catch((error: unknown) => {
        if (isAuthFailure(error)) throw redirect({ to: '/mailbox/sign-in' })
        throw error
      })
    if (!session.authenticated) throw redirect({ to: '/mailbox/sign-in' })
  },
  component: MailboxPortal,
  errorComponent: (props) => (
    <QueryState pending={false} error={props.error} retry={props.reset} />
  ),
})
