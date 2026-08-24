/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import {
  BellRing,
  LayoutDashboard,
  FileClock,
  ScrollText,
  Server,
  ServerCog,
  Settings,
  Users,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { SidebarData } from '@/components/layout/types'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export function useSidebarData(): SidebarData {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canViewManagedInstances = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
    ADMIN_PERMISSION_ACTIONS.VIEW
  )
  const canViewUsageRecords = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
    ADMIN_PERMISSION_ACTIONS.USAGE_VIEW
  )
  const canViewBillingAlerts = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.BILLING_ALERT,
    ADMIN_PERMISSION_ACTIONS.VIEW
  )

  return {
    navGroups: [
      {
        id: 'control-plane',
        title: t('Control plane'),
        items: [
          ...(canViewManagedInstances
            ? [
                {
                  title: t('Fleet overview'),
                  url: '/dashboard',
                  icon: LayoutDashboard,
                },
                {
                  title: t('Instance center'),
                  url: '/instances',
                  icon: Server,
                },
                {
                  title: t('Account management'),
                  url: '/account-management',
                  icon: Users,
                },
              ]
            : []),
          ...(canViewManagedInstances && canViewUsageRecords
            ? [
                {
                  title: t('Usage records'),
                  url: '/usage-records',
                  icon: ScrollText,
                },
                {
                  title: '导出记录',
                  url: '/export-records',
                  icon: FileClock,
                },
              ]
            : []),
          ...(canViewBillingAlerts
            ? [
                {
                  title: '预警任务',
                  url: '/billing-alerts',
                  activeUrls: ['/billing-alerts', '/billing-alert-records'],
                  icon: BellRing,
                },
              ]
            : []),
          {
            title: t('System Info'),
            url: '/system-info',
            icon: ServerCog,
            requiredRole: ROLE.SUPER_ADMIN,
          },
          {
            title: t('System Settings'),
            url: '/system-settings/site',
            activeUrls: ['/system-settings'],
            icon: Settings,
          },
        ],
      },
    ],
  }
}
