/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { describe, expect, test } from 'bun:test'

import {
  emptyPortalSelection,
  isPortalSelectionChecked,
  portalFilterOptions,
  PortalRequestGate,
  portalSelectionCount,
  toggleAllPortalSelection,
  togglePortalSelection,
} from './portal-state'
import type { PortalResult, PortalSelection } from './types'

const first: PortalSelection = { instance_id: 1, account_id: 'account-1' }
const secondPage: PortalSelection = {
  instance_id: 1,
  account_id: 'account-51',
}

describe('portal request ordering', () => {
  test('only the latest query response may update the page', () => {
    const gate = new PortalRequestGate()
    const older = gate.next()
    const latest = gate.next()

    expect(gate.isCurrent(older)).toBe(false)
    expect(gate.isCurrent(latest)).toBe(true)
  })
})

describe('portal cross-page selection', () => {
  test('selects all filtered rows and supports exclusions on later pages', () => {
    let state = toggleAllPortalSelection(emptyPortalSelection())
    expect(portalSelectionCount(state, 120)).toBe(120)
    expect(isPortalSelectionChecked(state, first)).toBe(true)

    state = togglePortalSelection(state, secondPage, false)
    expect(portalSelectionCount(state, 120)).toBe(119)
    expect(isPortalSelectionChecked(state, first)).toBe(true)
    expect(isPortalSelectionChecked(state, secondPage)).toBe(false)

    state = togglePortalSelection(state, secondPage, true)
    expect(portalSelectionCount(state, 120)).toBe(120)
  })
})

describe('portal filter options', () => {
  test('prefers server options and falls back to current-page values', () => {
    const result: PortalResult = {
      items: [
        {
          instance_id: 1,
          account_id: 'a',
          status: 'current-page-only',
          group: 'page-group',
        },
      ],
      filter_options: {
        status: ['all-pages', { value: 'disabled', label: '已停用' }],
        vendor_name: ['All Pages Supplier'],
      },
      pagination: { page: 1, page_size: 50, total: 1, has_more: false },
      summary: { total: 1 },
      observed_at: '',
      stale: false,
      partial: false,
    }

    const options = portalFilterOptions(result, [
      'status',
      'group',
      'vendor_name',
    ])
    expect(options.status).toEqual([
      { value: 'all-pages', label: 'all-pages' },
      { value: 'disabled', label: '已停用' },
    ])
    expect(options.group).toEqual([
      { value: 'page-group', label: 'page-group' },
    ])
    expect(options.vendor_name).toEqual([
      { value: 'All Pages Supplier', label: 'All Pages Supplier' },
    ])
  })
})
