/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
type ManagedInstanceKind =
  | 'new_api'
  | 'huichuan'
  | 'sub2api'
  | 'conductor'
  | 'claude_gateway'
  | 'generic'
export type ManagedInstanceStatus =
  | 'unknown'
  | 'healthy'
  | 'degraded'
  | 'offline'
  | 'auth_failed'

export interface ManagedInstanceCredential {
  auth_type: string
  access_scope: 'admin' | 'user'
  fingerprint: string
  expires_at: number
  last_verified_at: number
  rotated_at: number
}

export interface ManagedInstance {
  id: number
  name: string
  kind: ManagedInstanceKind
  base_url: string
  environment: string
  labels: Record<string, string>
  management_mode: 'observe' | 'operate' | 'enforce'
  status: ManagedInstanceStatus
  version: string
  capabilities: string[]
  tls_verify: boolean
  request_timeout_seconds: number
  check_interval_seconds: number
  alert_failure_threshold: number
  last_seen_at: number
  last_checked_at: number
  consecutive_failures: number
  created_at: number
  updated_at: number
  credential?: ManagedInstanceCredential
}

export interface ManagedInstanceInput {
  name: string
  kind: ManagedInstanceKind
  base_url: string
  environment: string
  labels: Record<string, string>
  management_mode: 'observe' | 'operate' | 'enforce'
  tls_verify: boolean
  request_timeout_seconds: number
  check_interval_seconds: number
  alert_failure_threshold: number
  credential?: ManagedInstanceCredentialInput
}

export interface ManagedInstanceCredentialInput {
  auth_type: string
  access_scope: 'admin' | 'user'
  secret: string
  user_id: string
  expires_at: number
}

export interface ManagedInstanceList {
  items: ManagedInstance[]
  total: number
  page: number
  page_size: number
}

export interface ManagedInstanceFilters {
  search: string
  kind: string
  status: string
}

export interface ManagedInstanceAudit {
  id: number
  instance_id: number
  actor_id: number
  action: string
  outcome: string
  details: string
  created_at: number
}

export interface ManagedInstanceAuditList {
  items: ManagedInstanceAudit[]
  total: number
  page: number
  page_size: number
}

export type ManagedInstanceTaskStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'

export interface ManagedInstanceTask {
  id: number
  task_id: string
  type: string
  scope_key: string
  status: ManagedInstanceTaskStatus
  payload: unknown
  state: unknown
  result: unknown
  error: string
  created_at: number
  updated_at: number
}

export type ManagedInstanceCollectionStatus =
  | 'succeeded'
  | 'failed'
  | 'unsupported'

export interface ManagedInstanceObservation<T> {
  source_instance_id: number
  observed_at: number
  collection_status: ManagedInstanceCollectionStatus
  error_code?: string
  etag?: string
  data?: T
}

export interface ManagedInstanceInventoryItem {
  id: number
  id_text?: string
  name: string
  email?: string
  note?: string
  ownership?: string
  vendor_id?: string
  vendor_name?: string
  vendor_email?: string
  type?: string
  platform?: string
  source_id?: string
  group?: string
  status?: string
  enabled?: boolean
  created_at?: number
  last_activity_at?: number
  disabled_at?: number
  expires_at?: number
  requests?: number
  tokens?: number
  cost?: number
  cost_unit?: 'usd' | 'quota'
  usage_window_days?: number
  lifetime_cost?: number
  today_cost?: number
  cost_excluding_today?: number
  requests_24h?: number
  successful_requests_24h?: number
  limited_requests_24h?: number
  balance?: number
  response_time_ms?: number
  error_message?: string
  rate_limited?: boolean
  active_sessions?: number
  rpm?: number
  account_count?: number
  utilization_5h?: number
  utilization_7d?: number
  utilization_7d_oi?: number
  input_price_per_m?: number
  output_price_per_m?: number
  cache_read_price_per_m?: number
  cache_create_price_per_m?: number
}

export interface ManagedInstanceInventorySource {
  id: string
  name: string
  url?: string
  status?: string
  enabled?: boolean
  account_count?: number
}

export interface ManagedInstanceInventoryPage {
  resource_kind: string
  items: ManagedInstanceInventoryItem[]
  sources?: ManagedInstanceInventorySource[]
  total: number
  next_cursor?: string
  vendor_collection_status?: ManagedInstanceCollectionStatus
  vendor_observed_at?: number
  vendor_stale?: boolean
  vendor_error_code?: string
}

export interface ManagedInstanceMetricSample {
  value: number | null
  unit: string
  collection_status: ManagedInstanceCollectionStatus
}

interface ManagedInstanceResourceSummary {
  resource_kind: string
  total: number
  enabled: number | null
  unhealthy: number | null
}

interface ManagedInstanceUsageTrendPoint {
  date: string
  requests: number
  tokens: number
  cost: number
}

export interface ManagedInstanceSummary {
  window: { start: number; end: number }
  resources: ManagedInstanceResourceSummary[]
  requests: ManagedInstanceMetricSample
  tokens: ManagedInstanceMetricSample
  cost: ManagedInstanceMetricSample
  error_rate: ManagedInstanceMetricSample
  latency: ManagedInstanceMetricSample
  trend: ManagedInstanceUsageTrendPoint[]
}

interface ManagedDashboardRange {
  range_key: string
  preset_days: number
  start: number
  end: number
  timezone: string
}

export interface ManagedDashboardSnapshotSection {
  range: ManagedDashboardRange
  observation?: ManagedInstanceObservation<ManagedInstanceSummary>
  last_attempt_at: number
  last_attempt_status: ManagedInstanceCollectionStatus | 'running' | ''
  last_error_code?: string
  stale: boolean
}

interface ManagedDashboardInstanceSnapshot {
  instance_id: number
  summary: ManagedDashboardSnapshotSection
  today: ManagedDashboardSnapshotSection
}

export interface ManagedDashboardSnapshotList {
  range: ManagedDashboardRange
  items: ManagedDashboardInstanceSnapshot[]
}

export interface ManagedDashboardRefreshResult {
  instance_id: number
  enqueued: boolean
  task?: ManagedInstanceTask
}

export interface ManagedDashboardEvent {
  type: 'summary' | 'status' | 'topology'
  instance_id?: number
  snapshot?: ManagedDashboardSnapshotSection
  instance_ids?: number[]
}

export interface ManagedInstanceRealtimeState {
  instance_id: number
  observed_at: number
  last_attempt_at?: number
  stream_status: string
  stale: boolean
  error_code?: string
  rpm: ManagedInstanceMetricSample
  rpm_capacity: ManagedInstanceMetricSample
  success_rate: ManagedInstanceMetricSample
  success_rate_sample_count?: number
  today_cost?: ManagedInstanceMetricSample
  today_cost_observed_at?: number
  today_cost_stale?: boolean
  cost_7d?: ManagedInstanceMetricSample
  cost_7d_observed_at?: number
  cost_7d_stale?: boolean
  cost_30d?: ManagedInstanceMetricSample
  cost_30d_observed_at?: number
  cost_30d_stale?: boolean
  concurrency_used?: ManagedInstanceMetricSample
  concurrency_max?: ManagedInstanceMetricSample
  concurrency_collection_status?: string
  accounts_total?: number
  accounts_available?: number
  accounts_rate_limited?: number
  accounts_collection_status?: string
  accounts_reporting?: number
  active_sessions?: number
  accounts?: ManagedInstanceInventoryItem[]
  sources?: ManagedInstanceInventorySource[]
  account_snapshot?: ManagedAccountSnapshotEvent
}

interface ManagedAccountSnapshotEvent {
  instance_id: number
  observed_at: number
  last_attempt_at: number
  last_attempt_status: ManagedInstanceCollectionStatus | ''
  last_error_code?: string
  range_keys: string[]
}

export type ManagedInstanceRPMHistoryBucket = 'minute' | 'hour' | 'day'

interface ManagedInstanceRPMHistoryPoint {
  timestamp: number
  rpm: number
  capacity: number | null
  samples: number
  success_rate: number | null
  success_rate_samples: number
  accounts_available: number | null
  accounts_total: number | null
  account_samples: number
  today_cost: number | null
  today_cost_samples: number
  today_cost_complete: boolean
}

export interface ManagedInstanceRPMHistory {
  bucket: ManagedInstanceRPMHistoryBucket
  start: number
  end: number
  points: ManagedInstanceRPMHistoryPoint[]
}

export interface ManagedInstanceAccountOutputItem {
  account: ManagedInstanceInventoryItem
  total_requests: number
  total_tokens: number
  amount: number
  currency: string
  collection_status: ManagedInstanceCollectionStatus
  error_code?: string
}

interface ManagedInstanceAccountOutput {
  source_instance_id: number
  kind: string
  window: { start: number; end: number }
  items: ManagedInstanceAccountOutputItem[]
  added_accounts: number
  collected_accounts: number
  total_requests: number
  total_tokens: number
  total_amount: number
  currency: string
}

export interface ManagedAccountRangeInput {
  preset_days?: 1 | 7 | 14 | 30
  start?: number
  end?: number
  timezone?: string
}

interface ManagedAccountSnapshotSection<T> {
  observation?: ManagedInstanceObservation<T>
  last_attempt_at: number
  last_attempt_status: ManagedInstanceCollectionStatus | ''
  last_error_code?: string
}

export interface ManagedAccountSnapshotView {
  range: {
    range_key: string
    preset_days: number
    start: number
    end: number
    timezone: string
  }
  inventory: ManagedAccountSnapshotSection<ManagedInstanceInventoryPage>
  account_output: ManagedAccountSnapshotSection<ManagedInstanceAccountOutput>
  refresh_recommended: boolean
  task?: ManagedInstanceTask
}

export interface ManagedAccountRefreshView {
  enqueued: boolean
  task?: ManagedInstanceTask
}

export interface ManagedInstanceAlert {
  id: number
  instance_id: number
  alert_type: 'availability' | 'credential'
  status: 'open' | 'resolved'
  error_code: string
  occurrences: number
  first_seen_at: number
  last_seen_at: number
  resolved_at: number
  email_status: 'pending' | 'retrying' | 'sent' | 'cancelled'
  email_recipients: string
  email_attempts: number
  email_error: string
  email_sent_at: number
  email_next_retry_at: number
  recovery_email_status: 'pending' | 'retrying' | 'sent' | 'cancelled'
  recovery_email_recipients: string
  recovery_email_attempts: number
  recovery_email_error: string
  recovery_email_sent_at: number
  recovery_email_next_retry_at: number
}

export interface ManagedInstanceAlertList {
  items: ManagedInstanceAlert[]
  total: number
  page: number
  page_size: number
}

export type ManagedInstanceOperationAction =
  | 'refresh_inventory'
  | 'test_resources'
  | 'toggle_resource'
  | 'apply_config'

export type ManagedInstanceOperationStatus =
  | 'planned'
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'unknown'

interface ManagedInstanceOperationPlan {
  action: ManagedInstanceOperationAction
  risk_level: 'low' | 'medium'
  writes_remote: boolean
  required_capability: string
  target_count: number
  summary: string
  expected_config_hash?: string
  template_id?: number
  differences?: ManagedConfigDiff[]
}

export interface ManagedInstanceOperationParameters {
  resource_ids?: number[]
  resource_id?: number
  enabled?: boolean
  template_id?: number
  schema_version?: number
  expected_hash?: string
  desired?: Record<string, unknown>
  rollback?: Record<string, unknown>
}

interface ManagedInstanceOperationResultItem {
  resource_id: number
  succeeded: boolean
  enabled?: boolean
}

interface ManagedInstanceOperationResult {
  action: ManagedInstanceOperationAction
  resource_kind: string
  count?: number
  items?: ManagedInstanceOperationResultItem[]
  changed_fields?: string[]
  observed_hash?: string
  desired_hash?: string
  verified?: boolean
  compensated?: boolean
}

export interface ManagedInstanceOperation {
  id: number
  operation_id: string
  instance_id: number
  task_id?: string
  actor_id: number
  executed_by: number
  action: ManagedInstanceOperationAction
  status: ManagedInstanceOperationStatus
  risk_level: 'low' | 'medium'
  writes_remote: boolean
  required_capability: string
  idempotency_fingerprint: string
  error_code?: string
  planned_at: number
  executed_at: number
  finished_at: number
  created_at: number
  updated_at: number
  parameters: ManagedInstanceOperationParameters
  plan: ManagedInstanceOperationPlan
  result?: ManagedInstanceOperationResult
  idempotent_replay?: boolean
}

export interface ManagedInstanceOperationPlanInput {
  action: ManagedInstanceOperationAction
  idempotency_key: string
  parameters: ManagedInstanceOperationParameters
}

export interface ManagedInstanceOperationExecuteInput {
  operation_id: string
  idempotency_key: string
}

export interface ManagedInstanceOperationExecution {
  operation: ManagedInstanceOperation
  task?: ManagedInstanceTask
}

export type ManagedInstanceBatchStatus =
  | 'planning'
  | 'planned'
  | 'partially_planned'
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'partially_failed'
  | 'failed'
  | 'needs_reconcile'

interface ManagedInstanceBatchSummary {
  total: number
  planned: number
  active: number
  succeeded: number
  failed: number
  unknown: number
}

export interface ManagedInstanceBatchItem {
  instance_id: number
  position: number
  status: ManagedInstanceOperationStatus
  error_code?: string
  parameters: ManagedInstanceOperationParameters
  operation?: ManagedInstanceOperation
}

export interface ManagedInstanceBatchView {
  batch_id: string
  actor_id: number
  executed_by: number
  action: 'refresh_inventory'
  status: ManagedInstanceBatchStatus
  idempotency_fingerprint: string
  planned_at: number
  executed_at: number
  finished_at: number
  created_at: number
  updated_at: number
  idempotent_replay?: boolean
  summary: ManagedInstanceBatchSummary
  items: ManagedInstanceBatchItem[]
}

export interface ManagedInstanceBatchPlanInput {
  action: 'refresh_inventory'
  idempotency_key: string
  targets: Array<{
    instance_id: number
    parameters: ManagedInstanceOperationParameters
  }>
}

export interface ManagedInstanceBatchExecuteInput {
  batch_id: string
  idempotency_key: string
}

export interface ApiResponse<T> {
  success: boolean
  message: string
  data: T
}

export interface ManagedConfigFieldSchema {
  key: string
  remote_key: string
  type: 'string' | 'integer' | 'boolean'
  description: string
  min?: number
  max?: number
  min_length?: number
  max_length?: number
  format?: string
  enum?: Array<string | number | boolean>
}

export interface ManagedConfigSchema {
  kind: ManagedInstanceKind
  version: number
  fields: ManagedConfigFieldSchema[]
}

export interface ManagedConfigTemplate {
  id: number
  name: string
  description: string
  kind: ManagedInstanceKind
  schema_version: number
  values: Record<string, unknown>
  created_by: number
  updated_by: number
  created_at: number
  updated_at: number
}

export interface ManagedConfigTemplateList {
  items: ManagedConfigTemplate[]
}

export type ManagedConfigMode = 'disabled' | 'audit' | 'enforce'
type ManagedConfigDriftStatus = 'unknown' | 'in_sync' | 'drifted' | 'failed'

export interface ManagedConfigBinding {
  id: number
  instance_id: number
  template_id: number
  mode: ManagedConfigMode
  drift_status: ManagedConfigDriftStatus
  desired_hash: string
  last_observed_hash: string
  last_error_code?: string
  last_checked_at: number
  last_applied_at: number
  template: ManagedConfigTemplate
}

interface ManagedConfigDiff {
  key: string
  current: unknown
  desired: unknown
}

export interface ManagedConfigPreview {
  binding: ManagedConfigBinding
  observed: Record<string, unknown>
  desired: Record<string, unknown>
  differences: ManagedConfigDiff[]
  observed_hash: string
  desired_hash: string
  drifted: boolean
  observed_at: number
}

export interface ManagedConfigTemplateInput {
  name: string
  description: string
  kind: ManagedInstanceKind
  schema_version: number
  values: Record<string, unknown>
}
