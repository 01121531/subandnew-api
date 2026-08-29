/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import type {
  AccountFilterMatchMode,
  AccountFilterRuleInput,
} from '@/features/managed-accounts/account-filtering'

export type PortalSession = {
  name: string
  description: string
  dataset: 'inventory' | 'account_output'
  preset_days: number
  timezone: string
  fields: string[]
  filter_fields: string[]
  page_size: number
  expires_at: number
  csrf_token: string
  last_observed_at: number
  stale: boolean
}

export type PortalQuery = {
  include_terms: string[]
  exclude_terms: string[]
  match_mode: AccountFilterMatchMode
  rules: AccountFilterRuleInput[]
  search: string
  sort_by: string
  sort_order: 'asc' | 'desc'
  page: number
  page_size: number
}

export type PortalItem = Record<string, unknown> & {
  instance_id: number
  account_id: string
}

export type PortalResult = {
  items: PortalItem[]
  pagination: {
    page: number
    page_size: number
    total: number
    has_more: boolean
  }
  summary: {
    total: number
    available?: number
    unavailable?: number
    unknown?: number
    requests?: number
    tokens?: number
    amounts?: Record<string, number>
  }
  observed_at: string
  stale: boolean
  partial: boolean
}

export type PortalSelection = { instance_id: number; account_id: string }
