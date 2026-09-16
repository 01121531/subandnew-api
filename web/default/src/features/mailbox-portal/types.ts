export type AccountType = 'refund' | 'opening'
export type CredentialKind = 'password' | 'otp' | 'card' | 'cvv'

type AccountStatus =
  | 'unassigned'
  | 'pending'
  | 'submitted'
  | 'approved'
  | 'rejected'
  | 'issue_pending'

export type Session =
  | { authenticated: false }
  | {
      authenticated: true
      operator: { id: number; username: string; display_name: string }
      csrf_token: string
      expires_at: number
    }

export type Account = {
  account_type?: AccountType
  card_last4?: string
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
  can_edit_remark?: boolean
  account_type?: AccountType
  card_last4?: string
  id: number
  assignment_id: number
  account_id: number
  email: string
  operator_id: number
  operator_name: string
  status: 'pending' | 'approved' | 'rejected'
  version: number
  review_reason: string
  remark?: string
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
  account_type?: AccountType
  search?: string
  status?: string
  page: number
  page_size: number
}
export type Credential = {
  persistent?: boolean
  available?: boolean
  card_number?: string
  card_expiry?: string
  password?: string
  code?: string
  cvv?: string
  expires_at?: number
  server_time: number
}
