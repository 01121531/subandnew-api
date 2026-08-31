/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { describe, expect, test } from 'bun:test'

import { buildSetupPayload } from './api'

const values = {
  username: 'admin',
  password: 'correct horse battery staple',
  confirmPassword: 'correct horse battery staple',
  setup_token: '',
}

describe('buildSetupPayload', () => {
  test('omits an empty initialization token', () => {
    expect(buildSetupPayload(values, false)).not.toHaveProperty('setup_token')
  })

  test('sends a trimmed initialization token only with the current setup request', () => {
    expect(
      buildSetupPayload({ ...values, setup_token: '  one-time-token  ' }, true)
    ).toEqual({ setup_token: 'one-time-token' })
  })
})
