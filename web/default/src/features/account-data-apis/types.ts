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

type AccountDataAPIDataset = 'inventory' | 'account_output'
type AccountDataAPIStatus = 'enabled' | 'disabled'

export type AccountDataAPIInput = {
  name: string
  description: string
  status: AccountDataAPIStatus
  dataset: AccountDataAPIDataset
  preset_days: number
  instance_ids: number[]
  include_terms: string[]
  exclude_terms: string[]
  match_mode: AccountFilterMatchMode
  rules: AccountFilterRuleInput[]
  fields: string[]
  sort_by: string
  sort_order: 'asc' | 'desc'
  page_size: number
  rate_limit_per_minute: number
  allowed_cidrs: string[]
  portal_enabled: boolean
  portal_password: string
  reset_portal_slug: boolean
}

export type AccountDataAPIKey = {
  id: number
  name: string
  prefix: string
  expires_at: number
  revoked_at: number
  last_used_at: number
  created_at: number
}

export type AccountDataAPI = AccountDataAPIInput & {
  id: number
  timezone: string
  matched_count: number
  last_observed_at: number
  last_accessed_at: number
  request_count: number
  stale: boolean
  created_by: number
  updated_by: number
  created_at: number
  updated_at: number
  endpoint: string
  portal_configured: boolean
  portal_url: string
  portal_password_at: number
  keys: AccountDataAPIKey[]
}

export type AccountDataAPIList = {
  items: AccountDataAPI[]
  total: number
  page: number
  page_size: number
}

export type AccountDataAPIInstance = {
  id: number
  name: string
  kind: string
  status: string
}

export type AccountDataAPIPreview = {
  total: number
  summary: {
    total: number
    available: number
    unavailable: number
    unknown: number
    requests: number
    tokens: number
    amounts: Record<string, number>
  }
  sample: Record<string, unknown>[]
  observed_at: number
  stale: boolean
  partial: boolean
  sources: Array<{
    instance_id: number
    instance_name: string
    status: string
    observed_at: number
    error_code: string
  }>
}

export type AccountDataAPIFilterOptions = {
  filter_options: Record<string, string[]>
  observed_at: number
  stale: boolean
  partial: boolean
}

export type AccountDataAPIAccessLog = {
  id: number
  api_id: number
  key_id: number
  key_prefix: string
  request_id: string
  ip_address: string
  status_code: number
  duration_ms: number
  result_count: number
  error_code: string
  auth_type: string
  action: string
  session_id: number
  created_at: number
}

export type AccountDataAPIAccessLogList = {
  items: AccountDataAPIAccessLog[]
  total: number
  page: number
  page_size: number
}

export type AccountDataAPICreateResult = {
  api: AccountDataAPI
  secret: string
  key_prefix: string
}
