export type AccountType = 'refund' | 'opening'
export type CredentialKind = 'password' | 'otp' | 'card'
export type MailboxStatus =
  | 'unassigned'
  | 'pending'
  | 'submitted'
  | 'approved'
  | 'rejected'
  | 'issue_pending'
export interface Page<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  has_more: boolean
}
export interface ListQuery {
  archived?: boolean
  kind?: string
  assignment_id?: number
  account_type?: AccountType
  page: number
  page_size: number
  search?: string
  status?: string
  operator_id?: number
}
export interface Account {
  archived_at?: number
  archived_by?: number
  account_type?: AccountType
  card_last4?: string
  id: number
  email: string
  version: number
  assignment_id: number
  assignment_version?: number
  operator_id: number
  operator_name: string
  status: MailboxStatus
  assigned_at: number
  credentials_available: boolean
}
export interface Operator {
  id: number
  username: string
  display_name: string
  enabled: boolean
  version: number
  created_at: number
  updated_at: number
  generated_password?: string
}
export interface OperatorInput {
  username: string
  display_name: string
  enabled: boolean
  version: number
  password?: string
}
export interface OperatorOption {
  id: number
  name?: string
  username?: string
  display_name?: string
}
export interface AccountOperator {
  id: number
  username: string
  display_name: string
}
export interface VersionedID {
  id: number
  version: number
}
export interface AssignInput {
  account_type?: AccountType
  items: VersionedID[]
  operator_id: number
}
export interface Attachment {
  id: string
  content_type: string
  size: number
  width: number
  height: number
  created_at: number
  expires_at: number
  deleted_at: number
}
export interface Submission {
  account_type?: AccountType
  card_last4?: string
  id: number
  assignment_id: number
  account_id: number
  email: string
  operator_id: number
  operator_name: string
  status: 'pending' | 'submitted' | 'approved' | 'rejected'
  version: number
  review_reason: string
  reviewed_by: number
  reviewed_at: number
  created_at: number
  attachments: Attachment[]
}
export interface ReviewInput {
  account_type?: AccountType
  version: number
  status: 'approved' | 'rejected'
  reason: string
}

export type IssueKind = 'email_login' | 'otp' | 'card' | 'other'
export interface Issue {
  submitted_version: number
  id: number
  account_id: number
  account_type: AccountType
  email: string
  card_last4: string
  account_version: number
  assignment_id: number
  assignment_version: number
  assignment_active: boolean
  operator_id: number
  operator_name: string
  kind: IssueKind
  description: string
  status: 'pending' | 'resolved' | 'invalidated'
  version: number
  resolution: string
  reply: string
  resolved_at: number
  created_at: number
  attachments: Attachment[]
}
export interface ResolveIssueInput {
  version: number
  account_version: number
  assignment_version: number
  resolution: 'resume' | 'recall'
  reply: string
  credentials?: {
    password?: string
    otp?: string
    card_number?: string
    card_expiry?: string
  }
}
export interface Audit {
  account_type?: AccountType
  card_last4?: string
  target_operator_id?: number
  id: number
  admin_id: number
  operator_id: number
  account_id: number
  assignment_id: number
  action: string
  status_code: number
  error_code: string
  ip_address: string
  created_at: number
}
export type ImportFormat = 'text' | 'csv' | 'xlsx'
export type ImportSource = { account_type?: AccountType } & (
  | { format: ImportFormat; file: File; text?: never }
  | { format: 'text' | 'csv'; text: string; file?: never }
)
export interface ImportPreview {
  rows: Array<{
    row: number
    email: string
    account_type?: AccountType
    card_last4?: string
  }>
  issues: Array<{ row: number; code: string }>
  total: number
  valid: boolean
}
export interface Credential {
  card_number?: string
  card_expiry?: string
  password?: string
  code?: string
  expires_at?: number
  server_time: number
}
export interface Envelope<T> {
  success: boolean
  message: string
  data: T
}
