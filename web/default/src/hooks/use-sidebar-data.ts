/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import {
  BellRing,
  Bot,
  Braces,
  LayoutDashboard,
  Mail,
  FileClock,
  ScrollText,
  Server,
  ServerCog,
  Settings,
  Users,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { SidebarData } from '@/components/layout/types'
import { canAccessMailbox } from '@/features/mailbox-management/lib/permissions'
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
  const canViewDailyReports = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.DAILY_REPORT,
    ADMIN_PERMISSION_ACTIONS.VIEW
  )
  const canViewAccountDataAPIs = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_ACCOUNT_API,
    ADMIN_PERMISSION_ACTIONS.VIEW
  )
  const canAccessAssistant = [
    ADMIN_PERMISSION_ACTIONS.ACCESS,
    ADMIN_PERMISSION_ACTIONS.MANAGE,
    ADMIN_PERMISSION_ACTIONS.AUDIT,
  ].some((action) =>
    hasPermission(user, ADMIN_PERMISSION_RESOURCES.ASSISTANT, action)
  )

  return {
    navGroups: [
      {
        id: 'control-plane',
        title: t('Control plane'),
        items: [
          { title: t('Profile'), url: '/profile', icon: Users },
          ...(canAccessMailbox(user)
            ? [
                {
                  title: t('mailbox.admin.title'),
                  url: '/mailbox-management',
                  icon: Mail,
                },
              ]
            : []),
          ...(user?.role === ROLE.SUPER_ADMIN
            ? [{ title: t('Users'), url: '/users', icon: Users }]
            : []),
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
          ...(canViewDailyReports
            ? [
                {
                  title: '日报',
                  icon: FileClock,
                  items: [
                    {
                      title: '供货商数据',
                      url: '/daily-reports?tab=suppliers',
                    },
                    {
                      title: '每日账户数据',
                      url: '/daily-reports?tab=accounts',
                    },
                    {
                      title: '上传账户数量',
                      url: '/daily-reports?tab=uploads',
                    },
                  ],
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
          ...(canViewAccountDataAPIs
            ? [
                {
                  title: t('接口管理'),
                  url: '/interface-management',
                  icon: Braces,
                },
              ]
            : []),
          ...(canAccessAssistant
            ? [
                {
                  title: t('AI Assistant'),
                  url: '/assistant',
                  icon: Bot,
                },
              ]
            : []),
          ...(hasPermission(
            user,
            ADMIN_PERMISSION_RESOURCES.SUPPLIER,
            ADMIN_PERMISSION_ACTIONS.VIEW
          )
            ? [{ title: t('supplier.title'), url: '/suppliers', icon: Users }]
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
            requiredRole: ROLE.SUPER_ADMIN,
          },
        ],
      },
    ],
  }
}
