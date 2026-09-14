type AccountStatus =
  | 'unassigned'
  | 'pending'
  | 'submitted'
  | 'approved'
  | 'rejected'

export type Session =
  | { authenticated: false }
  | {
      authenticated: true
      operator: { id: number; username: string; display_name: string }
      csrf_token: string
      expires_at: number
    }

export type Account = {
  id: number
  email: string
  version: number
  assignment_id: number
  assignment_version: number
  operator_id?: number
  operator_name?: string
  status: AccountStatus
  assigned_at?: number
  credentials_available: boolean
}

export type Attachment = {
  id: string
  content_type: string
  size: number
  width: number
  height: number
  created_at: number
  expires_at: number
  deleted_at: number
}

export type Submission = {
  id: number
  assignment_id: number
  account_id: number
  email: string
  operator_id: number
  operator_name: string
  status: 'pending' | 'approved' | 'rejected'
  version: number
  review_reason: string
  reviewed_by: number
  reviewed_at: number
  created_at: number
  attachments: Attachment[]
}

export type Page<T> = {
  items: T[]
  total: number
  page: number
  page_size: number
  has_more: boolean
}
export type ListQuery = {
  search?: string
  status?: string
  page: number
  page_size: number
}
export type Credential = {
  password?: string
  code?: string
  expires_at?: number
  server_time: number
}
