/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { normalizedFilterEntries } from './filter-details-data'

describe('export filter details normalization', () => {
  test('ignores missing and null legacy filter values', () => {
    assert.deepEqual(normalizedFilterEntries(null), [])
    assert.deepEqual(
      normalizedFilterEntries({ username: null, model: undefined }),
      []
    )
  })

  test('keeps valid values while discarding malformed entries', () => {
    assert.deepEqual(
      normalizedFilterEntries({
        username: ['alice', '', null],
        model: 'claude-sonnet',
        group: 42,
      }),
      [
        { key: 'username', values: ['alice'] },
        { key: 'model', values: ['claude-sonnet'] },
      ]
    )
  })
})
