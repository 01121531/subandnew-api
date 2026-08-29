/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
export type ApiResponse<T> = {
  success: boolean
  message: string
  data: T
}

export type AssistantModelProfile = {
  id: number
  name: string
  provider: 'openai_compatible'
  base_url: string
  model: string
  api_key_fingerprint?: string
  timeout_seconds: number
  max_output_tokens: number
  enabled: boolean
  is_primary: boolean
  created_at: number
  updated_at: number
}

export type AssistantModelProfileInput = {
  name: string
  provider: 'openai_compatible'
  base_url: string
  model: string
  api_key: string
  timeout_seconds: number
  max_output_tokens: number
  enabled: boolean
  is_primary: boolean
}

export type AssistantModelTestResult = {
  model: string
  latency_ms: number
  reachable: boolean
}

export type AssistantChannelStatus =
  | 'unbound'
  | 'qr_issued'
  | 'scanned'
  | 'verify_required'
  | 'connected'
  | 'degraded'
  | 'reauth_required'

export type AssistantChannel = {
  id: number
  type: 'wechat_ilink'
  account_id: string
  status: AssistantChannelStatus
  enabled: boolean
  last_seen_at: number
  reauth_reason?: string
  created_at: number
  updated_at: number
}

export type AssistantLoginState =
  | 'pending'
  | 'scanned'
  | 'verify_required'
  | 'connected'
  | 'expired'

export type AssistantLoginView = {
  channel_id: number
  state: AssistantLoginState
  qr_image?: string
  expires_at?: number
  channel?: AssistantChannel
}

export type AssistantBindingCode = {
  code: string
  command: string
  expires_at: number
}

export type AssistantRunStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled'

export type AssistantRun = {
  id: number
  run_id: string
  conversation_id: number
  trigger_message_id: number
  model_profile_id: number
  model: string
  prompt_version: string
  status: AssistantRunStatus
  deadline_at: number
  input_tokens: number
  output_tokens: number
  total_tokens: number
  cached_input_tokens: number
  cache_observed_input_tokens: number
  cost: string
  error_code?: string
  error_stage?: string
  error_reason_code?: string
  error_detail?: string
  error_detail_truncated?: boolean
  provider_status_code?: number
  provider_error_code?: string
  trace_id: string
  started_at: number
  finished_at: number
  created_at: number
  updated_at: number
}

export type AssistantToolCallStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'denied'

export type AssistantToolRisk = 'low' | 'medium' | 'high' | 'critical'

export type AssistantToolCall = {
  id: number
  run_id: number
  sequence: number
  tool: string
  arguments_redacted?: string
  result_digest?: string
  status: AssistantToolCallStatus
  permission: string
  risk: AssistantToolRisk
  latency_ms: number
  error_code?: string
  error_detail?: string
  error_detail_truncated?: boolean
  started_at: number
  finished_at: number
  created_at: number
  updated_at: number
}

export type AssistantRunList = {
  items: AssistantRun[]
  total: number
  page: number
  page_size: number
}

export type AssistantRunDetail = {
  run: AssistantRun
  tool_calls: AssistantToolCall[]
}

export type AssistantIdentityStatus =
  | 'pending'
  | 'active'
  | 'disabled'
  | 'revoked'

export type AssistantInstanceScope = 'none' | 'selected' | 'all'

export type AssistantDefaultSource = 'personal' | 'global' | 'all'

export type AssistantInstanceOption = {
  id: number
  name: string
  kind: string
  status: string
}

export type AssistantDefaultInstanceSetting = {
  default_instance_id: number | null
}

export type AssistantIdentity = {
  id: number
  channel_id: number
  external_user: string
  user_id: number
  username: string
  status: AssistantIdentityStatus
  allowed_instance_scope: AssistantInstanceScope
  instance_ids: number[]
  default_instance_id: number | null
  effective_default_instance_id: number
  effective_default_instance_name: string
  default_source: AssistantDefaultSource
  default_fallback: boolean
  instance_options?: AssistantInstanceOption[]
  bound_at: number
}
