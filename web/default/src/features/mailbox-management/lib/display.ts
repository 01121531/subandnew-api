export function statusLabelKey(status: string, review = false): string {
  if (review && status === 'pending') return 'mailbox.admin.awaitingReview'
  return `mailbox.admin.statuses.${status}`
}
