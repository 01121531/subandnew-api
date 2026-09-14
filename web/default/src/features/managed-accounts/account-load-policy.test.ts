import { expect, test } from 'bun:test'

import { accountLoadFields } from './account-load-policy'

test('partial load headings only name fields displayed in their cells', () => {
  const allowed = new Set(['requests', 'tokens', 'rpm'])
  const canView = (field: string) => allowed.has(field)
  for (const claudeGateway of [false, true]) {
    expect(accountLoadFields(false, claudeGateway, canView)).toEqual([
      'requests',
      'tokens',
    ])
  }
  expect(accountLoadFields(true, false, canView)).toEqual(['rpm'])
})

test('rates alone do not leave an empty non-conductor load column', () => {
  const ratesOnly = (field: string) => field === 'rates'
  expect(accountLoadFields(false, false, ratesOnly)).toEqual([])
  expect(accountLoadFields(false, true, ratesOnly)).toEqual([])
  expect(accountLoadFields(true, false, ratesOnly)).toEqual(['rates'])
  expect(accountLoadFields(false, true, () => false)).toEqual([])
})
