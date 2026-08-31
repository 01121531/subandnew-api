/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { afterEach, describe, expect, mock, test } from 'bun:test'

import { getPortalSession, PortalRequestError, queryPortal } from './api'

const originalFetch = globalThis.fetch

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('portal request states', () => {
  test('treats an unauthenticated session as normal data', async () => {
    globalThis.fetch = mock(async () =>
      Response.json({
        success: true,
        message: '',
        data: { authenticated: false },
      })
    ) as unknown as typeof fetch

    await expect(getPortalSession('portal')).resolves.toEqual({
      authenticated: false,
    })
  })

  test('preserves server errors instead of treating them as logged out', async () => {
    globalThis.fetch = mock(async () =>
      Response.json(
        { message: 'upstream unavailable', error: { code: 'upstream' } },
        { status: 500 }
      )
    ) as unknown as typeof fetch

    try {
      await getPortalSession('portal')
      throw new Error('expected request to fail')
    } catch (error) {
      expect(error).toBeInstanceOf(PortalRequestError)
      expect((error as PortalRequestError).status).toBe(500)
      expect((error as PortalRequestError).code).toBe('upstream')
    }
  })

  test('passes AbortSignal to portal queries', async () => {
    const controller = new AbortController()
    let observedSignal: AbortSignal | null = null
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        observedSignal = init?.signal as AbortSignal
        return Response.json({
          success: true,
          message: '',
          data: {
            items: [],
            pagination: { page: 1, page_size: 50, total: 0, has_more: false },
            summary: { total: 0 },
            observed_at: '',
            stale: false,
            partial: false,
          },
        })
      }
    ) as unknown as typeof fetch

    await queryPortal(
      'portal',
      'csrf',
      {
        include_terms: [],
        exclude_terms: [],
        match_mode: 'all',
        rules: [],
        search: '',
        sort_by: '',
        sort_order: 'desc',
        page: 1,
        page_size: 50,
      },
      controller.signal
    )

    expect(observedSignal === controller.signal).toBe(true)
  })
})
