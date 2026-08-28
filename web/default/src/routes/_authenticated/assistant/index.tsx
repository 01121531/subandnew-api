/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { createFileRoute, redirect } from '@tanstack/react-router'

import { AssistantManagement } from '@/features/assistant'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/assistant/')({
  beforeLoad: () => {
    const user = useAuthStore.getState().auth.user
    const canOpenAssistant = [
      ADMIN_PERMISSION_ACTIONS.ACCESS,
      ADMIN_PERMISSION_ACTIONS.MANAGE,
      ADMIN_PERMISSION_ACTIONS.AUDIT,
    ].some((action) =>
      hasPermission(user, ADMIN_PERMISSION_RESOURCES.ASSISTANT, action)
    )
    if (!canOpenAssistant) {
      throw redirect({ to: '/403' })
    }
  },
  component: AssistantManagement,
})
