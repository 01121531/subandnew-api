import { describe, expect, test } from 'bun:test'

import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'

import { supplierEn, supplierZh } from '@/i18n/supplier'

import { PolicyContext } from '../lib/permissions'
import type { Account } from '../types'
import { AccountCards } from './account-cards'
import { AccountRuntime } from './account-runtime'
import { AccountStatus } from './account-status'

const account: Account = {
  id: 'synthetic-account',
  name: 'Test account',
  email: '',
  status: 'active',
  created_at: '',
  group_name: '',
  total_cost: 0,
  today_cost: 0,
  total_requests: 0,
  total_tokens: 0,
  health_status: 'error',
  failure_kind: 'account_proxy_failure',
  last_error: 'Bound outbound proxy is unhealthy',
  cooldown: true,
  cooldown_reason: 'account_proxy_failure: upstream_header_timeout:240s',
  cooldown_remaining_seconds: 5,
  rpm: 0,
  active_sessions: 300,
  max_sessions: 300,
  max_rpm: 1000,
}

async function render(
  row: Account,
  allowed = true,
  mobile = false,
  language = 'zhCN'
) {
  const i18n = createInstance()
  await i18n.init({
    lng: language,
    resources: {
      zhCN: { translation: { supplier: supplierZh } },
      en: { translation: { supplier: supplierEn } },
    },
  })
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <PolicyContext.Provider
        value={{
          values: Object.fromEntries(
            ['status', 'rpm', 'tpm', 'concurrent', 'active_sessions'].map(
              (key) => [`account.${key}`, allowed]
            )
          ),
          sources: {},
          version: 'test',
        }}
      >
        {mobile ? (
          <AccountCards accounts={[row]} />
        ) : (
          <>
            <AccountStatus account={row} />
            <AccountRuntime account={row} />
          </>
        )}
      </PolicyContext.Provider>
    </I18nextProvider>
  )
}

describe('supplier account diagnostics and metrics', () => {
  test('desktop and mobile show both proxy error and cooldown reason, and runtime values', async () => {
    for (const mobile of [false, true]) {
      const html = await render(account, true, mobile)
      expect(html).toContain('Bound outbound proxy is unhealthy')
      expect(html).toContain(
        'account_proxy_failure: upstream_header_timeout:240s'
      )
      expect(html).toContain('异常')
      expect(html).toContain('冷却中')
      expect(html).toContain('采集时剩余 5 秒')
      expect(html).toContain('活跃会话')
      expect(html).toContain('上限 300')
      expect(html).toContain('RPM')
      expect(html).toContain('>0<')
      expect(html).toContain('--')
    }
  })
  test('hidden permissions hide reasons, values and limits in both layouts', async () => {
    for (const mobile of [false, true]) {
      const html = await render(account, false, mobile)
      for (const text of [
        'Bound outbound',
        'upstream_header_timeout',
        'account_proxy_failure',
        'RPM',
        '活跃会话',
        '300',
        '1,000',
      ]) {
        expect(html).not.toContain(text)
      }
    }
  })
  test('missing diagnostic is explicit, zero is real, inactive cooldown is not displayed', async () => {
    expect(
      await render({
        ...account,
        last_error: null,
        cooldown_reason: null,
        cooldown_remaining_seconds: 0,
      })
    ).toContain('采集时剩余 0 秒')
    expect(
      await render({ ...account, last_error: null, cooldown_reason: null })
    ).toContain('上游未提供具体异常原因')
    const html = await render(
      {
        ...account,
        health_status: null,
        last_error: null,
        failure_kind: null,
        cooldown: false,
      },
      true,
      false,
      'en'
    )
    expect(html).not.toContain('Cooldown reason')
    expect(html).not.toContain('upstream_header_timeout')
    expect(html).not.toContain('No detailed error')
  })
  test('upstream text remains plain text and duplicate reasons display once', async () => {
    const error = '<img src=x onerror=alert(1)>'
    const html = await render({
      ...account,
      last_error: error,
      cooldown_reason: error,
    })
    expect(html).not.toContain('<img')
    expect(html.split('&lt;img')).toHaveLength(2)
  })
})
