import type { PolicyTemplate } from '../types'

const limits = {
  max_rpm: 1_000_000,
  max_tpm: 1_000_000_000,
  max_concurrent: 100_000,
  max_sessions: 100_000,
}

export function policyValues(template: PolicyTemplate) {
  return Object.entries(limits).map(([key, max]) => {
    const field = key as keyof typeof limits
    const raw: unknown = template.policy?.[field]
    let value: number | '' = ''
    if (
      typeof raw === 'number' ||
      (typeof raw === 'string' && raw.trim() !== '')
    ) {
      const parsed = Number(raw)
      if (Number.isInteger(parsed) && parsed >= 0 && parsed <= max) {
        value = parsed
      }
    }
    return [field, value] as const
  })
}
