/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useTranslation } from 'react-i18next'

import { useStatus } from '@/hooks/use-status'
import { useAuthStore } from '@/stores/auth-store'

export type TopNavLink = {
  title: string
  href: string
  disabled?: boolean
  external?: boolean
}

export function useTopNavLinks(): TopNavLink[] {
  const { t } = useTranslation()
  const { status } = useStatus()
  const authenticated = useAuthStore((state) => Boolean(state.auth.user))
  const links: TopNavLink[] = []

  if (authenticated) {
    links.push({ title: t('Instance center'), href: '/instances' })
  }
  if (status?.docs_link) {
    links.push({
      title: t('Docs'),
      href: String(status.docs_link),
      external: true,
    })
  }
  return links
}
