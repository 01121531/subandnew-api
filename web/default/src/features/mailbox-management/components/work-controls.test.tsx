import { beforeAll, describe, expect, test } from 'bun:test'

import { createInstance } from 'i18next'
import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'

import { mailboxEn, mailboxZh } from '@/i18n/mailbox'

import type { WorkAccount, WorkSummary } from '../types'
import { WorkAccountFacts } from './work-accounts'
import {
  WorkCount,
  WorkDateFilter,
  WorkSummaryView,
  WorkTime,
} from './work-controls'

const locales = { en: createInstance(), zh: createInstance() }
beforeAll(async () => {
  for (const language of ['en', 'zh'] as const) {
    await locales[language].init({
      lng: language,
      resources: {
        [language]: {
          translation: { mailbox: language === 'en' ? mailboxEn : mailboxZh },
        },
      },
      interpolation: { escapeValue: false },
    })
  }
})
function render(children: ReactNode, language: 'en' | 'zh' = 'en') {
  return renderToStaticMarkup(
    <I18nextProvider i18n={locales[language]}>{children}</I18nextProvider>
  )
}
const summary: WorkSummary = {
  submitted_accounts: 5,
  refund_submitted: 2,
  opening_submitted: 3,
  submission_count: 19,
  issue_accounts: 4,
  current: {
    pending: 8,
    submitted: 3,
    approved: 1,
    rejected: 2,
    issue_pending: 6,
  },
}
const account: WorkAccount = {
  id: 8,
  account_type: 'opening',
  email: 'fixture@example.test',
  card_last4: '4242',
  archived_at: 4,
  assignment_id: 12,
  assignment_version: 3,
  status: 'rejected',
  assignment_active: false,
  assigned_at: 1,
  revoked_at: 3,
  submission_count: 9,
  last_submitted_at: 0,
  last_issue_at: 0,
}

describe('operator statistics presentation', () => {
  test('history timestamps use Beijing midnight rather than the browser timezone', () => {
    const value = Date.UTC(2026, 8, 15, 16, 5) / 1000
    expect(render(<WorkTime value={value} />)).toContain(
      '9/16/2026, 12:05:00 AM'
    )
    expect(render(<WorkTime value={0} />)).toBe('<span>--</span>')
  })
  test('each count identifies both operator and metric, including zero', () => {
    const markup = render(
      <WorkCount
        operatorName='Operator A'
        metric='submitted_accounts'
        summary={summary}
        onSelect={() => {}}
      />
    )
    expect(markup).toContain(
      'aria-label="Operator A · Submitted accounts (distinct): 5"'
    )
    expect(markup).toContain('>5</button>')
    const zero = render(
      <WorkCount
        operatorName='Operator B'
        metric='current_issue_pending'
        summary={{
          ...summary,
          current: { ...summary.current, issue_pending: 0 },
        }}
        onSelect={() => {}}
      />,
      'zh'
    )
    expect(zero).toContain(
      'aria-label="Operator B · 异常待处理（不限日期）: 0"'
    )
    expect(zero).toContain('>0</button>')
  })
  test('missing opt-in summaries are not presented as zero counts or clickable results', () => {
    const markup = render(
      <WorkCount
        operatorName='Operator'
        metric='submitted_accounts'
        onSelect={() => {}}
      />
    )
    expect(markup).toBe('<span>--</span>')
  })
  test('summary distinguishes current tasks from selected-period activity in both languages', () => {
    for (const language of ['en', 'zh'] as const) {
      const labels = language === 'en' ? mailboxEn : mailboxZh
      const markup = render(
        <WorkSummaryView
          operatorName='Operator A'
          summary={summary}
          range={{
            period: 'all',
            start_at: 0,
            end_at: 0,
            timezone: 'Asia/Shanghai',
          }}
          onSelect={() => {}}
        />,
        language
      )
      expect(markup).toContain(labels.work.summary)
      expect(markup).toContain(labels.work.current)
      expect(markup).toContain(labels.work.submissionCount)
      expect(markup).toContain('Asia/Shanghai')
      expect(markup).toContain(labels.work.periods.all)
      expect(markup).not.toContain('mailbox.work.')
    }
  })
  test('date filter defaults to all time and invalid custom ranges cannot be applied', () => {
    const all = render(
      <WorkDateFilter value={{ period: 'all' }} onChange={() => {}} />
    )
    expect(all).toContain('value="all" selected=""')
    expect(all).toContain('aria-label="Statistics period"')
    const invalid = render(
      <WorkDateFilter
        value={{
          period: 'custom',
          start_date: '2026-09-16',
          end_date: '2026-09-01',
        }}
        onChange={() => {}}
      />
    )
    expect(invalid).toContain('role="alert"')
    expect(invalid).toContain('disabled=""')
    expect(invalid).toContain('End date (inclusive)')
  })
  test('account facts label lifetime statistics and preserve archived, revoked, and no-event states', () => {
    const markup = render(<WorkAccountFacts account={account} />)
    expect(markup).toContain(mailboxEn.work.lifetimeSubmissions)
    expect(markup).toContain(mailboxEn.work.lastSubmitted)
    expect(markup).toContain(mailboxEn.work.lastIssue)
    expect(markup).toContain(mailboxEn.work.latestAssignment)
    expect(markup).toContain(mailboxEn.work.inactive)
    expect(markup).toContain(mailboxEn.archive.archived)
    expect(markup).toContain('4242')
    expect(markup.match(/<span>--<\/span>/g)).toHaveLength(2)
  })
})
