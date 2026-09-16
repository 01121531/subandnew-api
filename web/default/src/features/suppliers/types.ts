export type Envelope<T> =
  | { success: true; data: T }
  | { success: false; message: string }
export type Timestamp = string | number
export interface NamingRule {
  prefix: string
  suffix: string
}
export interface EffectiveNaming extends NamingRule {
  source: 'supplier' | 'binding'
  version: string
}
export type PolicyOverrides = Record<string, boolean | null>
export interface EffectivePolicy {
  values: Record<string, boolean>
  sources: Record<
    string,
    'global' | 'supplier' | 'binding' | 'responsible_admin'
  >
  version: string
}
export interface DefaultPolicy {
  policy: PolicyOverrides
  revision: number
}
export interface PortalSettings {
  title: string
  upload_methods: Record<UploadMethod, boolean>
  revision: number
}
export type PortalSupplier = Pick<
  Supplier,
  'id' | 'view_accounts' | 'view_usage' | 'manage_proxies' | 'upload_accounts'
>
export interface PortalBinding {
  id: number
  display_name: string
  enabled: boolean
  effective_policy?: EffectivePolicy
  effective_naming?: EffectiveNaming
  allowed_upload_methods: UploadMethod[]
  portal_revision: number
}
export interface Supplier {
  responsible_admin_id?: number
  naming_rule?: NamingRule | null
  effective_naming?: EffectiveNaming
  id: number
  name: string
  username: string
  enabled: boolean
  view_accounts: boolean
  view_usage: boolean
  manage_proxies: boolean
  upload_accounts: boolean
  policy_overrides?: PolicyOverrides | null
  effective_policy?: EffectivePolicy
  created_at: Timestamp
  updated_at: Timestamp
}
export type SupplierInput = Pick<Supplier, 'name' | 'username' | 'enabled'> &
  Partial<
    Pick<
      Supplier,
      'view_accounts' | 'view_usage' | 'manage_proxies' | 'upload_accounts'
    >
  > & {
    password?: string
    policy_overrides?: PolicyOverrides
    policy_revision?: string
    naming_rule?: NamingRule
    naming_revision?: string
  }
export interface Binding {
  display_name?: string | null
  naming_override?: NamingRule | null
  effective_naming?: EffectiveNaming
  id: number
  supplier_id: number
  instance_id: number
  instance_name: string
  remote_username?: string
  enabled: boolean
  policy_overrides?: PolicyOverrides | null
  effective_policy?: EffectivePolicy
}
export interface BindingInput {
  display_name?: string
  naming_override?: NamingRule | null
  naming_revision?: string
  instance_id: number
  identifier: string
  password: string
  enabled: boolean
  policy_overrides?: PolicyOverrides
  policy_revision?: string
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
  policy_changes?: string
  naming_changes?: string
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
  | {
      authenticated: true
      supplier: PortalSupplier
      csrf_token: string
      portal?: PortalSettings
    }
  | { authenticated: false; portal?: Pick<PortalSettings, 'title'> }
export type AuthSession = Extract<Session, { authenticated: true }>
export interface Account {
  id: string
  name: string
  email: string
  status: string
  health_status?: string | null
  failure_kind?: string | null
  last_error?: string | null
  cooldown?: boolean | null
  cooldown_reason?: string | null
  cooldown_remaining_seconds?: number | null
  rpm?: number | null
  tpm?: number | null
  concurrent?: number | null
  active_sessions?: number | null
  max_rpm?: number | null
  max_tpm?: number | null
  max_concurrent?: number | null
  max_sessions?: number | null
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
export interface AccountPage extends Snapshot {
  items: Account[]
  total?: number
  has_more?: boolean
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
  allowed_upload_methods?: UploadMethod[]
  portal_revision?: number
  effective_naming?: EffectiveNaming
  groups: Array<{ id: string; name: string }>
  policies: PolicyTemplate[]
  templates: Array<{ id: string; name: string }>
  proxies: Array<{ id: string; name: string; host: string; port: number }>
}
export interface UploadInput {
  portal_revision?: number
  name_time_mode?: NameTimeMode
  naming_revision?: string
  oauth_flow?: 'login' | 'setup_token'
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
export type UploadMethod = 'login' | 'setup_token' | 'rt' | 'sk'
export type NameTimeMode = 'none' | 'date' | 'date_time'
export interface AccountImportCredentials {
  refresh_token?: string
  access_token?: string
  session_keys?: string[]
}
type AccountImportStatus =
  | 'imported'
  | 'duplicate'
  | 'failed'
  | 'proxy_failed'
  | 'quota_blocked'
  | 'sk_invalid'
  | 'reauth_required'
  | 'unknown'
export interface AccountImportResult {
  resolved_name?: string
  resolved_name_prefix?: string
  total: number
  ok: number
  duplicate: number
  failed: number
  unknown: number
  results: Array<{ index: number; status: AccountImportStatus }>
}
export interface OAuthFlow {
  resolved_name?: string
  flow_id: string
  url: string
  expires_at: Timestamp
}
