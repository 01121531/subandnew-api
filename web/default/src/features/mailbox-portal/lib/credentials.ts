import {
  invalidateAccountCredentials,
  mailboxApi,
  MailboxRequestError,
} from '../api'
import type { Account, Credential, CredentialKind } from '../types'
import { assertCurrentAssignment, canReadCredentials } from './guards'

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
      ((kind === 'card' || kind === 'cvv') && accountType !== 'opening')
    ) {
      throw new MailboxRequestError('mailbox_credentials_revoked', 403)
    }
    assertCurrentAssignment(
      account,
      await mailboxApi.account(account.id, signal, accountType),
      'credentials'
    )
    // Submitted tasks retain login/card access, but cannot read CVV.
    if (kind === 'cvv' && account.status === 'submitted') {
      throw new MailboxRequestError('mailbox_cvv_task_restricted', 403)
    }
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
