export class MailboxError extends Error {
  constructor(
    public code: string,
    public status = 0
  ) {
    super(code)
    this.name = 'MailboxError'
  }
}
export function safeCode(value: unknown): string {
  return typeof value === 'string' && /^mailbox_[a-z0-9_]{1,80}$/.test(value)
    ? value
    : 'mailbox_request_failed'
}
export function errorKey(error: unknown): string {
  return `mailbox.errors.${error instanceof MailboxError ? safeCode(error.code) : 'mailbox_request_failed'}`
}
