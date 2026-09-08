export type Envelope<T> =
  | { success: true; data: T }
  | { success: false; message: string }
export type Timestamp = string | number
export interface Supplier {
  id: number
  name: string
  username: string
  enabled: boolean
  view_accounts: boolean
  view_usage: boolean
  manage_proxies: boolean
  upload_accounts: boolean
  created_at: Timestamp
  updated_at: Timestamp
}
export type SupplierInput = Omit<
  Supplier,
  'id' | 'created_at' | 'updated_at'
> & { password?: string }
export interface Binding {
  id: number
  supplier_id: number
  instance_id: number
  instance_name: string
  remote_username?: string
  enabled: boolean
}
export interface BindingInput {
  instance_id: number
  identifier: string
  password: string
  enabled: boolean
}
export interface Audit {
  id: number
  supplier_id?: number
  action?: string
  created_at?: Timestamp
  status_code: number
  duration_ms: number
  binding_id: number
  admin_id: number
  error_code?: string
  ip_address?: string
}
export interface Items<T> {
  items: T[]
  total: number
}
export interface Snapshot {
  observed_at: Timestamp
  stale: boolean
}
export type Session =
  | { authenticated: true; supplier: Supplier; csrf_token: string }
  | { authenticated: false }
export type AuthSession = Extract<Session, { authenticated: true }>
export interface Account {
  id: string
  name: string
  email: string
  status: string
  created_at: Timestamp
  group_name: string
  total_cost: number | null
  today_cost: number | null
  total_requests: number | null
  total_tokens: number | null
}
export interface AccountQuery {
  binding_id: number
  page: number
  page_size: number
  search: string
  status: string
  recovery_window: string
  sort: string
  direction: 'asc' | 'desc'
}
export interface AccountPage extends Items<Account>, Snapshot {
  page: number
  page_size: number
}
export interface AccountSummary extends Snapshot {
  total_accounts: number
  available_accounts: number
  rpm: number
  pool_rpm?: number | null
  pool_concurrent?: number | null
  pool_available_accounts?: number | null
}
export interface UsageMetrics {
  requests: number | null
  tokens: number | null
  cost: number | null
}
export interface Usage extends Snapshot {
  days: Array<UsageMetrics & { date: string }>
  accounts: Array<UsageMetrics & { id: string; name: string }> | null
}
export interface Proxy {
  id: string
  name: string
  scheme: string
  host: string
  port: number
  status: string
  health_status: string
  latency_ms: number | null
  is_owner: boolean | null
  ownership: 'owned' | 'assigned' | 'unknown' | 'conflict'
  can_update_status: boolean
  status_update_reason: string
}
export interface ProxyPage extends Items<Proxy>, Snapshot {}
export interface TestResult {
  ok: boolean
  latency_ms?: number
}
export interface ImportResult {
  imported: number
  failed: number
}
export interface PolicyTemplate {
  id: string
  name: string
  policy: Record<
    'max_rpm' | 'max_tpm' | 'max_concurrent' | 'max_sessions',
    number | null
  >
}
export interface UploadOptions extends Snapshot {
  groups: Array<{ id: string; name: string }>
  policies: PolicyTemplate[]
  templates: Array<{ id: string; name: string }>
  proxies: Array<{ id: string; name: string; host: string; port: number }>
}
export interface UploadInput {
  binding_id: number
  name: string
  outbound_proxy_mode: 'direct' | 'manual' | 'auto'
  outbound_proxy_id: string
  group_ids: string[]
  policy_template_id: string
  cc_template_id: string
  max_rpm: number
  max_tpm: number
  max_concurrent: number
  max_sessions: number
}
export interface OAuthFlow {
  flow_id: string
  url: string
  expires_at: Timestamp
}
