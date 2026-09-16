import type { ImportFailure } from '../types'
import { safeCode } from './errors'

export function importFailuresMetadata(
  value?: ImportFailure[]
): ImportFailure[] {
  return (value ?? []).map((failure) => ({
    row: failure.row,
    email: failure.email,
    codes: failure.codes.map(safeCode),
  }))
}

export function importFailureCSV(
  failures: ImportFailure[],
  headers: string[],
  describe: (code: string) => string
): string {
  const cell = (value: string | number) => {
    const text = String(value)
    const safe = /^\s*[=+@-]/.test(text) ? `'${text}` : text
    return `"${safe.replaceAll('"', '""')}"`
  }
  const rows = failures.map((failure) => [
    failure.row,
    failure.email,
    failure.codes.map(describe).join('; '),
  ])
  return `\uFEFF${[headers, ...rows].map((row) => row.map(cell).join(',')).join('\r\n')}`
}
