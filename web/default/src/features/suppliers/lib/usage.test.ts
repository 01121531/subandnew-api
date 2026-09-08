import { describe, expect, test } from 'bun:test'

import { aggregateUsage, formatCost } from './usage'

describe('supplier usage completeness', () => {
  test('empty and all-null series do not fabricate zero totals', () => {
    expect(aggregateUsage([]).cost).toEqual({ value: null, partial: false })
    expect(
      aggregateUsage([{ cost: null, requests: null, tokens: null }]).requests
    ).toEqual({ value: null, partial: false })
    expect(formatCost(null)).toBe('--')
  })
  test('real zeros remain complete observed zero values', () => {
    expect(aggregateUsage([{ cost: 0, requests: 0, tokens: 0 }]).cost).toEqual({
      value: 0,
      partial: false,
    })
    expect(formatCost(0)).toBe('USD 0')
  })
  test('each metric independently marks partial observations', () => {
    const result = aggregateUsage([
      { cost: 0.00000001, requests: 0, tokens: null },
      { cost: null, requests: 2, tokens: 100 },
    ])
    expect(result.cost).toEqual({ value: 0.00000001, partial: true })
    expect(result.requests).toEqual({ value: 2, partial: false })
    expect(result.tokens).toEqual({ value: 100, partial: true })
    expect(formatCost(result.cost.value)).toBe('USD 0.00000001')
  })
  test('nonfinite values are missing observations', () => {
    expect(
      aggregateUsage([{ cost: Number.NaN, requests: 1, tokens: 0 }]).cost.value
    ).toBeNull()
    expect(formatCost(Number.POSITIVE_INFINITY)).toBe('--')
  })
})
