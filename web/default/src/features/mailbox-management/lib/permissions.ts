import { hasPermission } from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
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
export function canMailboxWork(user: AuthUser | null | undefined): boolean {
  return (
    !!user &&
    (user.role === ROLE.ADMIN || user.role === ROLE.SUPER_ADMIN) &&
    canMailbox(user, 'operators') &&
    canMailbox(user, 'view') &&
    canMailbox(user, 'review')
  )
}
export function mailboxTabs(user: AuthUser | null | undefined): string[] {
  const tabs: string[] = []
  if (mailboxActions.slice(0, 4).some((action) => canMailbox(user, action))) {
    tabs.push('pool')
  }
  if (canMailbox(user, 'operators')) tabs.push('operators')
  if (canMailbox(user, 'review')) tabs.push('review', 'issues')
  if (canMailboxRepair(user)) tabs.push('repairs')
  if (canMailbox(user, 'audit')) tabs.push('audit')
  return tabs
}
export function canMailboxRepair(user: AuthUser | null | undefined): boolean {
  return (['view', 'assign', 'review'] as const).every((action) =>
    canMailbox(user, action)
  )
}
