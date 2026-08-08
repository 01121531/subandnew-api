type ManagedInstanceKind = 'new_api' | 'huichuan' | 'sub2api' | 'generic'
export type ManagedInstanceStatus =
  | 'unknown'
  | 'healthy'
  | 'degraded'
  | 'offline'
  | 'auth_failed'

export interface ManagedInstanceCredential {
  auth_type: string
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
  credential?: ManagedInstanceCredentialInput
}

export interface ManagedInstanceCredentialInput {
  auth_type: string
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

interface ManagedInstanceInventoryItem {
  id: number
  name: string
  type?: string
  group?: string
  status?: string
  enabled?: boolean
}

export interface ManagedInstanceInventoryPage {
  resource_kind: string
  items: ManagedInstanceInventoryItem[]
  total: number
  next_cursor?: string
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

export interface ManagedInstanceSummary {
  window: { start: number; end: number }
  resources: ManagedInstanceResourceSummary[]
  requests: ManagedInstanceMetricSample
  tokens: ManagedInstanceMetricSample
  cost: ManagedInstanceMetricSample
  error_rate: ManagedInstanceMetricSample
  latency: ManagedInstanceMetricSample
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
}

export interface ManagedInstanceAlertList {
  items: ManagedInstanceAlert[]
  total: number
  page: number
  page_size: number
}

interface ManagedInstanceConnectionStage {
  name: 'dns' | 'tcp' | 'tls' | 'http'
  status: 'not_run' | 'succeeded' | 'failed'
}

export interface ManagedInstancePreflight {
  success: boolean
  probe?: {
    kind: ManagedInstanceKind
    version: string
    system_name: string
    start_time: number
    status: ManagedInstanceStatus
    capabilities: string[]
    latency_ms: number
    checked_at: number
  }
  stages: ManagedInstanceConnectionStage[]
  error_code?: string
  advice?: string
}

export type ManagedInstanceOperationAction =
  | 'refresh_inventory'
  | 'test_resources'
  | 'toggle_resource'

export type ManagedInstanceOperationStatus =
  | 'planned'
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'unknown'

interface ManagedInstanceOperationPlan {
  action: ManagedInstanceOperationAction
  risk_level: 'low'
  writes_remote: boolean
  required_capability: string
  target_count: number
  summary: string
}

export interface ManagedInstanceOperationParameters {
  resource_ids?: number[]
  resource_id?: number
  enabled?: boolean
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
  risk_level: 'low'
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
