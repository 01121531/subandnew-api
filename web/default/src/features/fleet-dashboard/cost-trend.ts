export type CostTrendMode = 'daily' | 'cumulative'

type CostHistoryPoint = {
  timestamp: number
  today_cost: number | null
  today_cost_complete: boolean
}

const shanghaiDateParts = new Intl.DateTimeFormat('en-US', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  timeZone: 'Asia/Shanghai',
})

function formatShanghaiDateKey(timestamp: number): string {
  const parts = Object.fromEntries(
    shanghaiDateParts
      .formatToParts(new Date(timestamp * 1000))
      .map((part) => [part.type, part.value])
  )
  return `${parts.year}-${parts.month}-${parts.day}`
}

export function buildCostTrend(
  points: CostHistoryPoint[],
  mode: CostTrendMode
) {
  let cumulative = 0
  return points
    .filter((point) => point.today_cost_complete && point.today_cost != null)
    .map((point) => {
      cumulative += point.today_cost ?? 0
      return {
        date: formatShanghaiDateKey(point.timestamp),
        value: mode === 'cumulative' ? cumulative : (point.today_cost ?? 0),
      }
    })
}

export function isCostTrendPartial(
  points: CostHistoryPoint[],
  start: number,
  end: number
) {
  const expectedDays = Math.floor((end - start) / (24 * 60 * 60)) + 1
  return (
    points.length < expectedDays ||
    points.some((point) => !point.today_cost_complete)
  )
}
