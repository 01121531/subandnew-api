import { QueryClient, queryOptions } from '@tanstack/react-query'

import { cancelMailboxRequests, mailboxApi, onAuthFailure } from './api'
import type { Session } from './types'

export const mailboxClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
      staleTime: 0,
      gcTime: 0,
      refetchOnWindowFocus: true,
    },
    mutations: { retry: false, gcTime: 0 },
  },
})

let identity: string | null = null
export function clearMailboxSession(): void {
  identity = null
  cancelMailboxRequests()
  mailboxClient.clear()
  mailboxClient.setQueryData(['mailbox-session'], { authenticated: false })
}
onAuthFailure(clearMailboxSession)

export function acceptSession(session: Session): Session {
  const next = session.authenticated
    ? `${session.operator.id}:${session.csrf_token}`
    : null
  if (identity !== next || !session.authenticated) {
    void mailboxClient.cancelQueries({ queryKey: ['mailbox'] })
    mailboxClient.removeQueries({ queryKey: ['mailbox'] })
    mailboxClient.getMutationCache().clear()
  }
  identity = next
  return session
}

export const sessionOptions = queryOptions({
  queryKey: ['mailbox-session'],
  queryFn: async ({ signal }) =>
    acceptSession(await mailboxApi.session(signal)),
})
