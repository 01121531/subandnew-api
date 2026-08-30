/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { createFileRoute, redirect } from '@tanstack/react-router'

import { BillingAlerts, type BillingAlertTab } from '@/features/billing-alerts'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/billing-alerts/')({
  validateSearch: (search: Record<string, unknown>) => ({
    tab: isBillingAlertTab(search.tab) ? search.tab : ('rules' as const),
  }),
  beforeLoad: () => {
    const user = useAuthStore.getState().auth.user
    if (
      !hasPermission(
        user,
        ADMIN_PERMISSION_RESOURCES.BILLING_ALERT,
        ADMIN_PERMISSION_ACTIONS.VIEW
      )
    ) {
      throw redirect({ to: '/403' })
    }
  },
  component: BillingAlertsRoute,
})

const tabs = new Set<BillingAlertTab>([
  'rules',
  'metrics',
  'instance-alerts',
  'records',
  'templates',
  'exchange',
  'notifications',
])

function isBillingAlertTab(value: unknown): value is BillingAlertTab {
  return typeof value === 'string' && tabs.has(value as BillingAlertTab)
}

function BillingAlertsRoute() {
  const { tab } = Route.useSearch()
  const navigate = Route.useNavigate()
  return (
    <BillingAlerts
      activeTab={tab}
      onTabChange={(nextTab) => {
        void navigate({ search: { tab: nextTab }, replace: true })
      }}
    />
  )
}
