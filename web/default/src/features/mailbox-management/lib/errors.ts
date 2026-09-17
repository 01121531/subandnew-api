export class MailboxError extends Error {
  constructor(
    public code: string,
    public status = 0,
    public conflicts: Array<{ id: number; email: string }> = []
  ) {
    super(code)
    this.name = 'MailboxError'
  }
}
export function assignmentConflicts(
  code: unknown,
  data: unknown
): Array<{ id: number; email: string }> {
  if (
    code !== 'mailbox_already_submitted' ||
    !data ||
    typeof data !== 'object'
  ) {
    return []
  }
  const values = (data as { conflicts?: unknown }).conflicts
  if (!Array.isArray(values)) return []
  return values
    .slice(0, 1000)
    .flatMap((item) =>
      item &&
      Number.isSafeInteger(item.id) &&
      item.id > 0 &&
      typeof item.email === 'string'
        ? [{ id: item.id, email: item.email.slice(0, 320) }]
        : []
    )
}
export function safeCode(value: unknown): string {
  return typeof value === 'string' && /^mailbox_[a-z0-9_]{1,80}$/.test(value)
    ? value
    : 'mailbox_request_failed'
}
export function errorKey(error: unknown): string {
  return `mailbox.errors.${error instanceof MailboxError ? safeCode(error.code) : 'mailbox_request_failed'}`
}
