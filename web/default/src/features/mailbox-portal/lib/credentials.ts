import {
  invalidateAccountCredentials,
  mailboxApi,
  MailboxRequestError,
} from '../api'
import type { Account, Credential, CredentialKind } from '../types'
import { assertCurrentAssignment, canReadCredentials } from './guards'

// Only concurrent checks share a result; post-read checks cannot reuse pre-read results.
const checks = new WeakMap<AbortSignal, Map<string, Promise<Account>>>()
function checkAccount(
  account: Account,
  signal: AbortSignal,
  phase: 'before' | 'after'
) {
  let pending = checks.get(signal)
  if (!pending) {
    pending = new Map()
    checks.set(signal, pending)
  }
  const key = `${credentialScope(account)}:${account.version}:${phase}`
  const existing = pending.get(key)
  if (existing) return existing
  const result = mailboxApi
    .account(account.id, signal, account.account_type ?? 'refund')
    .finally(() => {
      pending.delete(key)
    })
  pending.set(key, result)
  return result
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
      ((kind === 'card' || kind === 'cvv') && accountType !== 'opening')
    ) {
      throw new MailboxRequestError('mailbox_credentials_revoked', 403)
    }
    assertCurrentAssignment(
      account,
      await checkAccount(account, signal, 'before'),
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
      await checkAccount(account, signal, 'after'),
      'credentials'
    )
    return value
  } catch (error) {
    invalidateAccountCredentials(account.id, error)
    throw error
  }
}
