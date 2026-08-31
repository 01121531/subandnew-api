/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { describe, expect, test } from 'bun:test'

import { AxiosError } from 'axios'

import { shouldNavigateToServerError } from './query-error-policy'

function serverError() {
  return new AxiosError('failed', 'ERR_BAD_RESPONSE', undefined, undefined, {
    status: 500,
  } as never)
}

describe('query error navigation policy', () => {
  test('background 500 errors remain local to their feature', () => {
    expect(shouldNavigateToServerError(serverError(), undefined)).toBe(false)
    expect(
      shouldNavigateToServerError(serverError(), { criticalErrorPage: false })
    ).toBe(false)
  })

  test('explicit critical queries may navigate to the server error page', () => {
    expect(
      shouldNavigateToServerError(serverError(), { criticalErrorPage: true })
    ).toBe(true)
  })
})
