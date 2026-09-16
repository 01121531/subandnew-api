import { afterEach, describe, expect, mock, test } from 'bun:test'

import {
  cancelMailboxRequests,
  mailboxApi,
  onAccountFailure,
  onAuthFailure,
} from './api'
import { readCredential } from './lib/credentials'
import {
  clearMailboxSession,
  clearMailboxWorkspace,
  mailboxClient,
  onMailboxWorkspaceClear,
  sessionOptions,
} from './session'
import type { Account, AccountType, Session } from './types'

const originalFetch = globalThis.fetch
const opening: Account = {
  id: 3,
  account_type: 'opening',
  card_last4: '4242',
  email: 'opening@example.test',
  version: 1,
  assignment_id: 4,
  assignment_version: 8,
  credentials_available: true,
  status: 'pending',
}
const card = {
  card_number: '4242424242424242',
  card_expiry: '12/30',
  server_time: 100,
}
const cvv = {
  cvv: '987',
  expires_at: 190,
  server_time: 100,
}
const hidden = {
  ...card,
  cvv: '987',
  password: 'private-password',
  code: '123456',
}
const page = (items: unknown[]) => ({
  items,
  total: items.length,
  page: 1,
  page_size: 20,
  has_more: false,
})

afterEach(() => {
  globalThis.fetch = originalFetch
  clearMailboxSession()
})

describe('independent account pools', () => {
  for (const accountType of ['refund', 'opening'] as const) {
    test(`${accountType} scopes every resource request and strips card data from ordinary DTOs`, async () => {
      const calls: { path: string; init?: RequestInit }[] = []
      const record = { ...opening, account_type: accountType, ...hidden }
      globalThis.fetch = mock(
        async (url: RequestInfo | URL, init?: RequestInit) => {
          const parsed = new URL(String(url), 'https://fixture.test')
          expect(parsed.searchParams.get('account_type')).toBe(accountType)
          expect(init?.cache).toBe('no-store')
          calls.push({ path: parsed.pathname, init })
          if (parsed.pathname === '/mailbox-api/v1/attachments/image') {
            return new Response('fixture', {
              headers: { 'Content-Type': 'image/png' },
            })
          }
          let data: unknown = record
          if (
            parsed.pathname.endsWith('/accounts') ||
            parsed.pathname.endsWith('/submissions')
          ) {
            data = { ...page([record]), ...hidden }
          }
          if (parsed.pathname.endsWith('/credentials')) data = hidden
          return Response.json({ success: true, data })
        }
      ) as unknown as typeof fetch
      const query = { account_type: accountType, page: 1, page_size: 20 }
      const ordinary = [
        await mailboxApi.accounts(query),
        await mailboxApi.account(3, undefined, accountType),
        await mailboxApi.submissions(query),
        await mailboxApi.upload(
          'csrf',
          4,
          new File(['image'], 'x.png'),
          undefined,
          accountType
        ),
        await mailboxApi.submit(
          'csrf',
          4,
          8,
          ['image'],
          undefined,
          accountType
        ),
      ]
      for (const data of ordinary) {
        const json = JSON.stringify(data)
        for (const key of Object.keys(hidden)) {
          expect(json).not.toContain(`"${key}"`)
        }
      }
      const credential = await mailboxApi.credentials(
        'csrf',
        3,
        'card',
        undefined,
        accountType
      )
      expect(credential).toEqual(card)
      expect(JSON.parse(String(calls.at(-1)?.init?.body))).toEqual({
        kind: 'card',
      })
      await mailboxApi.attachment('image', undefined, accountType)
      expect(calls).toHaveLength(7)
    })
  }

  test('missing legacy types default to refund, never opening', async () => {
    const legacy = {
      ...opening,
      account_type: undefined,
      card_last4: card.card_number,
    }
    globalThis.fetch = mock(async () =>
      Response.json({ success: true, data: legacy })
    ) as unknown as typeof fetch
    expect(await mailboxApi.account(3)).toMatchObject({
      account_type: 'refund',
      card_last4: '',
    })
    await expect(
      mailboxApi.account(3, undefined, 'opening')
    ).rejects.toMatchObject({ code: 'mailbox_assignment_changed' })
  })

  test('mixed or wrong pool responses fail closed for account and history lists', async () => {
    for (const accountType of ['refund', 'opening'] as const) {
      const other: AccountType = accountType === 'refund' ? 'opening' : 'refund'
      globalThis.fetch = mock(async () =>
        Response.json({
          success: true,
          data: page([
            { ...opening, account_type: accountType },
            { ...opening, id: 9, account_type: other },
          ]),
        })
      ) as unknown as typeof fetch
      for (const list of [mailboxApi.accounts, mailboxApi.submissions]) {
        await expect(
          list({ account_type: accountType, page: 1, page_size: 20 })
        ).rejects.toMatchObject({ code: 'mailbox_assignment_changed' })
      }
    }
  })

  test('invalid last4 fields cannot expose full card numbers through ordinary queries', async () => {
    globalThis.fetch = mock(async () =>
      Response.json({
        success: true,
        data: { ...opening, card_last4: card.card_number },
      })
    ) as unknown as typeof fetch
    expect((await mailboxApi.account(3, undefined, 'opening')).card_last4).toBe(
      ''
    )
  })
})

describe('card reauthorization and clearing', () => {
  test('opening accounts can request CVV without caching the secret', async () => {
    const calls: string[] = []
    globalThis.fetch = mock(async (url: RequestInfo | URL) => {
      const path = new URL(String(url), 'https://fixture.test')
      calls.push(path.pathname)
      return Response.json({
        success: true,
        data: path.pathname.endsWith('/credentials') ? cvv : opening,
      })
    }) as unknown as typeof fetch
    expect(
      await readCredential('csrf', opening, 'cvv', new AbortController().signal)
    ).toEqual(cvv)
    expect(calls).toEqual([
      '/mailbox-api/v1/accounts/3',
      '/mailbox-api/v1/accounts/3/credentials',
      '/mailbox-api/v1/accounts/3',
    ])
    expect(
      mailboxClient.getQueryCache().findAll({ queryKey: ['mailbox'] })
    ).toHaveLength(0)
  })

  test('every reveal checks ownership before and after a fresh, uncached credential request', async () => {
    const calls: string[] = []
    globalThis.fetch = mock(async (url: RequestInfo | URL) => {
      const path = new URL(String(url), 'https://fixture.test')
      calls.push(path.pathname)
      expect(path.searchParams.get('account_type')).toBe('opening')
      return Response.json({
        success: true,
        data: path.pathname.endsWith('/credentials') ? card : opening,
      })
    }) as unknown as typeof fetch
    for (let i = 0; i < 2; i += 1) {
      expect(
        await readCredential(
          'csrf',
          opening,
          'card',
          new AbortController().signal
        )
      ).toEqual(card)
    }
    expect(calls).toEqual(
      Array(2)
        .fill([
          '/mailbox-api/v1/accounts/3',
          '/mailbox-api/v1/accounts/3/credentials',
          '/mailbox-api/v1/accounts/3',
        ])
        .flat()
    )
    expect(
      mailboxClient.getQueryCache().findAll({ queryKey: ['mailbox'] })
    ).toHaveLength(0)
    expect(mailboxClient.getMutationCache().getAll()).toHaveLength(0)
  })

  for (const changed of [
    { status: 'approved' as const },
    { assignment_id: 0, status: 'unassigned' as const },
    { assignment_id: 99 },
    { assignment_version: 9 },
    { credentials_available: false },
    { account_type: 'refund' as const },
  ]) {
    for (const phase of ['before', 'after'] as const) {
      test(`${JSON.stringify(changed)} blocks card ${phase} retrieval and clears consumers`, async () => {
        let calls = 0
        let cleared = false
        const unsubscribe = onAccountFailure(() => {
          cleared = true
        })
        globalThis.fetch = mock(async () => {
          calls += 1
          let data: unknown = opening
          if (calls === 2) data = card
          if (calls === (phase === 'before' ? 1 : 3)) {
            data = { ...opening, ...changed }
          }
          return Response.json({ success: true, data })
        }) as unknown as typeof fetch
        try {
          await expect(
            readCredential(
              'csrf',
              opening,
              'card',
              new AbortController().signal
            )
          ).rejects.toBeInstanceOf(Error)
          expect(calls).toBe(phase === 'before' ? 1 : 3)
          expect(cleared).toBe(true)
        } finally {
          unsubscribe()
        }
      })
    }
  }

  test('refund accounts never request a card credential', async () => {
    const fetch = mock(async () => Response.json({ success: true, data: card }))
    globalThis.fetch = fetch as unknown as typeof globalThis.fetch
    await expect(
      readCredential(
        'csrf',
        { ...opening, account_type: 'refund' },
        'card',
        new AbortController().signal
      )
    ).rejects.toMatchObject({ code: 'mailbox_credentials_revoked' })
    expect(fetch).not.toHaveBeenCalled()
  })

  for (const clear of [clearMailboxWorkspace, clearMailboxSession]) {
    test(`${clear.name} clears both pools and rejects late cards even if fetch ignores abort`, async () => {
      let resolve!: (response: Response) => void
      globalThis.fetch = mock(
        () =>
          new Promise<Response>((done) => {
            resolve = done
          })
      ) as unknown as typeof fetch
      const authenticated: Session = {
        authenticated: true,
        operator: { id: 7, username: 'fixture', display_name: 'Fixture' },
        csrf_token: 'fixture-csrf',
        expires_at: 1000,
      }
      mailboxClient.setQueryData(sessionOptions.queryKey, authenticated)
      for (const type of ['refund', 'opening']) {
        mailboxClient.setQueryData(
          ['mailbox', 'accounts', type],
          page([opening])
        )
        mailboxClient.setQueryData(
          ['mailbox', 'submissions', type],
          page([opening])
        )
      }
      let cleared = false
      const unsubscribe = onMailboxWorkspaceClear(() => {
        cleared = true
      })
      try {
        const pending = mailboxApi.credentials(
          'csrf',
          3,
          'card',
          undefined,
          'opening'
        )
        clear()
        resolve(Response.json({ success: true, data: card }))
        await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
        expect(cleared).toBe(true)
        expect(
          mailboxClient.getQueryCache().findAll({ queryKey: ['mailbox'] })
        ).toHaveLength(0)
        expect(
          mailboxClient.getQueryData(sessionOptions.queryKey)?.authenticated
        ).toBe(clear === clearMailboxWorkspace)
      } finally {
        unsubscribe()
      }
    })
  }

  test('a late failure from the old pool cannot log out or invalidate the new pool', async () => {
    let resolve!: (response: Response) => void
    globalThis.fetch = mock(
      () =>
        new Promise<Response>((done) => {
          resolve = done
        })
    ) as unknown as typeof fetch
    let failed = false
    const unsubscribe = onAuthFailure(() => {
      failed = true
    })
    try {
      const pending = mailboxApi.credentials(
        'csrf',
        3,
        'card',
        undefined,
        'opening'
      )
      cancelMailboxRequests()
      resolve(
        Response.json(
          { success: false, message: 'mailbox_session_expired' },
          { status: 401 }
        )
      )
      await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
      expect(failed).toBe(false)
    } finally {
      unsubscribe()
    }
  })
})
