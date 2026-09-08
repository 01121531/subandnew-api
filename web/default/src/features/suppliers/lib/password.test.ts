import { afterEach, expect, spyOn, test } from 'bun:test'

import { generateSupplierPassword } from './password'

afterEach(() => {
  getRandomValues.mockRestore()
})
const getRandomValues = spyOn(globalThis.crypto, 'getRandomValues')

test('password generation uses 24 Web Crypto bytes with no insecure fallback', () => {
  getRandomValues.mockImplementationOnce(
    <T extends ArrayBufferView | null>(array: T): T => {
      if (array instanceof Uint8Array) array.fill(0)
      return array
    }
  )
  expect(generateSupplierPassword()).toBe('A'.repeat(24))
  expect(getRandomValues).toHaveBeenCalledTimes(1)
  expect(getRandomValues.mock.calls[0][0]?.byteLength).toBe(24)
})
