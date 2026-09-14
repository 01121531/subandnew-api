import { afterEach, expect, test } from 'bun:test'

import {
  acceptSession,
  clearMailboxSession,
  mailboxClient,
  sessionOptions,
} from './session'
import type { Session } from './types'

afterEach(clearMailboxSession)
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
