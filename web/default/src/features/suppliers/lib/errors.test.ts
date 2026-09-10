import { describe, expect, test } from 'bun:test'

import { SupplierRequestError } from '../portal-api'
import { canRetainQueryData } from './errors'

describe('supplier background refresh presentation', () => {
  test('keeps already loaded rows on transient failures without claiming a new snapshot', () => {
    for (const status of [0, 429, 500, 502, 503, 504]) {
      expect(
        canRetainQueryData(new SupplierRequestError('REQUEST_FAILED', status))
      ).toBe(true)
    }
    expect(canRetainQueryData(new TypeError('Failed to fetch'))).toBe(true)
  })

  test('hides cached content when permission is removed or the resource no longer exists', () => {
    for (const status of [200, 400, 401, 403, 404, 410]) {
      expect(
        canRetainQueryData(new SupplierRequestError('REQUEST_FAILED', status))
      ).toBe(false)
    }
    expect(canRetainQueryData(new Error('Unknown error'))).toBe(false)
  })
})
