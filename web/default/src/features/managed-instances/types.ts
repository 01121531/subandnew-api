export type ManagedInstanceKind = 'new_api' | 'huichuan' | 'sub2api' | 'generic'
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

export interface ApiResponse<T> {
  success: boolean
  message: string
  data: T
}
