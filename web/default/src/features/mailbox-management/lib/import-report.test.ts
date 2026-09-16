import { describe, expect, it } from 'bun:test'

import type { ImportFailure } from '../types'
import { importFailureCSV, importFailuresMetadata } from './import-report'

describe('mailbox import failure reports', () => {
  it('projects only safe report fields', () => {
    const raw = [
      {
        row: 2,
        email: 'failed@example.test',
        codes: ['mailbox_import_invalid_otp'],
        password: 'private',
        secret: 'private',
        cvv: '012',
      },
    ]
    expect(importFailuresMetadata(raw)).toEqual([
      {
        row: 2,
        email: 'failed@example.test',
        codes: ['mailbox_import_invalid_otp'],
      },
    ])
    expect(importFailuresMetadata()).toEqual([])
  })
  it('exports a UTF-8 CSV with quoting and spreadsheet formula protection', () => {
    const failures: ImportFailure[] = [
      {
        row: 2,
        email: 'failed@example.test',
        codes: ['mailbox_import_invalid_otp'],
      },
      {
        row: 3,
        email: '=formula@example.test',
        codes: ['mailbox_import_duplicate'],
      },
      { row: 4, email: '', codes: ['mailbox_import_columns'] },
    ]
    const csv = importFailureCSV(
      failures,
      ['行号', '邮箱', '原因'],
      () => 'invalid, "value"\nnext'
    )
    expect(csv.startsWith('\uFEFF')).toBe(true)
    expect(csv).toContain(
      '"2","failed@example.test","invalid, ""value""\nnext"'
    )
    expect(csv).toContain('"\'=formula@example.test"')
    expect(csv).toContain('"4",""')
  })
})
