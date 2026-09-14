import { BarChart3, KeyRound, Network, Users } from 'lucide-react'

import type { PortalSupplier } from '../types'

export function portalViews(supplier: PortalSupplier) {
  return [
    {
      id: 'accounts',
      allowed: supplier.view_accounts || supplier.upload_accounts,
      icon: Users,
    },
    { id: 'usage', allowed: supplier.view_usage, icon: BarChart3 },
    { id: 'proxies', allowed: supplier.manage_proxies, icon: Network },
    { id: 'security', allowed: true, icon: KeyRound },
  ].filter((item) => item.allowed)
}
