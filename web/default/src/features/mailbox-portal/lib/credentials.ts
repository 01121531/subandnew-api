import { invalidateAccountCredentials, mailboxApi } from '../api'
import type { Account, Credential } from '../types'
import { assertCurrentAssignment } from './guards'

export async function readCredential(
  csrf: string,
  account: Account,
  kind: 'password' | 'otp',
  signal: AbortSignal
): Promise<Credential> {
  try {
    assertCurrentAssignment(
      account,
      await mailboxApi.account(account.id, signal),
      'credentials'
    )
    const value = await mailboxApi.credentials(csrf, account.id, kind, signal)
    // Reject a late secret if ownership/review changed while it was being fetched.
    assertCurrentAssignment(
      account,
      await mailboxApi.account(account.id, signal),
      'credentials'
    )
    return value
  } catch (error) {
    invalidateAccountCredentials(account.id, error)
    throw error
  }
}
