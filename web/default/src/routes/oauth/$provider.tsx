/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import i18next from 'i18next'
import { useEffect } from 'react'
import { toast } from 'sonner'

import { OAuthCallbackScreen } from '@/features/auth/components/oauth-callback-screen'
import { api, getSelf } from '@/lib/api'
import { type AuthUser, useAuthStore } from '@/stores/auth-store'

function OAuthCallback() {
  const navigate = useNavigate()
  const { provider } = Route.useParams()
  const search = Route.useSearch()

  useEffect(() => {
    void (async () => {
      if (!search.code) {
        toast.error(i18next.t('Missing code'))
        await navigate({ to: '/sign-in', replace: true })
        return
      }

      try {
        const response = await api.get(`/api/oauth/${provider}`, {
          params: { code: search.code, state: search.state },
          skipBusinessError: true,
        })
        if (!response.data?.success) {
          throw new Error(response.data?.message || 'OAuth failed')
        }

        const self = (await getSelf()) as {
          success?: boolean
          data?: AuthUser | null
        }
        if (!self.success || !self.data) {
          throw new Error('OAuth session not found')
        }

        useAuthStore.getState().auth.setUser(self.data)
        window.localStorage.setItem('uid', String(self.data.id))
        toast.success(i18next.t('Signed in successfully!'))
        await navigate({ to: '/instances', replace: true })
      } catch (error) {
        toast.error(
          error instanceof Error ? error.message : i18next.t('OAuth failed')
        )
        await navigate({ to: '/sign-in', replace: true })
      }
    })()
  }, [navigate, provider, search.code, search.state])

  return <OAuthCallbackScreen provider={provider} mode='login' />
}

export const Route = createFileRoute('/oauth/$provider')({
  validateSearch: (search: Record<string, unknown>) => ({
    code: typeof search.code === 'string' ? search.code : undefined,
    state: typeof search.state === 'string' ? search.state : undefined,
  }),
  component: OAuthCallback,
})
