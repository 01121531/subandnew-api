import { hasPermission } from '@/lib/admin-permissions'
import type { AuthUser } from '@/stores/auth-store'

export const mailboxActions = [
  'view',
  'manage',
  'assign',
  'credentials',
  'operators',
  'review',
  'audit',
] as const
export type MailboxAction = (typeof mailboxActions)[number]
export function canMailbox(
  user: AuthUser | null | undefined,
  action: MailboxAction
): boolean {
  return hasPermission(user, 'mailbox_management', action)
}
export function canAccessMailbox(user: AuthUser | null | undefined): boolean {
  return mailboxActions.some((action) => canMailbox(user, action))
}
export function mailboxTabs(user: AuthUser | null | undefined): string[] {
  const tabs: string[] = []
  if (mailboxActions.slice(0, 4).some((action) => canMailbox(user, action))) {
    tabs.push('pool')
  }
  if (canMailbox(user, 'operators')) tabs.push('operators')
  if (canMailbox(user, 'review')) tabs.push('review', 'issues')
  if (canMailbox(user, 'audit')) tabs.push('audit')
  return tabs
}
