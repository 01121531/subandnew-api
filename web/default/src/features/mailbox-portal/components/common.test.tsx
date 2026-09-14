import { describe, expect, test } from 'bun:test'

import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'

import { mailboxPortalZh } from '@/i18n/mailbox-portal'

import { Status, Time } from './common'

describe('mailbox account and history timestamps', () => {
  for (const [language, locale] of [
    ['zhCN', 'zh-CN'],
    ['zhTW', 'zh-TW'],
    ['en', 'en'],
  ] as const) {
    test(`renders timestamps with interface language ${language} without crashing the portal`, async () => {
      const i18n = createInstance()
      await i18n.init({ lng: language, resources: {}, fallbackLng: false })
      const timestamp = 1789395082
      const date = new Date(timestamp * 1000)
      const html = renderToStaticMarkup(
        <I18nextProvider i18n={i18n}>
          <Time value={timestamp} />
        </I18nextProvider>
      )
      expect(html).toContain(`dateTime="${date.toISOString()}"`)
      expect(html).toContain(date.toLocaleString(locale))
    })
  }
})

test('pending submissions are awaiting review while pending accounts remain pending work', async () => {
  const i18n = createInstance()
  await i18n.init({
    lng: 'zhCN',
    resources: { zhCN: { translation: { mailboxPortal: mailboxPortalZh } } },
    fallbackLng: false,
  })
  const account = renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <Status status='pending' />
    </I18nextProvider>
  )
  const submission = renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <Status submission status='pending' />
    </I18nextProvider>
  )
  expect(account).toContain(mailboxPortalZh.status_pending)
  expect(account).not.toContain(mailboxPortalZh.submissionPending)
  expect(submission).toContain(mailboxPortalZh.submissionPending)
  expect(submission).not.toContain(mailboxPortalZh.status_pending)
})
