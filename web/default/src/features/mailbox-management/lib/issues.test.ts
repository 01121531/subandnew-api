import { describe, expect, test } from 'bun:test'

import type { Issue } from '../types'
import { importExample } from './import-examples'
import { issueMetadata } from './pools'

describe('mailbox issue and import contracts', () => {
  test('examples preserve field counts, fictional identities and leading zero CVV', () => {
    for (const kind of ['refund', 'opening'] as const) {
      for (const cvv of [false, true]) {
        const rows = importExample(kind, cvv).split('\n')
        expect(rows).toHaveLength(2)
        for (const row of rows) {
          const fields = row.split('----')
          const openingColumns = cvv ? 6 : 5
          expect(fields).toHaveLength(kind === 'refund' ? 3 : openingColumns)
          expect(fields[0]).toEndWith('@example.test')
          if (kind === 'opening' && cvv) expect(fields[5]).toBe('012')
        }
      }
    }
  })
  test('issue DTO strips unexpected credentials and enforces pool isolation', () => {
    const input = {
      id: 3,
      account_type: 'opening',
      card_last4: '4242',
      status: 'pending',
      password: 'never-cache',
      cvv: '012',
      attachments: [
        { id: 'test', storage_key: 'private', content_type: 'image/png' },
      ],
    } as unknown as Issue
    const output = issueMetadata(input, 'opening')
    expect(output).not.toHaveProperty('password')
    expect(output).not.toHaveProperty('cvv')
    expect(output.attachments[0]).not.toHaveProperty('storage_key')
    expect(() => issueMetadata(input, 'refund')).toThrow()
  })
})
