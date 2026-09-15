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
const clearListeners = new Set<() => void>()

export function onMailboxWorkspaceClear(listener: () => void): () => void {
  clearListeners.add(listener)
  return () => {
    clearListeners.delete(listener)
  }
}

export function clearMailboxWorkspace(): void {
  cancelMailboxRequests()
  for (const listener of clearListeners) listener()
  void mailboxClient.cancelQueries({ queryKey: ['mailbox'] })
  mailboxClient.removeQueries({ queryKey: ['mailbox'] })
  mailboxClient.getMutationCache().clear()
}

export function clearMailboxSession(): void {
  identity = null
  clearMailboxWorkspace()
  // Keep the query that mounted useQuery observers are subscribed to.
  void mailboxClient.cancelQueries({
    queryKey: ['mailbox-session'],
    exact: true,
  })
  mailboxClient.setQueryData(['mailbox-session'], { authenticated: false })
}
onAuthFailure(clearMailboxSession)

export function acceptSession(session: Session): Session {
  const next = session.authenticated
    ? `${session.operator.id}:${session.csrf_token}`
    : null
  if (identity !== next || !session.authenticated) {
    clearMailboxWorkspace()
  }
  identity = next
  return session
}

export const sessionOptions = queryOptions({
  queryKey: ['mailbox-session'],
  queryFn: async ({ signal }) =>
    acceptSession(await mailboxApi.session(signal)),
})
