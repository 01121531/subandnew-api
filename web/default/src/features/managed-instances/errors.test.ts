import { expect, test } from 'bun:test'

import { isInstanceOrderConflict } from './errors'

test('instance order conflict recognizes the control-plane HTTP error shape', () => {
  const error = Object.assign(new Error('Order changed'), {
    response: {
      status: 409,
      data: { success: false, message: 'Order changed' },
    },
  })
  expect(isInstanceOrderConflict(error)).toBe(true)
})

test('other save failures do not force the conflict reload state', () => {
  for (const error of [
    undefined,
    null,
    '409',
    409,
    new Error('Network failed'),
    {},
    { response: null },
    { response: '409' },
    { response: {} },
    { response: { status: '409' } },
    { response: { status: 400 } },
    { response: { status: 401 } },
    { response: { status: 500 } },
  ]) {
    expect(isInstanceOrderConflict(error)).toBe(false)
  }
})
