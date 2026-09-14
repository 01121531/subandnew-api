import { SupplierRequestError } from '../portal-api'

export function errorKey(error: unknown): string {
  if (error instanceof SupplierRequestError) {
    if (uploadSettingsChanged(error)) return 'supplier.uploadSettingsChanged'
    if (error.code === 'supplier_invalid_portal_text') {
      return 'supplier.portalTextInvalid'
    }
    if (error.code === 'supplier_invalid_upload_methods') {
      return 'supplier.portalMethodsInvalid'
    }
    if (error.code === 'supplier_naming_changed') {
      return 'supplier.namingChanged'
    }
    if (error.code === 'supplier_invalid_naming_rule') {
      return 'supplier.namingInvalidRule'
    }
    if (error.code === 'supplier_invalid_account_name') {
      return 'supplier.namingInvalidName'
    }
    if (error.code === 'supplier_account_name_too_long') {
      return 'supplier.namingNameTooLong'
    }
    if (error.code === 'supplier_sk_suffix_not_supported') {
      return 'supplier.namingSkUnavailable'
    }
    if (error.code === 'supplier_policy_changed') {
      return 'supplier.policyChanged'
    }
    if (error.code === 'supplier_invalid_policy') {
      return 'supplier.invalidPolicy'
    }
    if (error.code === 'supplier_field_forbidden') {
      return 'supplier.fieldForbidden'
    }
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

export function uploadSettingsChanged(error: unknown): boolean {
  return (
    error instanceof SupplierRequestError &&
    [
      'supplier_portal_settings_changed',
      'supplier_upload_method_disabled',
      'supplier_oauth_flow_expired_or_used',
    ].includes(error.code)
  )
}

export function sessionExpired(error: unknown): boolean {
  return (
    error instanceof SupplierRequestError &&
    error.status === 401 &&
    error.code !== 'supplier_current_password_incorrect' &&
    error.code !== 'supplier_invalid_credentials'
  )
}

export function canRetainQueryData(error: unknown): boolean {
  if (error instanceof SupplierRequestError) {
    return error.status === 0 || error.status === 429 || error.status >= 500
  }
  return error instanceof TypeError
}
