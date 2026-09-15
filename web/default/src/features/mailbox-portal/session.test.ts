import { afterEach, expect, mock, test } from 'bun:test'

import { MutationObserver, QueryObserver } from '@tanstack/react-query'

import { mailboxApi } from './api'
import {
  acceptSession,
  clearMailboxSession,
  clearMailboxWorkspace,
  mailboxClient,
  sessionOptions,
} from './session'
import type { Session } from './types'

const originalFetch = globalThis.fetch
afterEach(() => {
  globalThis.fetch = originalFetch
  clearMailboxSession()
})
const session: Session = {
  authenticated: true,
  operator: { id: 7, username: 'fixture', display_name: 'Fixture' },
  csrf_token: 'csrf-fixture',
  expires_at: 1000,
}

test('logout clears operator data, mutations and session while preserving no credentials', () => {
  acceptSession(session)
  mailboxClient.setQueryData(['mailbox', 'accounts'], { items: [{ id: 1 }] })
  mailboxClient.setQueryData(sessionOptions.queryKey, session)
  clearMailboxSession()
  expect(mailboxClient.getQueryData(['mailbox', 'accounts'])).toBeUndefined()
  expect(
    mailboxClient.getQueryData<Session>(['mailbox-session'])?.authenticated
  ).toBe(false)
  expect(mailboxClient.getMutationCache().getAll()).toHaveLength(0)
})

test('operator and session rotation invalidate private query data', () => {
  acceptSession(session)
  mailboxClient.setQueryData(['mailbox', 'submissions'], { items: [{ id: 2 }] })
  acceptSession({ ...session, csrf_token: 'new-csrf-fixture' })
  expect(mailboxClient.getQueryData(['mailbox', 'submissions'])).toBeUndefined()
})

function observeSession() {
  mailboxClient.setQueryData(sessionOptions.queryKey, acceptSession(session))
  const observer = new QueryObserver(mailboxClient, {
    ...sessionOptions,
    enabled: false,
  })
  const unsubscribe = observer.subscribe(() => {})
  return { observer, unsubscribe }
}

test('successful logout updates the mounted session observer to signed out and supports signing back in', async () => {
  const { observer, unsubscribe } = observeSession()
  const query = observer.getCurrentQuery()
  globalThis.fetch = mock(async () =>
    Response.json({ success: true, data: {} })
  ) as unknown as typeof fetch
  const logout = new MutationObserver(mailboxClient, {
    mutationFn: () => mailboxApi.logout(session.csrf_token),
    onMutate: clearMailboxWorkspace,
    onSuccess: clearMailboxSession,
  })
  try {
    mailboxClient.setQueryData(['mailbox', 'accounts', 'opening'], {
      items: [{ id: 3 }],
    })
    await logout.mutate()
    expect(observer.getCurrentResult().data).toEqual({ authenticated: false })
    expect(observer.getCurrentResult().error).toBeNull()
    expect(observer.getCurrentQuery()).toBe(query)
    expect(
      mailboxClient.getQueryCache().findAll({ queryKey: ['mailbox'] })
    ).toHaveLength(0)
    expect(mailboxClient.getMutationCache().getAll()).toHaveLength(0)
    mailboxClient.setQueryData(sessionOptions.queryKey, acceptSession(session))
    expect(observer.getCurrentResult().data).toEqual(session)
  } finally {
    unsubscribe()
  }
})

for (const code of ['mailbox_session_expired', 'mailbox_session_revoked']) {
  test(`${code} on a resource updates the mounted observer for the sign-in redirect`, async () => {
    const { observer, unsubscribe } = observeSession()
    globalThis.fetch = mock(async () =>
      Response.json({ success: false, message: code }, { status: 401 })
    ) as unknown as typeof fetch
    try {
      mailboxClient.setQueryData(['mailbox', 'submissions', 'refund'], {
        items: [{ id: 2 }],
      })
      await expect(
        mailboxApi.accounts({ page: 1, page_size: 20 })
      ).rejects.toMatchObject({ code })
      expect(observer.getCurrentResult().data).toEqual({ authenticated: false })
      expect(observer.getCurrentResult().error).toBeNull()
      expect(
        mailboxClient.getQueryCache().findAll({ queryKey: ['mailbox'] })
      ).toHaveLength(0)
    } finally {
      unsubscribe()
    }
  })
}

test('revoked session checks resolve signed out instead of canceling the route guard or blocking the login form', async () => {
  const { observer, unsubscribe } = observeSession()
  globalThis.fetch = mock(async () =>
    Response.json(
      { success: false, message: 'mailbox_session_expired' },
      { status: 401 }
    )
  ) as unknown as typeof fetch
  try {
    expect(await mailboxClient.fetchQuery(sessionOptions)).toEqual({
      authenticated: false,
    })
    expect(observer.getCurrentResult().data).toEqual({ authenticated: false })
    expect(observer.getCurrentResult().error).toBeNull()
  } finally {
    unsubscribe()
  }
})

test('logout failure preserves authentication and settles with an error so the workspace can return', async () => {
  const { observer, unsubscribe } = observeSession()
  globalThis.fetch = mock(async () =>
    Response.json(
      { success: false, message: 'mailbox_request_failed' },
      { status: 500 }
    )
  ) as unknown as typeof fetch
  const logout = new MutationObserver(mailboxClient, {
    mutationFn: () => mailboxApi.logout(session.csrf_token),
    onMutate: clearMailboxWorkspace,
    onSuccess: clearMailboxSession,
  })
  try {
    await expect(logout.mutate()).rejects.toMatchObject({ status: 500 })
    expect(logout.getCurrentResult().isPending).toBe(false)
    expect(logout.getCurrentResult().isError).toBe(true)
    expect(observer.getCurrentResult().data).toEqual(session)
  } finally {
    unsubscribe()
  }
})

test('logout cancels a pending session refresh without replacing its observer or accepting a late authenticated response', async () => {
  const { observer, unsubscribe } = observeSession()
  let resolve!: (response: Response) => void
  globalThis.fetch = mock(
    () =>
      new Promise<Response>((done) => {
        resolve = done
      })
  ) as unknown as typeof fetch
  try {
    const refresh = mailboxClient.fetchQuery(sessionOptions)
    const settled = refresh.catch(() => undefined)
    clearMailboxSession()
    resolve(Response.json({ success: true, data: session }))
    await settled
    expect(observer.getCurrentResult().data).toEqual({ authenticated: false })
    expect(observer.getCurrentResult().error).toBeNull()
    expect(observer.getCurrentResult().fetchStatus).toBe('idle')
  } finally {
    unsubscribe()
  }
})
