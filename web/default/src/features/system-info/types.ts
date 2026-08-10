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
export type SystemInstanceStatus = 'online' | 'stale'

type SystemInstanceInfo = {
  schema_version?: number
  node?: {
    name?: string
    source?: string
    manually_configured?: boolean
    should_configure_manually?: boolean
    [key: string]: unknown
  }
  role?: {
    is_master?: boolean
    [key: string]: unknown
  }
  runtime?: {
    version?: string
    goos?: string
    goarch?: string
    started_at?: number
    [key: string]: unknown
  }
  host?: {
    hostname?: string
    [key: string]: unknown
  }
  resources?: {
    cpu?: {
      usage_percent?: number
      [key: string]: unknown
    }
    memory?: {
      usage_percent?: number
      [key: string]: unknown
    }
    storage?: {
      total_bytes?: number
      used_bytes?: number
      free_bytes?: number
      used_percent?: number
      [key: string]: unknown
    }
    [key: string]: unknown
  }
  [key: string]: unknown
}

export type SystemInstance = {
  node_name: string
  status: SystemInstanceStatus
  stale_after_seconds: number
  started_at: number
  last_seen_at: number
  info?: SystemInstanceInfo
}

export type SystemInstanceListResponse = {
  success: boolean
  message: string
  data?: SystemInstance[]
}

export type SystemInstanceDeleteResponse = {
  success: boolean
  message: string
  data?: {
    deleted_count: number
  }
}

export type SystemUpdateCapability = {
  supported: boolean
  reason?: string
  platform: string
  arch: string
}

export type SystemUpdateRelease = {
  id: number
  tag_name: string
  name?: string
  body?: string
  html_url?: string
  published_at?: string
  current_version: string
  update_available: boolean
  installable: boolean
  reason?: string
  asset_name?: string
}

export type SystemUpdatePhase =
  | 'idle'
  | 'downloading'
  | 'verifying'
  | 'staged'
  | 'restarting'
  | 'validating'
  | 'succeeded'
  | 'failed'
  | 'rolling_back'
  | 'rolled_back'

export type SystemUpdateState = {
  task_id?: string
  phase: SystemUpdatePhase
  progress: number
  current_version?: string
  target_version?: string
  release_id?: number
  message_code?: string
  error_code?: string
  started_at?: number
  updated_at?: number
  completed_at?: number
  restart_required: boolean
}

export type SystemUpdateResponse<T> = {
  success: boolean
  message: string
  data: T
}
