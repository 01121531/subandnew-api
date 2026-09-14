type OutputMetrics = {
  collection_status?: string
  currency?: string
  amount?: number
  total_requests?: number
  total_tokens?: number
}

const exactNumber = new Intl.NumberFormat(undefined, {
  maximumFractionDigits: 4,
})
const exactCurrency = new Intl.NumberFormat(undefined, {
  style: 'currency',
  currency: 'USD',
  maximumFractionDigits: 4,
})

export function hasAccountOutputMetrics(output: OutputMetrics): boolean {
  return (
    output.collection_status == null || output.collection_status === 'succeeded'
  )
}

export function formatOutputAmount(
  value: number | null | undefined,
  currency?: string
): string {
  if (
    value == null ||
    !Number.isFinite(value) ||
    !currency ||
    currency === 'mixed'
  ) {
    return '--'
  }
  if (currency.toUpperCase() === 'USD') return exactCurrency.format(value)
  return `${exactNumber.format(value)} ${currency}`
}

export function accountOutputTotals(outputs: OutputMetrics[]) {
  // Field grants can redact collection status independently of the measurements.
  const collected = outputs.filter(hasAccountOutputMetrics)
  const sum = (key: 'amount' | 'total_requests' | 'total_tokens') => {
    if (
      !collected.length ||
      collected.some((output) => !Number.isFinite(output[key]))
    ) {
      return null
    }
    return collected.reduce((total, output) => total + (output[key] ?? 0), 0)
  }
  const currencies = new Set(collected.map((output) => output.currency))
  const currency = currencies.size === 1 ? [...currencies][0] : 'mixed'
  const amount = !currency || currency === 'mixed' ? null : sum('amount')
  return {
    added: outputs.length,
    collected: collected.length,
    requests: sum('total_requests'),
    tokens: sum('total_tokens'),
    amount,
    average:
      amount == null || collected.length !== outputs.length || !outputs.length
        ? null
        : amount / outputs.length,
    currency,
  }
}
