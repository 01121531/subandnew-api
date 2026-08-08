export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface UserProfile {
  id: number
  username: string
  display_name: string
  role: number
  status: number
  email?: string
  github_id?: string
  discord_id?: string
  oidc_id?: string
  wechat_id?: string
  telegram_id?: string
  linux_do_id?: string
  language?: string
}

export interface UpdateUserRequest {
  username?: string
  display_name?: string
  password?: string
  original_password?: string
}

export interface TwoFAStatus {
  enabled: boolean
  locked: boolean
  backup_codes_remaining: number
}

export interface TwoFASetupData {
  secret: string
  qr_code_data: string
  backup_codes: string[]
}
