import { describe, expect, test } from 'bun:test'

import { buildCostTrend, isCostTrendPartial } from './cost-trend'

describe('Claude Gateway cost trend', () => {
  const day = 24 * 60 * 60
  const start = Date.parse('2026-08-28T00:00:00+08:00') / 1000
  const points = [
    { timestamp: start, today_cost: 2.5, today_cost_complete: true },
    { timestamp: start + day, today_cost: 0, today_cost_complete: true },
    { timestamp: start + 2 * day, today_cost: 3.25, today_cost_complete: true },
  ]

  test('keeps daily real zero values', () => {
    expect(buildCostTrend(points, 'daily')).toEqual([
      { date: '2026-08-28', value: 2.5 },
      { date: '2026-08-29', value: 0 },
      { date: '2026-08-30', value: 3.25 },
    ])
  })

  test('builds a running total in the selected range', () => {
    expect(
      buildCostTrend(points, 'cumulative').map((point) => point.value)
    ).toEqual([2.5, 2.5, 5.75])
  })

  test('does not treat missing samples as zero', () => {
    const incomplete = [
      points[0],
      { timestamp: start + day, today_cost: null, today_cost_complete: false },
    ]
    expect(buildCostTrend(incomplete, 'daily')).toHaveLength(1)
    expect(isCostTrendPartial(incomplete, start, start + day)).toBe(true)
  })
})
