import type {
  Account,
  AccountType,
  Audit,
  ImportPreview,
  Issue,
  Page,
  Submission,
} from '../types'
import { MailboxError } from './errors'
import { importFailuresMetadata } from './import-report'

function cardLast4(value: unknown): string {
  return typeof value === 'string' && /^\d{4}$/.test(value) ? value : ''
}

// Explicit DTO projections keep unexpected credential fields out of query caches.
export function accountMetadata(
  value: Account,
  accountType: AccountType = 'refund'
): Account {
  assertPool(value, accountType)
  return {
    id: value.id,
    email: value.email,
    account_type: value.account_type ?? 'refund',
    card_last4: cardLast4(value.card_last4),
    version: value.version,
    assignment_id: value.assignment_id,
    assignment_version: value.assignment_version,
    operator_id: value.operator_id,
    operator_name: value.operator_name,
    status: value.status,
    assigned_at: value.assigned_at,
    credentials_available: value.credentials_available,
    archived_at: value.archived_at ?? 0,
    archived_by: value.archived_by ?? 0,
  }
}

function assertPool(
  value: { account_type?: AccountType },
  accountType: AccountType
) {
  if ((value.account_type ?? 'refund') !== accountType) {
    throw new MailboxError('mailbox_not_found', 404)
  }
}

export function issueMetadata(value: Issue, accountType: AccountType): Issue {
  assertPool(value, accountType)
  return {
    submitted_version: value.submitted_version,
    id: value.id,
    account_id: value.account_id,
    account_type: accountType,
    email: value.email,
    card_last4: cardLast4(value.card_last4),
    account_version: value.account_version,
    assignment_id: value.assignment_id,
    assignment_version: value.assignment_version,
    assignment_active: value.assignment_active,
    operator_id: value.operator_id,
    operator_name: value.operator_name,
    kind: value.kind,
    description: value.description,
    status: value.status,
    version: value.version,
    resolution: value.resolution,
    reply: value.reply,
    resolved_at: value.resolved_at,
    created_at: value.created_at,
    attachments: (value.attachments ?? []).map((item) => ({
      id: item.id,
      content_type: item.content_type,
      size: item.size,
      width: item.width,
      height: item.height,
      created_at: item.created_at,
      expires_at: item.expires_at,
      deleted_at: item.deleted_at,
    })),
  }
}

export function pageMetadata<T>(
  value: Page<T>,
  project: (item: T) => T
): Page<T> {
  return {
    items: value.items.map(project),
    total: value.total,
    page: value.page,
    page_size: value.page_size,
    has_more: value.has_more,
  }
}

export function submissionMetadata(
  value: Submission,
  accountType: AccountType
): Submission {
  assertPool(value, accountType)
  return {
    id: value.id,
    assignment_id: value.assignment_id,
    account_id: value.account_id,
    email: value.email,
    account_type: value.account_type ?? 'refund',
    card_last4: cardLast4(value.card_last4),
    operator_id: value.operator_id,
    operator_name: value.operator_name,
    status: value.status,
    version: value.version,
    review_reason: value.review_reason,
    remark: typeof value.remark === 'string' ? value.remark : '',
    reviewed_by: value.reviewed_by,
    reviewed_at: value.reviewed_at,
    created_at: value.created_at,
    attachments:
      value.attachments?.map((item) => ({
        id: item.id,
        content_type: item.content_type,
        size: item.size,
        width: item.width,
        height: item.height,
        created_at: item.created_at,
        expires_at: item.expires_at,
        deleted_at: item.deleted_at,
      })) ?? [],
  }
}

export function auditMetadata(value: Audit): Audit {
  return {
    from_status: value.from_status,
    to_status: value.to_status,
    reason: value.reason,
    id: value.id,
    target_operator_id: value.target_operator_id,
    admin_id: value.admin_id,
    operator_id: value.operator_id,
    account_id: value.account_id,
    assignment_id: value.assignment_id,
    account_type: value.account_type ?? 'refund',
    card_last4: cardLast4(value.card_last4),
    action: value.action,
    status_code: value.status_code,
    error_code: value.error_code,
    ip_address: value.ip_address,
    created_at: value.created_at,
  }
}

export function importMetadata(value: ImportPreview): ImportPreview {
  return {
    ready: value.ready,
    failures: importFailuresMetadata(value.failures),
    total: value.total,
    valid: value.valid,
    rows: value.rows.map((row) => ({
      row: row.row,
      email: row.email,
      account_type: row.account_type ?? 'refund',
      card_last4: cardLast4(row.card_last4),
      otp_available: row.otp_available === true,
    })),
    issues: value.issues.map((issue) => ({ row: issue.row, code: issue.code })),
    notices: (value.notices ?? []).map((notice) => ({
      row: notice.row,
      code: notice.code,
    })),
  }
}
