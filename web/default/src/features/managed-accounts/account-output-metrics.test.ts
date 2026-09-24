import { expect, test } from 'bun:test'

import {
  accountOutputTotals,
  formatOutputAmount,
} from './account-output-metrics'

test('redacted amount, currency, and status keep request and token totals usable', () => {
  const totals = accountOutputTotals([{ total_requests: 12, total_tokens: 34 }])
  expect(totals.requests).toBe(12)
  expect(totals.tokens).toBe(34)
  expect(totals.amount).toBeNull()
  expect(totals.average).toBeNull()
  expect(formatOutputAmount(totals.amount, totals.currency)).toBe('--')
  expect(formatOutputAmount(0, undefined)).toBe('--')
  expect(formatOutputAmount(undefined, 'USD')).toBe('--')
  expect(formatOutputAmount(Number.NaN, 'USD')).toBe('--')
})

test('real zero is retained but missing observations never become zero', () => {
  expect(
    accountOutputTotals([{ amount: 0, currency: 'USD', total_requests: 0 }])
      .amount
  ).toBe(0)
  expect(accountOutputTotals([{}]).requests).toBeNull()
  expect(accountOutputTotals([]).amount).toBeNull()
  expect(
    accountOutputTotals([{ amount: 10, currency: 'USD' }, { currency: 'USD' }])
      .amount
  ).toBe(10)
  const partial = accountOutputTotals([
    { total_requests: 12, requests_available: true },
    { total_requests: 0, requests_available: false },
  ])
  expect(partial.requests).toBe(12)
  expect(partial.missingRequests).toBe(1)
  expect(formatOutputAmount(0, 'USD')).not.toBe('--')
})
