export type SystemOption = { key: string; value: string }

export type SystemOptionsResponse = {
  success: boolean
  message: string
  data: SystemOption[]
}

export type UpdateOptionRequest = {
  key: string
  value: string | boolean | number
}

export type UpdateOptionResponse = { success: boolean; message: string }

export type SystemTaskStatus = 'pending' | 'running' | 'succeeded' | 'failed'

export type SystemTask = {
  id: number
  task_id: string
  type: string
  status: SystemTaskStatus
  active_key?: string
  payload?: Record<string, unknown>
  state?: Record<string, unknown>
  result?: Record<string, unknown>
  error?: string
  locked_by?: string
  locked_until?: number
  created_at: number
  updated_at: number
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
