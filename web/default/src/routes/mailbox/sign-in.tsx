import { createFileRoute } from '@tanstack/react-router'

import { MailboxSignIn } from '@/features/mailbox-portal/sign-in'

export const Route = createFileRoute('/mailbox/sign-in')({
  component: MailboxSignIn,
})
