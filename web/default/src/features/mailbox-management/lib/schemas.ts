import { z } from 'zod'

import type { VersionedID } from '../types'

const password = z
  .string()
  .refine(
    (value) =>
      value === '' ||
      (new TextEncoder().encode(value).length >= 8 &&
        new TextEncoder().encode(value).length <= 72),
    'mailbox.admin.passwordLength'
  )
export const operatorSchema = z.object({
  username: z.string().trim().min(1).max(96),
  display_name: z.string().trim().min(1).max(128),
  password,
  enabled: z.boolean(),
  version: z.number().int().nonnegative(),
})
export const passwordSchema = z.object({ password })
export const reviewSchema = z
  .object({
    status: z.enum(['approved', 'rejected']),
    reason: z.string().max(2000),
  })
  .refine(
    (data) => data.status !== 'rejected' || data.reason.trim().length > 0,
    { path: ['reason'], message: 'mailbox.admin.reasonRequired' }
  )
export function parseVersionedIDs(value: string): VersionedID[] {
  const rows = value
    .trim()
    .split(/\r?\n/)
    .filter((row) => row.trim())
  if (rows.length === 0 || rows.length > 1000) {
    throw new Error('mailbox.admin.invalidIDs')
  }
  const seen = new Set<number>()
  return rows.map((row) => {
    const match = /^\s*(\d+)\s*[,\t:]\s*(\d+)\s*$/.exec(row)
    const id = Number(match?.[1])
    const version = Number(match?.[2])
    if (
      !Number.isSafeInteger(id) ||
      id <= 0 ||
      !Number.isSafeInteger(version) ||
      version <= 0 ||
      seen.has(id)
    ) {
      throw new Error('mailbox.admin.invalidIDs')
    }
    seen.add(id)
    return { id, version }
  })
}
export function otpRemaining(
  expiresAt: number,
  serverOffset: number,
  now = Date.now()
): number {
  return Math.max(0, Math.ceil((expiresAt * 1000 - now - serverOffset) / 1000))
}
