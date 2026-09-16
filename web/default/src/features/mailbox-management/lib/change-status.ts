import type { Account } from '../types'

export function statusTargets(accounts: Account[]): string[] {
  const transitions: Record<string, string[]> = {
    pending: ['rejected'],
    rejected: ['pending'],
    submitted: ['approved', 'rejected'],
    approved: ['pending', 'rejected'],
  }
  return ['pending', 'approved', 'rejected'].filter(
    (target) =>
      accounts.length > 0 &&
      accounts.every(
        (account) =>
          !account.archived_at &&
          account.assignment_id > 0 &&
          transitions[account.status]?.includes(target)
      )
  )
}
