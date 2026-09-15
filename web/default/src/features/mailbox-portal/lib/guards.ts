import { MailboxRequestError } from '../api'
import type { Account, Credential } from '../types'

export function canReadCredentials(account: Account): boolean {
  return (
    account.assignment_id > 0 &&
    account.credentials_available &&
    ['pending', 'submitted', 'rejected'].includes(account.status)
  )
}

export function canSubmit(account: Account): boolean {
  return (
    account.assignment_id > 0 &&
    account.assignment_version > 0 &&
    ['pending', 'rejected'].includes(account.status)
  )
}

export function assertCurrentAssignment(
  expected: Account,
  actual: Account,
  purpose: 'credentials' | 'submit'
): void {
  // Draft uploads bump the account row version without changing the assignment.
  if (
    actual.id !== expected.id ||
    (actual.account_type ?? 'refund') !== (expected.account_type ?? 'refund') ||
    actual.assignment_id !== expected.assignment_id ||
    actual.assignment_version !== expected.assignment_version
  ) {
    throw new MailboxRequestError('mailbox_assignment_changed', 409)
  }
  if (purpose === 'credentials' && !canReadCredentials(actual)) {
    throw new MailboxRequestError('mailbox_credentials_revoked', 403)
  }
  if (purpose === 'credentials' && actual.status !== expected.status) {
    throw new MailboxRequestError('mailbox_assignment_changed', 409)
  }
  if (purpose === 'submit' && !canSubmit(actual)) {
    throw new MailboxRequestError('mailbox_assignment_changed', 409)
  }
}

export function otpSeconds(
  credential: Credential,
  receivedAt: number,
  now: number
): number {
  if (!credential.expires_at || !Number.isFinite(credential.server_time)) {
    return 0
  }
  return Math.max(
    0,
    Math.ceil(
      credential.expires_at - credential.server_time - (now - receivedAt) / 1000
    )
  )
}

export function validateImages(files: File[], existing: number): void {
  if (!files.length || files.length + existing > 5) {
    throw new MailboxRequestError('mailbox_attachment_count', 400)
  }
  for (const file of files) {
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) {
      throw new MailboxRequestError('mailbox_invalid_image', 400)
    }
    if (file.size <= 0 || file.size > 10 * 1024 * 1024) {
      throw new MailboxRequestError('mailbox_attachment_too_large', 400)
    }
  }
}
