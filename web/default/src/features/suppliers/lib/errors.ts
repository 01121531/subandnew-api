import { SupplierRequestError } from '../portal-api'

export function errorKey(error: unknown): string {
  if (error instanceof SupplierRequestError) {
    if (error.code === 'supplier_upstream_import_result_unconfirmed') {
      return 'supplier.importStatus_unknown'
    }
    if (error.code === 'supplier_invalid_session_keys') {
      return 'supplier.sessionKeysInvalid'
    }
    if (error.code === 'supplier_upstream_resource_ownership_conflict') {
      return 'supplier.proxyOwnershipConflict'
    }
    if (error.code === 'supplier_upstream_resource_ownership_unknown') {
      return 'supplier.proxyOwnershipUnknown'
    }
    if (error.code === 'supplier_upstream_resource_not_owned') {
      return 'supplier.proxyNotOwned'
    }
    if (error.code === 'supplier_binding_conflict') {
      return 'supplier.bindingConflict'
    }
    if (error.code === 'supplier_binding_limit') return 'supplier.bindingLimit'
    if (error.code === 'supplier_username_conflict') {
      return 'supplier.usernameConflict'
    }
    if (error.code === 'supplier_encryption_unavailable') {
      return 'supplier.encryptionUnavailable'
    }
    if (error.code === 'supplier_current_password_incorrect') {
      return 'supplier.currentPasswordIncorrect'
    }
    if (
      error.code === 'supplier_upstream_authentication_failed' ||
      error.code === 'supplier_upstream_supplier_identity_denied'
    ) {
      return 'supplier.upstreamAuthFailed'
    }
    if (
      error.code === 'supplier_upstream_upstream_rate_limited' ||
      error.status === 429
    ) {
      return 'supplier.rateLimited'
    }
    if (error.code === 'supplier_upstream_resource_not_authorized') {
      return 'supplier.forbidden'
    }
    if (
      error.code === 'INVALID_CREDENTIALS' ||
      error.code === 'supplier_invalid_credentials'
    ) {
      return 'supplier.invalidLogin'
    }
    if (error.status === 401) return 'supplier.unauthorized'
    if (error.status === 403) return 'supplier.forbidden'
  }
  return 'supplier.requestFailed'
}

export function sessionExpired(error: unknown): boolean {
  return (
    error instanceof SupplierRequestError &&
    error.status === 401 &&
    error.code !== 'supplier_current_password_incorrect' &&
    error.code !== 'supplier_invalid_credentials'
  )
}
