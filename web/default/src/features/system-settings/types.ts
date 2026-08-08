export type SystemOption = {
  key: string
  value: string
}

export type SystemOptionKey = string

export type SystemOptionsResponse = {
  success: boolean
  message: string
  data: SystemOption[]
}

export type UpdateOptionRequest = {
  key: string
  value: string | boolean | number
}

export type UpdateOptionResponse = {
  success: boolean
  message: string
}

export type NERVSelfCheckRequiredFile = {
  path: string
  exists: boolean
  size: number
}

export type NERVSelfCheckAssets = {
  base_path: string
  exists: boolean
  file_count: number
  total_size_bytes: number
  required_files: NERVSelfCheckRequiredFile[]
  candidates: string[]
}

export type NERVAssetItem = {
  path: string
  name: string
  kind: string
  size: number
  modified_at: number
  previewable: boolean
}

export type NERVAssetsData = {
  base_path: string
  count: number
  items: NERVAssetItem[]
}

export type NERVAssetsResponse = {
  success: boolean
  message: string
  data?: NERVAssetsData
}

export type NERVAssetFileData = {
  path: string
  name: string
  kind: string
  size: number
  content_type: string
  text?: string
  content_base64?: string
  truncated: boolean
}

export type NERVAssetFileResponse = {
  success: boolean
  message: string
  data?: NERVAssetFileData
}

export type NERVSelfCheckCatalog = {
  tools_json_exists: boolean
  tools_parsed: boolean
  tool_count: number
  category_count: number
  tool_available: number
  tool_missing: number
  tool_uncheckable: number
  tool_availability: NERVSelfCheckToolAvailability[]
  skill_count: number
  skill_dir_count: number
  error?: string
}

export type NERVSelfCheckToolAvailability = {
  name: string
  category: string
  binary: string
  checkable: boolean
  available: boolean
}

export type NERVTamperRuleError = {
  line: number
  pattern: string
  error: string
}

export type NERVSelfCheckConfig = {
  enabled: boolean
  chat_enabled: boolean
  responses_enabled: boolean
  skills_enabled: boolean
  skills_limit: number
  tamper_enabled: boolean
  mode: string
  models: string
  targets: string
  prompt_configured: boolean
  prompt_length: number
  tamper_rule_lines: number
  tamper_rule_count: number
  tamper_rule_invalid: number
  tamper_rule_errors: NERVTamperRuleError[]
  bundled_rule_count?: number
  bundled_rule_source?: string
  mcp_backend: string
  wsl_distro: string
  docker_container: string
  ssh_host: string
}

export type NERVSelfCheckRecentEvent = {
  ts: number
  event: string
  target: string
  model: string
}

export type NERVSelfCheckStats = {
  total: number
  inject: number
  tamper: number
  chat_inject: number
  responses_inject: number
  chat_tamper: number
  responses_tamper: number
  last_event_at: number
  last_event: string
  last_target: string
  last_model: string
  recent: NERVSelfCheckRecentEvent[]
  recent_valid: boolean
}

export type NERVMemorySuccess = {
  ts: string
  category: string
  user: string
  result: string
  technique: string
  hash: string
  target: string
  model: string
  event: string
}

export type NERVMemoryKernel = {
  successes: NERVMemorySuccess[]
  patterns: Record<string, number>
  techniques: Record<string, number>
  stats: Record<string, number>
}

export type NERVSelfCheckItem = {
  key: string
  ok: boolean
  message: string
}

export type NERVSelfCheckData = {
  assets: NERVSelfCheckAssets
  catalog: NERVSelfCheckCatalog
  config: NERVSelfCheckConfig
  stats: NERVSelfCheckStats
  memory: NERVMemoryKernel
  checks: NERVSelfCheckItem[]
  working_dir: string
  executable_dir: string
}

export type NERVSelfCheckResponse = {
  success: boolean
  message: string
  data: NERVSelfCheckData
}

export type NERVVerifySmokeData = {
  ok: boolean
  checks: NERVSelfCheckItem[]
  asset_path: string
  tool_count: number
  skill_count: number
  missing_required_files: string[]
}

export type NERVVerifySmokeResponse = {
  success: boolean
  message: string
  data?: NERVVerifySmokeData
}

export type NERVCodexConfigStatus = {
  home: string
  config_path: string
  found: boolean
  config_exists: boolean
  backup_exists: boolean
  bridge_active: boolean
  bridge_exists: boolean
  skills_exists: boolean
  skill_count: number
  asset_path: string
  asset_exists: boolean
  asset_bridge_exists: boolean
  asset_skills_exists: boolean
  asset_mcp_server_exists: boolean
  mcp_server_script_path: string
  mcp_active: boolean
  mcp_backup_exists: boolean
  mcp_config_raw?: string
  candidates: string[]
  message: string
  model_instructions_raw?: string
}

export type NERVCodexConfigResult = {
  action: 'apply' | 'remove' | 'apply_mcp' | 'remove_mcp'
  changed: boolean
  backup_path?: string
  messages: string[]
  status: NERVCodexConfigStatus
}

export type NERVCodexConfigResponse = {
  success: boolean
  message: string
  data?: NERVCodexConfigStatus
}

export type NERVCodexConfigMutationResponse = {
  success: boolean
  message: string
  data?: NERVCodexConfigResult
}

export type NERVCodexVerifyCheck = {
  key: string
  ok: boolean
  level: 'pass' | 'fail' | 'warn'
  message: string
}

export type NERVCodexVerifyResult = {
  ok: boolean
  home: string
  found: boolean
  config_path: string
  script_path: string
  python_command: string
  exit_code: number
  timed_out: boolean
  duration_ms: number
  output: string
  checks: NERVCodexVerifyCheck[]
  candidates: string[]
  bridge_verified: boolean
  skills_verified: boolean
  codex_cli_available: boolean
  smoke_ok: boolean
  message: string
}

export type NERVCodexVerifyResponse = {
  success: boolean
  message: string
  data?: NERVCodexVerifyResult
}

export type NERVBridgePromptData = {
  path: string
  prompt: string
  length: number
}

export type NERVBridgePromptResponse = {
  success: boolean
  message: string
  data?: NERVBridgePromptData
}

export type NERVTamperRulesData = {
  path: string
  source: string
  patterns: string
  count: number
}

export type NERVTamperRulesResponse = {
  success: boolean
  message: string
  data?: NERVTamperRulesData
}

export type NERVToolBackend = 'auto' | 'local' | 'wsl' | 'docker' | 'ssh'

export type NERVToolCatalogItem = {
  name: string
  description: string
  category: string
  command: string
  params: string[]
  binary: string
  checkable: boolean
  available: boolean
}

export type NERVToolCatalogData = {
  tools: NERVToolCatalogItem[]
  count: number
  category_count: number
  base_path: string
}

export type NERVToolsResponse = {
  success: boolean
  message: string
  data?: NERVToolCatalogData
}

export type RunNERVToolRequest = {
  name: string
  args: Record<string, string>
  backend?: NERVToolBackend
  timeout_seconds?: number
}

export type NERVToolRunResult = {
  name: string
  backend: NERVToolBackend
  command: string
  exit_code: number
  stdout: string
  stderr: string
  timed_out: boolean
  duration_ms: number
  output_bytes: number
}

export type RunNERVToolResponse = {
  success: boolean
  message: string
  data?: NERVToolRunResult
}

export type NERVLabActionName =
  | 'tools-check'
  | 'tools-install'
  | 'kali-wsl'
  | 'kali-docker'
  | 'kali-ssh'
  | 'ssh-test'

export type NERVLabActionRequest = {
  action: NERVLabActionName
  backend?: NERVToolBackend
  timeout_seconds?: number
}

export type NERVLabActionResult = {
  action: NERVLabActionName
  backend: NERVToolBackend
  command: string
  exit_code: number
  stdout: string
  stderr: string
  timed_out: boolean
  duration_ms: number
  output_bytes: number
  message: string
}

export type NERVLabActionResponse = {
  success: boolean
  message: string
  data?: NERVLabActionResult
}

export type NERVProxyEvent = {
  ts: number
  request_id: string
  event: string
  target: string
  model: string
  path: string
  method: string
  status_code: number
  injected: boolean
  tampered: boolean
  stream: boolean
  request_bytes: number
  response_bytes: number
  user_preview: string
  reply_preview: string
  technique: string
}

export type NERVProxyStats = {
  total: number
  inject: number
  tamper: number
  stream: number
  chat_inject: number
  responses_inject: number
  chat_tamper: number
  responses_tamper: number
}

export type NERVProxyLogsData = {
  events: NERVProxyEvent[]
  stats: NERVProxyStats
  limit: number
  target?: string
}

export type NERVProxyLogsResponse = {
  success: boolean
  message: string
  data?: NERVProxyLogsData
}

export type NERVProxyStatsResponse = {
  success: boolean
  message: string
  data?: {
    stats: NERVProxyStats
  }
}

export type NERVProxyProcessStatus = {
  running: boolean
  pid: number
  asset_path: string
  script_path: string
  codex_home: string
  pid_path: string
  log_path: string
  listen_url: string
  dashboard_url: string
  listen_open: boolean
  dashboard_open: boolean
  started_at: number
  message: string
  log_tail: string
  python_command: string
  candidates: string[]
}

export type NERVProxyDashboardSnapshot = {
  available: boolean
  status_code: number
  content_type: string
  url: string
  html: string
  message: string
  fetched_at: number
}

export type NERVProxyProcessResult = {
  action: 'start' | 'stop'
  changed: boolean
  message: string
  status: NERVProxyProcessStatus
}

export type NERVProxyProcessStatusResponse = {
  success: boolean
  message: string
  data?: NERVProxyProcessStatus
}

export type NERVProxyDashboardResponse = {
  success: boolean
  message: string
  data?: NERVProxyDashboardSnapshot
}

export type NERVProxyProcessMutationResponse = {
  success: boolean
  message: string
  data?: NERVProxyProcessResult
}

export type NERVDirectProxyStatus = {
  running: boolean
  pid: number
  asset_path: string
  script_path: string
  codex_home: string
  pid_path: string
  log_path: string
  listen_url: string
  listen_open: boolean
  started_at: number
  message: string
  log_tail: string
  python_command: string
  candidates: string[]
}

export type NERVDirectProxyResult = {
  action: 'start' | 'stop'
  changed: boolean
  message: string
  status: NERVDirectProxyStatus
}

export type NERVDirectProxyStatusResponse = {
  success: boolean
  message: string
  data?: NERVDirectProxyStatus
}

export type NERVDirectProxyMutationResponse = {
  success: boolean
  message: string
  data?: NERVDirectProxyResult
}

export type DatasetCaptureModelMode = 'all' | 'selected'
export type DatasetCaptureScopeMode = 'all' | 'selected'

export type DatasetCapturePerformancePolicy = {
  queue_size: number
  workers: number
  buffer_segment_kb: number
  max_sample_mb: number
  max_inflight_mb: number
  spool_threshold_mb: number
  index_queue_size: number
  index_batch_size: number
  index_flush_interval_ms: number
  min_free_disk_gb: number
  max_disk_gb: number
  export_concurrency: number
  export_read_mbps: number
}

export type DatasetCaptureAlertPolicy = {
  enabled: boolean
  recipients: string[]
  types: string[]
  silence_minutes: number
  alert_after_drops: number
  send_recovery: boolean
  access: DatasetCaptureAccessAlertPolicy
}

export type DatasetCaptureAccessAlertPolicy = {
  enabled: boolean
  actions: DatasetCaptureAccessAuditAction[]
  operator_mode: DatasetCaptureScopeMode
  operator_user_ids: number[]
  owner_mode: DatasetCaptureScopeMode
  owner_user_ids: number[]
}

export type DatasetCapturePolicy = {
  version: number
  enabled: boolean
  model_mode: DatasetCaptureModelMode
  models: string[]
  user_mode: DatasetCaptureScopeMode
  user_ids: number[]
  token_mode: DatasetCaptureScopeMode
  token_ids: number[]
  capture_stream: boolean
  preserve_multimodal_base64: boolean
  reasoning_mode: 'full' | 'redacted' | 'disabled'
  reasoning_sample_percent: number
  performance: DatasetCapturePerformancePolicy
  alerts: DatasetCaptureAlertPolicy
}

export type DatasetCapturePolicyResponse = {
  success: boolean
  message?: string
  data: DatasetCapturePolicy
}

export type DatasetCaptureModelOption = {
  id: string
  available: boolean
}

export type DatasetCaptureModelsResponse = {
  success: boolean
  message?: string
  data: {
    models: DatasetCaptureModelOption[]
  }
}

export type DatasetCapturePolicyUserOption = {
  id: number
  username: string
  role: number
}

export type DatasetCapturePolicyTokenOption = {
  id: number
  user_id: number
  name: string
  username: string
}

export type DatasetCaptureSubjectsResponse = {
  success: boolean
  message?: string
  data: {
    users: DatasetCapturePolicyUserOption[]
    operators: DatasetCapturePolicyUserOption[]
    tokens: DatasetCapturePolicyTokenOption[]
  }
}

export type DatasetCaptureWriterStatus = {
  queue_depth: number
  queue_capacity: number
  normalized_depth: number
  index_queue_depth: number
  index_queue_capacity: number
  inflight_bytes: number
  disk_bytes: number
  free_disk_bytes: number
  submitted: number
  written: number
  dropped_queue_full: number
  dropped_sample_too_large: number
  dropped_inflight_limit: number
  build_failed: number
  incomplete_dropped: number
  jsonl_write_failed: number
  index_write_failed: number
  disk_limit_dropped: number
  disk_low_dropped: number
  last_minute: DatasetCaptureActivityWindow
  last_five_minutes: DatasetCaptureActivityWindow
  jsonl_write_p50_ms: number
  jsonl_write_p95_ms: number
  index_write_p50_ms: number
  index_write_p95_ms: number
  last_error_type: string
  last_error_at: number
}

export type DatasetCaptureActivityWindow = {
  submitted: number
  written: number
  dropped_queue_full: number
  dropped_sample_too_large: number
  dropped_inflight_limit: number
  build_failed: number
  incomplete_dropped: number
  jsonl_write_failed: number
  disk_limit_dropped: number
  disk_low_dropped: number
}

export type DatasetCaptureAlertStatus = {
  event_queue_depth: number
  access_queue_depth: number
  mail_queue_depth: number
  events_dropped: number
  access_dropped: number
  access_queued: number
  mail_dropped: number
  send_failed: number
  last_alert_at: number
  last_access_at: number
  last_recovery_at: number
}

export type DatasetCaptureRuntimeStatusResponse = {
  success: boolean
  message?: string
  data: {
    enabled: boolean
    writer_initialized: boolean
    node: string
    writer: DatasetCaptureWriterStatus
    alerts: DatasetCaptureAlertStatus
  }
}

export type DatasetCaptureAccessAuditAction = 'view' | 'download'
export type DatasetCaptureAccessAuditOutcome =
  | 'prepared'
  | 'delivered'
  | 'failed'

export type DatasetCaptureAccessAuditEntry = {
  event_id: string
  action: DatasetCaptureAccessAuditAction
  outcome: DatasetCaptureAccessAuditOutcome
  operator_user_id: number
  operator_username: string
  operator_role: number
  auth_method: string
  ip: string
  node: string
  selection_mode: string
  record_count: number
  user_count: number
  bytes: number
  created_at: number
  completed_at: number
  capture_id: string
  user_id: number
  username: string
  token_id: number
  token_name: string
  user_group: string
  effective_model: string
  channel_id: number
  session_id: string
  capture_created_at: number
}

export type DatasetCaptureAccessAuditResponse = {
  success: boolean
  message?: string
  data: {
    items: DatasetCaptureAccessAuditEntry[]
    total: number
    page: number
    page_size: number
  }
}

export type ConfirmPaymentComplianceResponse = {
  success: boolean
  message: string
  data?: {
    confirmed: boolean
    terms_version: string
    confirmed_at: number
    confirmed_by: number
  }
}

export type SystemTaskStatus = 'pending' | 'running' | 'succeeded' | 'failed'

export type SystemTask<
  TPayload = Record<string, unknown>,
  TState = Record<string, unknown>,
  TResult = Record<string, unknown>,
> = {
  id: number
  task_id: string
  type: string
  status: SystemTaskStatus
  active_key?: string
  payload?: TPayload
  state?: TState
  result?: TResult
  error?: string
  locked_by?: string
  locked_until?: number
  created_at: number
  updated_at: number
}

export type LogCleanupTaskPayload = {
  target_timestamp: number
  batch_size: number
}

export type LogCleanupTaskState = {
  total: number
  processed: number
  progress: number
  remaining: number
}

export type LogCleanupTaskResult = {
  deleted_count: number
}

export type LogCleanupTask = SystemTask<
  LogCleanupTaskPayload,
  LogCleanupTaskState,
  LogCleanupTaskResult
>

export type SystemTaskResponse<TTask = SystemTask | null> = {
  success: boolean
  message: string
  data?: TTask
}

export type SystemTaskListResponse = {
  success: boolean
  message: string
  data?: SystemTask[]
}

export type SiteSettings = {
  SystemName: string
  Logo: string
  Footer: string
  ServerAddress: string
}

export type AuthSettings = {
  PasswordLoginEnabled: boolean
  EmailVerificationEnabled: boolean
  EmailDomainRestrictionEnabled: boolean
  EmailAliasRestrictionEnabled: boolean
  EmailDomainWhitelist: string
  ServerAddress: string
  GitHubOAuthEnabled: boolean
  GitHubClientId: string
  GitHubClientSecret: string
  'discord.enabled': boolean
  'discord.client_id': string
  'discord.client_secret': string
  'oidc.enabled': boolean
  'oidc.client_id': string
  'oidc.client_secret': string
  'oidc.well_known': string
  'oidc.authorization_endpoint': string
  'oidc.token_endpoint': string
  'oidc.user_info_endpoint': string
  TelegramOAuthEnabled: boolean
  TelegramBotToken: string
  TelegramBotName: string
  LinuxDOOAuthEnabled: boolean
  LinuxDOClientId: string
  LinuxDOClientSecret: string
  LinuxDOMinimumTrustLevel: string
  WeChatAuthEnabled: boolean
  WeChatServerAddress: string
  WeChatServerToken: string
  WeChatAccountQRCodeImageURL: string
  TurnstileCheckEnabled: boolean
  TurnstileSiteKey: string
  TurnstileSecretKey: string
  'passkey.enabled': boolean
  'passkey.rp_display_name': string
  'passkey.rp_id': string
  'passkey.origins': string
  'passkey.allow_insecure_origin': boolean
  'passkey.user_verification': 'required' | 'preferred' | 'discouraged'
  'passkey.attachment_preference': '' | 'platform' | 'cross-platform'
}

export type ContentSettings = {
  'console_setting.api_info': string
  'console_setting.announcements': string
  'console_setting.faq': string
  'console_setting.uptime_kuma_groups': string
  'console_setting.api_info_enabled': boolean
  'console_setting.announcements_enabled': boolean
  'console_setting.faq_enabled': boolean
  'console_setting.uptime_kuma_enabled': boolean
  DataExportEnabled: boolean
  DataExportDefaultTime: string
  DataExportInterval: number
  Chats: string
  DrawingEnabled: boolean
  MjNotifyEnabled: boolean
  MjAccountFilterEnabled: boolean
  MjForwardUrlEnabled: boolean
  MjModeClearEnabled: boolean
  MjActionCheckSuccessEnabled: boolean
}

export type ModelSettings = {
  'global.pass_through_request_enabled': boolean
  'global.thinking_model_blacklist': string
  'global.chat_completions_to_responses_policy': string
  'general_setting.ping_interval_enabled': boolean
  'general_setting.ping_interval_seconds': number
  'gemini.safety_settings': string
  'gemini.version_settings': string
  'gemini.supported_imagine_models': string
  'gemini.thinking_adapter_enabled': boolean
  'gemini.thinking_adapter_budget_tokens_percentage': number
  'gemini.function_call_thought_signature_enabled': boolean
  'gemini.remove_function_response_id_enabled': boolean
  'claude.model_headers_settings': string
  'claude.default_max_tokens': string
  'claude.thinking_adapter_enabled': boolean
  'claude.thinking_adapter_budget_tokens_percentage': number
  'grok.violation_deduction_enabled': boolean
  'grok.violation_deduction_amount': number
  ModelPrice: string
  ModelRatio: string
  CacheRatio: string
  CreateCacheRatio: string
  CompletionRatio: string
  ImageRatio: string
  AudioRatio: string
  AudioCompletionRatio: string
  ExposeRatioEnabled: boolean
  'billing_setting.billing_mode': string
  'billing_setting.billing_expr': string
  'tool_price_setting.prices': string
  TopupGroupRatio: string
  GroupRatio: string
  UserUsableGroups: string
  GroupGroupRatio: string
  AutoGroups: string
  DefaultUseAutoGroup: boolean
  'group_ratio_setting.group_special_usable_group': string
  RetryTimes: number
}

export type BillingSettings = {
  QuotaForNewUser: number
  PreConsumedQuota: number
  QuotaForInviter: number
  QuotaForInvitee: number
  TopUpLink: string
  'general_setting.docs_link': string
  'quota_setting.enable_free_model_pre_consume': boolean
  QuotaPerUnit: number
  USDExchangeRate: number
  'general_setting.quota_display_type': string
  'general_setting.custom_currency_symbol': string
  'general_setting.custom_currency_exchange_rate': number
  DisplayInCurrencyEnabled: boolean
  DisplayTokenStatEnabled: boolean
  ModelPrice: string
  ModelRatio: string
  CacheRatio: string
  CreateCacheRatio: string
  CompletionRatio: string
  ImageRatio: string
  AudioRatio: string
  AudioCompletionRatio: string
  ExposeRatioEnabled: boolean
  'billing_setting.billing_mode': string
  'billing_setting.billing_expr': string
  'tool_price_setting.prices': string
  TopupGroupRatio: string
  GroupRatio: string
  UserUsableGroups: string
  GroupGroupRatio: string
  AutoGroups: string
  DefaultUseAutoGroup: boolean
  'group_ratio_setting.group_special_usable_group': string
  PayAddress: string
  EpayId: string
  EpayKey: string
  Price: number
  MinTopUp: number
  CustomCallbackAddress: string
  PayMethods: string
  'payment_setting.amount_options': string
  'payment_setting.amount_discount': string
  'payment_setting.compliance_confirmed': boolean
  'payment_setting.compliance_terms_version': string
  'payment_setting.compliance_confirmed_at': number
  'payment_setting.compliance_confirmed_by': number
  'payment_setting.compliance_confirmed_ip': string
  StripeApiSecret: string
  StripeWebhookSecret: string
  StripePriceId: string
  StripeUnitPrice: number
  StripeMinTopUp: number
  StripePromotionCodesEnabled: boolean
  CreemApiKey: string
  CreemWebhookSecret: string
  CreemTestMode: boolean
  CreemProducts: string
  WaffoEnabled: boolean
  WaffoApiKey: string
  WaffoPrivateKey: string
  WaffoPublicCert: string
  WaffoSandboxPublicCert: string
  WaffoSandboxApiKey: string
  WaffoSandboxPrivateKey: string
  WaffoSandbox: boolean
  WaffoMerchantId: string
  WaffoCurrency: string
  WaffoUnitPrice: number
  WaffoMinTopUp: number
  WaffoNotifyUrl: string
  WaffoReturnUrl: string
  WaffoPayMethods: string
  WaffoPancakeMerchantID: string
  WaffoPancakePrivateKey: string
  WaffoPancakeReturnURL: string
  // Bound by the operator through the catalog flow in the admin Pancake
  // section (saved via /api/option/waffo-pancake/save).
  WaffoPancakeStoreID: string
  WaffoPancakeProductID: string
  'checkin_setting.enabled': boolean
  'checkin_setting.min_quota': number
  'checkin_setting.max_quota': number
}

export type OperationsSettings = {
  DefaultCollapseSidebar: boolean
  DemoSiteEnabled: boolean
  SelfUseModeEnabled: boolean
  QuotaRemindThreshold: string
  SMTPServer: string
  SMTPPort: string
  SMTPAccount: string
  SMTPFrom: string
  SMTPToken: string
  SMTPSSLEnabled: boolean
  SMTPStartTLSEnabled: boolean
  SMTPInsecureSkipVerify: boolean
  SMTPForceAuthLogin: boolean
  WorkerUrl: string
  WorkerValidKey: string
  WorkerAllowHttpImageRequestEnabled: boolean
  LogConsumeEnabled: boolean
  UsageLogIPCaptureEnabled: boolean
  TrustedProxyCIDRs: string
  UsageLogExportDirectLimit: number
  UsageLogExportMaxRows: number
  UsageLogExportBatchSize: number
  UsageLogExportRetentionHours: number
  'performance_setting.disk_cache_enabled': boolean
  'performance_setting.disk_cache_threshold_mb': number
  'performance_setting.disk_cache_max_size_mb': number
  'performance_setting.disk_cache_path': string
  'performance_setting.monitor_enabled': boolean
  'performance_setting.monitor_cpu_threshold': number
  'performance_setting.monitor_memory_threshold': number
  'performance_setting.monitor_disk_threshold': number
  'perf_metrics_setting.enabled': boolean
  'perf_metrics_setting.flush_interval': number
  'perf_metrics_setting.bucket_time': 'hour' | 'minute' | '5min'
  'perf_metrics_setting.retention_days': number
  'nerv_setting.enabled': boolean
  'nerv_setting.prompt': string
  'nerv_setting.mode': 'prepend' | 'append' | 'override'
  'nerv_setting.models': string
  'nerv_setting.chat_enabled': boolean
  'nerv_setting.responses_enabled': boolean
  'nerv_setting.skills_enabled': boolean
  'nerv_setting.skills_limit': number
  'nerv_setting.tamper_enabled': boolean
  'nerv_setting.tamper_reply': string
  'nerv_setting.tamper_patterns': string
  'nerv_setting.targets': string
  'nerv_setting.mcp_backend': 'auto' | 'local' | 'wsl' | 'docker' | 'ssh'
  'nerv_setting.wsl_distro': string
  'nerv_setting.docker_container': string
  'nerv_setting.ssh_host': string
  'nerv_stats.total': number
  'nerv_stats.inject': number
  'nerv_stats.tamper': number
  'nerv_stats.chat_inject': number
  'nerv_stats.responses_inject': number
  'nerv_stats.chat_tamper': number
  'nerv_stats.responses_tamper': number
  'nerv_stats.last_event_at': number
  'nerv_stats.last_event': string
  'nerv_stats.last_target': string
  'nerv_stats.last_model': string
  'nerv_stats.recent': string
}

export type SecuritySettings = {
  ModelRequestRateLimitEnabled: boolean
  ModelRequestRateLimitCount: number
  ModelRequestRateLimitSuccessCount: number
  ModelRequestRateLimitDurationMinutes: number
  ModelRequestRateLimitGroup: string
  CheckSensitiveEnabled: boolean
  CheckSensitiveOnPromptEnabled: boolean
  SensitiveWords: string
  'fetch_setting.enable_ssrf_protection': boolean
  'fetch_setting.allow_private_ip': boolean
  'fetch_setting.domain_filter_mode': boolean
  'fetch_setting.ip_filter_mode': boolean
  'fetch_setting.domain_list': string[]
  'fetch_setting.ip_list': string[]
  'fetch_setting.allowed_ports': number[]
  'fetch_setting.apply_ip_filter_for_domain': boolean
  'token_setting.max_user_tokens': number
}

export type UpstreamChannel = {
  id: number
  name: string
  base_url: string
  status: number
  type?: number
}

export type RatioType =
  | 'model_ratio'
  | 'completion_ratio'
  | 'cache_ratio'
  | 'create_cache_ratio'
  | 'image_ratio'
  | 'audio_ratio'
  | 'audio_completion_ratio'
  | 'model_price'
  | 'billing_mode'
  | 'billing_expr'

export type RatioDifference = {
  current: number | string | null
  upstreams: Record<string, number | string | 'same'>
  confidence: Record<string, boolean>
}

export type DifferencesMap = Record<
  string,
  Partial<Record<RatioType, RatioDifference>>
>

export type UpstreamChannelsResponse = {
  success: boolean
  message: string
  data: UpstreamChannel[]
}

export type UpstreamConfig = {
  id: number
  name: string
  base_url: string
  endpoint: string
}

export type FetchUpstreamRatiosRequest = {
  upstreams: UpstreamConfig[]
  timeout: number
}

export type TestResult = {
  name: string
  status: 'success' | 'error'
  error?: string
}

export type UpstreamRatiosResponse = {
  success: boolean
  message: string
  data: {
    differences: DifferencesMap
    test_results: TestResult[]
  }
}
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
