import type { UsageMetrics } from '../types'

export function aggregateUsage(days: UsageMetrics[] | undefined) {
  const result = {
    cost: { value: null as number | null, partial: false },
    requests: { value: null as number | null, partial: false },
    tokens: { value: null as number | null, partial: false },
  }
  for (const metric of ['cost', 'requests', 'tokens'] as const) {
    let total = 0
    let count = 0
    for (const day of days ?? []) {
      const value = day[metric]
      if (value !== null && Number.isFinite(value)) {
        total += value
        count += 1
      }
    }
    result[metric] = {
      value: count ? total : null,
      partial: count > 0 && count < (days?.length ?? 0),
    }
  }
  return result
}

export function formatCost(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return '--'
  }
  return `USD ${value.toLocaleString('en-US', { maximumFractionDigits: 8 })}`
}
