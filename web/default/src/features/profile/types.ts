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
