import {
  invalidateAccountCredentials,
  mailboxApi,
  MailboxRequestError,
} from '../api'
import type { Account, Credential, CredentialKind } from '../types'
import { assertCurrentAssignment, canReadCredentials } from './guards'

export function revealRemaining(startedAt: number, now: number): number {
  return Math.max(0, 60_000 - Math.max(0, now - startedAt))
}

export function credentialScope(account: Account): string {
  return [
    account.id,
    account.account_type ?? 'refund',
    account.assignment_id,
    account.assignment_version,
    account.status,
    account.credentials_available,
    account.operator_id ?? '',
  ].join(':')
}

export async function readCredential(
  csrf: string,
  account: Account,
  kind: CredentialKind,
  signal: AbortSignal
): Promise<Credential> {
  try {
    const accountType = account.account_type ?? 'refund'
    if (
      !canReadCredentials(account) ||
      (kind === 'card' && accountType !== 'opening')
    ) {
      throw new MailboxRequestError('mailbox_credentials_revoked', 403)
    }
    assertCurrentAssignment(
      account,
      await mailboxApi.account(account.id, signal, accountType),
      'credentials'
    )
    const value = await mailboxApi.credentials(
      csrf,
      account.id,
      kind,
      signal,
      accountType
    )
    // Reject a late secret if ownership/review changed while it was being fetched.
    assertCurrentAssignment(
      account,
      await mailboxApi.account(account.id, signal, accountType),
      'credentials'
    )
    return value
  } catch (error) {
    invalidateAccountCredentials(account.id, error)
    throw error
  }
}
