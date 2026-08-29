/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import type {
  PortalQuery,
  PortalResult,
  PortalSelection,
  PortalSession,
  PortalSessionProbe,
} from './types'

type Envelope<T> = {
  success: boolean
  message: string
  data: T
  csrf_token?: string
  error?: { code?: string }
}

export class PortalRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code = ''
  ) {
    super(message)
  }
}

function base(slug: string) {
  return `/open-portal/v1/account-data/${encodeURIComponent(slug)}`
}

async function request<T>(
  url: string,
  init?: RequestInit,
  responseType: 'json' | 'blob' = 'json'
) {
  const response = await fetch(url, { credentials: 'include', ...init })
  if (!response.ok) {
    const payload = await response.json().catch(() => null)
    throw new PortalRequestError(
      payload?.message || '请求暂时无法完成。',
      response.status,
      payload?.error?.code
    )
  }
  if (responseType === 'blob') return response
  return (await response.json()) as Envelope<T>
}

export async function loginPortal(slug: string, password: string) {
  const response = (await request<PortalSession>(`${base(slug)}/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })) as Envelope<PortalSession>
  return { ...response.data, csrf_token: response.csrf_token ?? '' }
}

export async function getPortalSession(slug: string) {
  const response = (await request<PortalSessionProbe>(
    `${base(slug)}/session`
  )) as Envelope<PortalSessionProbe>
  return response.data
}

export async function queryPortal(
  slug: string,
  csrf: string,
  query: PortalQuery
) {
  const response = (await request<PortalResult>(`${base(slug)}/query`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Portal-CSRF': csrf },
    body: JSON.stringify(query),
  })) as Envelope<PortalResult>
  return response.data
}

export async function exportPortal(
  slug: string,
  csrf: string,
  query: PortalQuery,
  mode: 'filtered' | 'selected',
  selections: PortalSelection[]
) {
  const response = (await request<never>(
    `${base(slug)}/export`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Portal-CSRF': csrf },
      body: JSON.stringify({ query, mode, selections }),
    },
    'blob'
  )) as Response
  const disposition = response.headers.get('Content-Disposition') ?? ''
  const fileName =
    disposition.match(/filename="([^"]+)"/)?.[1] ?? 'accounts.xlsx'
  return { blob: await response.blob(), fileName }
}

export async function logoutPortal(slug: string, csrf: string) {
  await request(`${base(slug)}/logout`, {
    method: 'POST',
    headers: { 'X-Portal-CSRF': csrf },
  })
}
