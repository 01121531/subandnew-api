import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { adminDataAuthorizationKey } from '@/lib/admin-data-policy'
import { useAuthStore } from '@/stores/auth-store'

import { errorKey } from './lib/errors'

export function useMailboxQuery<T>(
  key: readonly unknown[],
  load: (signal: AbortSignal) => Promise<T>,
  enabled = true
) {
  const user = useAuthStore((state) => state.auth.user)
  return useQuery({
    queryKey: ['mailbox-admin', adminDataAuthorizationKey(user), ...key],
    queryFn: ({ signal }) => load(signal),
    enabled,
    retry: false,
    gcTime: 0,
  })
}
export function useMailboxMutation<V>(
  run: (value: V) => Promise<void>,
  success?: () => void
) {
  const client = useQueryClient()
  const { t } = useTranslation()
  const lock = useRef(false)
  // Mutation variables and results must not retain import bodies or passwords.
  const input = useRef<{ value: V } | undefined>(undefined)
  const mutation = useMutation({
    mutationFn: async () => {
      if (!input.current) return
      try {
        await run(input.current.value)
      } finally {
        input.current = undefined
        lock.current = false
      }
    },
    gcTime: 0,
    retry: false,
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['mailbox-admin'] })
      success?.()
    },
    onError: (error) =>
      toast.error(
        t(errorKey(error), {
          defaultValue: t('mailbox.errors.mailbox_request_failed'),
        })
      ),
  })
  return {
    ...mutation,
    submit: (value: V) => {
      if (lock.current) return
      lock.current = true
      input.current = { value }
      mutation.mutate()
    },
  }
}
