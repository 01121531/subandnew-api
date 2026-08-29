export type AccountAmountSummary = {
  key: string
  label: string
  value: string
}

const exactAmount = new Intl.NumberFormat('zh-CN', {
  minimumFractionDigits: 8,
  maximumFractionDigits: 8,
})

function displayUnit(unit: string) {
  return unit.toLowerCase() === 'unknown' ? '未知单位' : unit
}

export function accountAmountSummaries(
  amounts: Record<string, number> | undefined
): AccountAmountSummary[] {
  const entries = Object.entries(amounts ?? {}).filter(([, value]) =>
    Number.isFinite(value)
  )
  entries.sort(([left], [right]) => {
    const leftUSD = left.toUpperCase() === 'USD'
    const rightUSD = right.toUpperCase() === 'USD'
    if (leftUSD !== rightUSD) return leftUSD ? -1 : 1
    return left.localeCompare(right)
  })
  return entries.map(([unit, amount]) => {
    const sourceUnit = unit.trim() || 'unknown'
    const normalizedUnit = displayUnit(sourceUnit)
    return {
      key: unit,
      label: entries.length === 1 ? '总金额' : `总金额（${normalizedUnit}）`,
      value:
        sourceUnit.toUpperCase() === 'USD'
          ? `US$${exactAmount.format(amount)}`
          : `${exactAmount.format(amount)} ${normalizedUnit}`,
    }
  })
}
