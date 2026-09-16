import { expect, test } from 'bun:test'

import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'

import { mailboxPortalEn } from '@/i18n/mailbox-portal'

import type { Account } from '../types'
import { Credentials } from './credentials'
import { MaskedCard } from './masked-card'

const account: Account = {
  id: 3,
  account_type: 'opening',
  card_last4: '4242',
  email: 'fixture@example.test',
  version: 1,
  assignment_id: 4,
  assignment_version: 8,
  status: 'pending',
  credentials_available: true,
}
const unexpectedCardFields = {
  card_number: '4242424242424242',
  card_expiry: '12/30',
}

test('initial detail render exposes no card fields and refund has no card control', async () => {
  const i18n = createInstance()
  await i18n.init({
    lng: 'en',
    resources: { en: { translation: { mailboxPortal: mailboxPortalEn } } },
  })
  for (const accountType of ['refund', 'opening'] as const) {
    const html = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <Credentials
          account={{
            ...account,
            account_type: accountType,
            ...unexpectedCardFields,
          }}
          csrf='fixture'
        />
      </I18nextProvider>
    )
    expect(html).not.toContain('4242424242424242')
    expect(html).not.toContain('12/30')
    expect(html.includes(mailboxPortalEn.cardNumber)).toBe(
      accountType === 'opening'
    )
    expect(html.includes(mailboxPortalEn.cardExpiry)).toBe(
      accountType === 'opening'
    )
    expect(html).not.toContain(mailboxPortalEn.reveal)
    expect(html.includes(mailboxPortalEn.cvv)).toBe(accountType === 'opening')
  }
  const approved = renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <Credentials
        account={{ ...account, status: 'approved' }}
        csrf='fixture'
      />
    </I18nextProvider>
  )
  expect(approved).toContain(mailboxPortalEn.credentialsBlocked)
  expect(approved).not.toContain('<button')
})

test('list masking only renders exactly four numeric digits', async () => {
  const i18n = createInstance()
  await i18n.init({
    lng: 'en',
    resources: { en: { translation: { mailboxPortal: mailboxPortalEn } } },
  })
  const render = (last4: string) =>
    renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <MaskedCard last4={last4} />
      </I18nextProvider>
    )
  expect(render('4242')).toContain('**** 4242')
  expect(render('4242424242424242')).not.toContain('4242')
  expect(render('CVV 987')).not.toContain('987')
})
