export type MailboxStatus =
  | 'unassigned'
  | 'pending'
  | 'submitted'
  | 'approved'
  | 'rejected'
export interface Page<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  has_more: boolean
}
export interface ListQuery {
  page: number
  page_size: number
  search?: string
  status?: string
  operator_id?: number
}
export interface Account {
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
  version: number
  status: 'approved' | 'rejected'
  reason: string
}
export interface Audit {
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
export type ImportSource =
  | { format: ImportFormat; file: File; text?: never }
  | { format: 'text' | 'csv'; text: string; file?: never }
export interface ImportPreview {
  rows: Array<{ row: number; email: string }>
  issues: Array<{ row: number; code: string }>
  total: number
  valid: boolean
}
export interface Credential {
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
