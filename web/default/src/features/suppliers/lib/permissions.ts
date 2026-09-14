import { createContext, useContext } from 'react'

import type {
  DefaultPolicy,
  EffectivePolicy,
  PolicyOverrides,
  PortalSupplier,
} from '../types'

export function globalEffectivePolicy(policy: DefaultPolicy): EffectivePolicy {
  return {
    values: Object.fromEntries(
      Object.entries(policy.policy).map(([key, value]) => [key, value === true])
    ),
    sources: Object.fromEntries(
      Object.keys(policy.policy).map((key) => [key, 'global' as const])
    ),
    version: String(policy.revision),
  }
}

export const policyGroups = [
  {
    name: 'functions',
    keys: ['view_accounts', 'view_usage', 'manage_proxies', 'upload_accounts'],
  },
  {
    name: 'account',
    keys: [
      'account.email',
      'account.group_name',
      'account.status',
      'account.created_at',
      'account.total_cost',
      'account.today_cost',
      'account.total_requests',
      'account.total_tokens',
    ],
  },
  {
    name: 'summary',
    keys: [
      'summary.total_accounts',
      'summary.available_accounts',
      'summary.rpm',
    ],
  },
  {
    name: 'pool',
    keys: [
      'summary.pool_rpm',
      'summary.pool_concurrent',
      'summary.pool_available_accounts',
    ],
  },
  { name: 'usage', keys: ['usage.cost', 'usage.requests', 'usage.tokens'] },
] as const

export function applyOverrides(
  parent: EffectivePolicy,
  overrides: PolicyOverrides,
  source: 'supplier' | 'binding'
): EffectivePolicy {
  const result = {
    values: { ...parent.values },
    sources: { ...parent.sources },
    version: parent.version,
  }
  for (const [key, value] of Object.entries(overrides)) {
    if (value !== null) {
      result.values[key] = value
      result.sources[key] = source
    }
  }
  return result
}
export function bindingCapabilities<T extends PortalSupplier>(
  supplier: T,
  policy?: EffectivePolicy
): T {
  return {
    ...supplier,
    view_accounts: policy?.values.view_accounts ?? false,
    view_usage: policy?.values.view_usage ?? false,
    manage_proxies: policy?.values.manage_proxies ?? false,
    upload_accounts: policy?.values.upload_accounts ?? false,
  }
}

export const PolicyContext = createContext<EffectivePolicy | undefined>(
  undefined
)
export function useSupplierPolicy() {
  const policy = useContext(PolicyContext)
  return (key: string) => policy?.values[key] === true
}
