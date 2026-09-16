import { describe, expect, test } from 'bun:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createInstance } from 'i18next'
import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'

import { DraftSubmission } from '@/features/mailbox-portal/components/draft-submission'
import { History } from '@/features/mailbox-portal/components/history'
import type { Account } from '@/features/mailbox-portal/types'
import { mailboxEn, mailboxZh } from '@/i18n/mailbox'
import { mailboxPortalEn, mailboxPortalZh } from '@/i18n/mailbox-portal'

import { MailboxSubmissionRemark } from './mailbox-submission-remark'

const remark = `<script>alert("fixture")</script>\n${'long-word'.repeat(200)}`
const escapedRemark = `&lt;script&gt;alert(&quot;fixture&quot;)&lt;/script&gt;\n${'long-word'.repeat(200)}`

describe('submission remark rendering', () => {
  for (const [language, mailbox, portal] of [
    ['en', mailboxEn, mailboxPortalEn],
    ['zhCN', mailboxZh, mailboxPortalZh],
  ] as const) {
    async function render(
      children: ReactNode,
      client = new QueryClient()
    ): Promise<string> {
      const i18n = createInstance()
      await i18n.init({
        lng: language,
        resources: {
          [language]: { translation: { mailbox, mailboxPortal: portal } },
        },
      })
      return renderToStaticMarkup(
        <I18nextProvider i18n={i18n}>
          <QueryClientProvider client={client}>{children}</QueryClientProvider>
        </I18nextProvider>
      )
    }
    test(`${language} shared history and review text is full, wrapped, multiline and escaped`, async () => {
      for (const labelKey of [
        'mailbox.admin.submissionRemark',
        'mailboxPortal.remark',
      ] as const) {
        const html = await render(
          <MailboxSubmissionRemark remark={remark} labelKey={labelKey} />
        )
        expect(html).toContain(escapedRemark)
        expect(html).toContain('whitespace-pre-wrap')
        expect(html).toContain('[overflow-wrap:anywhere]')
        expect(html).not.toContain('<script>')
        expect(html).not.toContain('truncate')
        expect(html).not.toContain('line-clamp')
        expect(html).not.toContain(labelKey)
        expect(
          await render(<MailboxSubmissionRemark labelKey={labelKey} />)
        ).toBe('')
      }
    })
    for (const accountType of ['refund', 'opening'] as const) {
      test(`${language} ${accountType} portal history shows remarks without attachments and accepts legacy records`, async () => {
        const client = new QueryClient()
        client.setQueryData(
          ['mailbox', 'submissions', accountType, '', '', 1],
          {
            items: [
              {
                id: 1,
                email: 'remark@example.test',
                remark,
                status: 'pending',
                attachments: [],
              },
              {
                id: 2,
                email: 'legacy@example.test',
                status: 'pending',
                attachments: [],
              },
            ],
            page: 1,
            total: 2,
            page_size: 20,
            has_more: false,
          }
        )
        const html = await render(<History accountType={accountType} />, client)
        expect(html).toContain(portal.remark)
        expect(html).toContain(escapedRemark)
        expect(html).toContain('legacy@example.test')
        expect(html).not.toContain('<img')
        client.clear()
      })
      test(`${language} ${accountType} normal drafts show an optional secure remark; issue description stays separate`, async () => {
        const account: Account = {
          id: 1,
          account_type: accountType,
          email: 'fixture@example.test',
          version: 1,
          assignment_id: 2,
          assignment_version: 3,
          status: 'pending',
          credentials_available: true,
        }
        const leave = { current: { dirty: false, pending: false } }
        const normal = await render(
          <DraftSubmission
            account={account}
            csrf='fixture'
            leave={leave}
            onSubmitted={() => {}}
          />
        )
        expect(normal).toContain(portal.remarkOptional)
        expect(normal).toContain(portal.remarkWarning)
        expect(normal).toContain('autoComplete="off"')
        expect(normal).not.toContain('maxLength="2000"')
        expect(normal).not.toContain(mailbox.issues.description)
        const issue = await render(
          <DraftSubmission
            issue
            account={account}
            csrf='fixture'
            leave={leave}
            onSubmitted={() => {}}
          />
        )
        expect(issue).not.toContain(portal.remarkOptional)
        expect(issue).toContain(mailbox.issues.description)
        expect(issue).toContain('maxLength="2000"')
      })
    }
  }
})
