import type {
  AccountType,
  RepairCandidate,
  RepairInput,
  RepairItem,
} from '../types'
import { MailboxError } from './errors'

export const repairLimit = 1000

export function repairItem(value: RepairItem): RepairItem {
  return {
    account_id: value.account_id,
    account_version: value.account_version,
    original_assignment_id: value.original_assignment_id,
    original_assignment_version: value.original_assignment_version,
    current_assignment_id: value.current_assignment_id,
    current_assignment_version: value.current_assignment_version,
    submission_id: value.submission_id,
    submission_version: value.submission_version,
  }
}

export function repairMetadata(
  value: RepairCandidate,
  pool: AccountType
): RepairCandidate {
  if (value.account_type !== pool) {
    throw new MailboxError('mailbox_not_found', 404)
  }
  return {
    ...repairItem(value),
    later_assignments: (value.later_assignments ?? []).map((item) => ({
      id: item.id,
      operator_id: item.operator_id,
      operator_name: item.operator_name,
      status: item.status,
      assigned_at: item.assigned_at,
      revoked_at: item.revoked_at,
    })),
    account_type: pool,
    email: value.email,
    original_operator_id: value.original_operator_id,
    original_operator_name: value.original_operator_name,
    original_revoked_at: value.original_revoked_at,
    submission_status: value.submission_status,
    submitted_at: value.submitted_at,
    review_reason: value.review_reason,
    reviewed_at: value.reviewed_at,
    current_operator_name: value.current_operator_name,
    current_status: value.current_status,
    current_assigned_at: value.current_assigned_at,
    can_repair: value.can_repair === true,
    conflict_code:
      typeof value.conflict_code === 'string' &&
      /^[a-z0-9_]{1,100}$/.test(value.conflict_code)
        ? value.conflict_code
        : '',
  }
}

export function repairBody(input: RepairInput): RepairInput {
  const items = input.items.map(repairItem)
  if (
    !['refund', 'opening'].includes(input.account_type) ||
    !input.reason.trim() ||
    input.reason.trim().length > 2000 ||
    !items.length ||
    items.length > repairLimit ||
    new Set(items.map((item) => item.account_id)).size !== items.length ||
    items.some(
      (item) =>
        Object.entries(item).some(
          ([key, value]) =>
            !Number.isSafeInteger(value) ||
            value < (key.startsWith('current_assignment_') ? 0 : 1)
        ) ||
        (item.current_assignment_id === 0) !==
          (item.current_assignment_version === 0)
    )
  ) {
    throw new MailboxError('mailbox_repair_invalid')
  }
  return {
    account_type: input.account_type,
    reason: input.reason.trim(),
    items,
  }
}
