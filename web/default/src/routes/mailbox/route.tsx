import { createFileRoute } from '@tanstack/react-router'

import { MailboxProvider } from '@/features/mailbox-portal/provider'

export const Route = createFileRoute('/mailbox')({ component: MailboxProvider })
