import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { errorKey, sessionExpired } from '../lib/errors'
import { clearSupplierSession } from '../session'

export function usePortalQuery<T>(
  key: readonly unknown[],
  fetcher: (signal: AbortSignal, refresh: boolean) => Promise<T>
) {
  const force = useRef(false)
  const query = useQuery({
    queryKey: ['supplier', ...key],
    queryFn: ({ signal }) => {
      const refresh = force.current
      force.current = false
      return fetcher(signal, refresh)
    },
  })
  return {
    ...query,
    refresh: () => {
      force.current = true
      void query.refetch()
    },
  }
}
export function useSupplierMutation<T, V = void>(
  mutationFn: (variables: V) => Promise<T>,
  onSuccess?: (data: T) => void
) {
  const client = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn,
    onSuccess: (data) => {
      void client.invalidateQueries({ queryKey: ['supplier'] })
      onSuccess?.(data)
    },
    onError: (error) => {
      if (sessionExpired(error)) {
        clearSupplierSession()
      }
      toast.error(t(errorKey(error)))
    },
  })
}
