import { expect, test } from 'bun:test'

import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'

import { mailboxPortalEn, mailboxPortalZh } from '@/i18n/mailbox-portal'

import { MailboxAssistantDownload } from './mailbox-assistant-download'

test('download entry is translated and isolates the external tab from the operator session', async () => {
  for (const [lng, resource] of [
    ['zhCN', mailboxPortalZh],
    ['en', mailboxPortalEn],
  ] as const) {
    const i18n = createInstance()
    await i18n.init({
      lng,
      resources: { [lng]: { translation: { mailboxPortal: resource } } },
    })
    const html = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <MailboxAssistantDownload />
      </I18nextProvider>
    )
    expect(html).toContain(resource.downloadAssistant)
    expect(html).toContain('target="_blank"')
    expect(html).toContain('rel="noopener noreferrer"')
    expect(html).toContain('referrerPolicy="no-referrer"')
    expect(html).toContain(
      'https://github.com/01121531/subandnew-api/releases/'
    )
    expect(html).not.toContain('csrf')
  }
})
