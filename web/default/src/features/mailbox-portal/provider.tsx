import { QueryClientProvider } from '@tanstack/react-query'
import { Outlet } from '@tanstack/react-router'

import { mailboxClient } from './session'

export function MailboxProvider() {
  return (
    <QueryClientProvider client={mailboxClient}>
      <Outlet />
    </QueryClientProvider>
  )
}
