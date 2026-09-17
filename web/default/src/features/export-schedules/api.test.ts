import { afterEach, expect, spyOn, test } from 'bun:test'

import { api } from '@/lib/api'

import {
  listSchedules,
  newSchedule,
  parseShanghaiInput,
  shanghaiInput,
  splitRecipients,
} from './api'

let get: ReturnType<typeof spyOn<typeof api, 'get'>> | undefined
afterEach(() => get?.mockRestore())

test('calendar inputs use Beijing time independently of browser timezone', () => {
  const stamp = Date.parse('2026-09-17T01:15:00Z') / 1000
  expect(shanghaiInput(stamp)).toBe('2026-09-17T09:15')
  expect(parseShanghaiInput('2026-09-17T09:15')).toBe(stamp)
  expect(parseShanghaiInput('invalid')).toBe(0)
})

test('new schedule preserves filters and fixed candidates, defaults paused and last30', () => {
  const query = {
    instance_ids: [4],
    dataset: 'inventory' as const,
    preset_days: 30,
    match_mode: 'all' as const,
    rules: [],
    sort_by: 'name',
    sort_order: 'desc',
    include_terms: ['production'],
  }
  const value = newSchedule(query, [{ instance_id: 4, account_id: '12' }])
  expect(value.enabled).toBe(false)
  expect(value.config.period.kind).toBe('last30')
  expect(value.config.scope).toBe('dynamic')
  expect(value.config.query).toEqual(query)
  expect(value.config.accounts).toHaveLength(1)
})

test('recipient splitting deduplicates addresses', () => {
  expect(
    splitRecipients('a@example.test, b@example.test;\na@example.test')
  ).toEqual(['a@example.test', 'b@example.test'])
})

test('server omitted optional arrays remain editable', async () => {
  get = spyOn(api, 'get').mockResolvedValue({
    data: {
      data: {
        total: 1,
        smtp_ready: false,
        items: [{ id: 1, config: { query: { instance_ids: [1] } } }],
      },
    },
  })
  const result = await listSchedules()
  expect(result.items[0].config.accounts).toEqual([])
  expect(result.items[0].config.query.rules).toEqual([])
})
