export function proxyEnabled(status: string | null): boolean {
  return status === 'active' || status === 'enabled'
}

export function formatNumber(
  value: number | null | undefined,
  digits?: number
): string {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return '--'
  }
  return digits === undefined ? value.toLocaleString() : value.toFixed(digits)
}

const auditActions: Record<string, string> = {
  'GET /api/suppliers/default-policy': 'defaultPolicy',
  'GET /api/suppliers/portal-settings': 'portalSettings',
  'PUT /api/suppliers/portal-settings': 'portalSettings',
  'PUT /api/suppliers/default-policy': 'defaultPolicy',
  'GET /api/suppliers': 'title',
  'POST /api/suppliers': 'create',
  'GET /api/suppliers/instances': 'instance',
  'GET /api/suppliers/:id': 'supplier',
  'PUT /api/suppliers/:id': 'edit',
  'DELETE /api/suppliers/:id': 'delete',
  'POST /api/suppliers/:id/password': 'resetPassword',
  'POST /api/suppliers/:id/revoke-sessions': 'revoke',
  'GET /api/suppliers/:id/bindings': 'bindings',
  'POST /api/suppliers/:id/bindings': 'newBinding',
  'PUT /api/suppliers/:id/bindings/:binding_id': 'editBinding',
  'DELETE /api/suppliers/:id/bindings/:binding_id': 'deleteBinding',
  'POST /api/suppliers/:id/bindings/:binding_id/test': 'test',
  'GET /api/suppliers/:id/audits': 'audits',
  'POST /supplier-api/v1/auth/login': 'signIn',
  'GET /supplier-api/v1/auth/session': 'checkSession',
  'POST /supplier-api/v1/auth/logout': 'logout',
  'POST /supplier-api/v1/auth/password': 'changePassword',
  'GET /supplier-api/v1/bindings': 'bindings',
  'GET /supplier-api/v1/accounts': 'readAccounts',
  'GET /supplier-api/v1/account-summary': 'totalAccounts',
  'GET /supplier-api/v1/usage': 'readUsage',
  'GET /supplier-api/v1/proxies': 'proxies',
  'POST /supplier-api/v1/proxies': 'import',
  'POST /supplier-api/v1/proxies/:id/test': 'test',
  'PATCH /supplier-api/v1/proxies/:id': 'proxyStatus',
  'DELETE /supplier-api/v1/proxies/:id': 'deleteProxy',
  'GET /supplier-api/v1/account-upload/options': 'configure',
  'POST /supplier-api/v1/account-upload/auth-url': 'authorize',
  'POST /supplier-api/v1/account-upload/exchange': 'exchange',
  'POST /supplier-api/v1/account-upload/import-rt': 'method_rt',
  'POST /supplier-api/v1/account-upload/import-sk': 'method_sk',
}

export function auditLabelKey(action: string | undefined): string {
  return `supplier.${auditActions[action?.trim().replaceAll(/\s+/g, ' ') ?? ''] ?? 'auditAction'}`
}
