import { isAxiosError } from 'axios'

import { ROLE } from '@/lib/roles'

type Identity = { id: number; role: number }

export function canManageUser(
  actor: Identity | null | undefined,
  target: Identity
): boolean {
  if (actor?.role !== ROLE.SUPER_ADMIN) return false
  if (target.role < ROLE.ADMIN) return true
  return (
    actor.role === ROLE.SUPER_ADMIN &&
    target.role === ROLE.ADMIN &&
    actor.id !== target.id
  )
}

export function isAdminPolicyConflict(error: unknown): boolean {
  if (isAxiosError(error)) {
    if (error.response?.status === 409) return true
    return isAdminPolicyConflict(error.response?.data)
  }
  if (!error || typeof error !== 'object') return false
  const code = 'code' in error ? error.code : undefined
  const message = 'message' in error ? error.message : undefined
  return (
    (typeof code === 'string' && /conflict|revision/i.test(code)) ||
    (typeof message === 'string' &&
      /conflict|revision|版本冲突|版本已|版本不匹配/i.test(message))
  )
}
