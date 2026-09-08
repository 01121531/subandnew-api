import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { errorKey } from '../lib/errors'

export function useAdminMutation<T, V>(
  mutationFn: (data: V) => Promise<T>,
  onSuccess: (data: T) => void
) {
  const client = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn,
    gcTime: 0,
    onSuccess: (data) => {
      void client.invalidateQueries({ queryKey: ['supplier-admin'] })
      onSuccess(data)
    },
    onError: (error) => toast.error(t(errorKey(error))),
  })
}
