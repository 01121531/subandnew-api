import { MailboxRequestError } from '../api'

export function errorKey(error: unknown): string {
  if (!(error instanceof MailboxRequestError)) {
    return 'mailboxPortal.requestFailed'
  }
  const known: Record<string, string> = {
    mailbox_invalid_credentials: 'invalidLogin',
    mailbox_current_password_incorrect: 'currentPasswordIncorrect',
    mailbox_current_password_invalid: 'currentPasswordIncorrect',
    mailbox_credentials_revoked: 'credentialsBlocked',
    mailbox_assignment_changed: 'assignmentChanged',
    mailbox_attachment_count: 'attachmentCount',
    mailbox_invalid_image: 'invalidImage',
    mailbox_attachment_too_large: 'imageTooLarge',
    mailbox_attachment_expired: 'imageExpired',
    mailbox_invalid_password: 'passwordInvalid',
    mailbox_draft_limit: 'draftLimit',
    mailbox_clipboard_empty: 'clipboardEmpty',
    mailbox_clipboard_unavailable: 'clipboardUnavailable',
  }
  if (known[error.code]) return `mailboxPortal.${known[error.code]}`
  if (error.status === 401) return 'mailboxPortal.sessionExpired'
  if (error.status === 403) return 'mailboxPortal.forbidden'
  if (error.status === 404 || error.status === 409) {
    return 'mailboxPortal.assignmentChanged'
  }
  if (error.status === 410) return 'mailboxPortal.imageExpired'
  if (error.status === 413) return 'mailboxPortal.imageTooLarge'
  if (error.status === 429) return 'mailboxPortal.rateLimited'
  return 'mailboxPortal.requestFailed'
}
