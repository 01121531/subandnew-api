import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { takeOwnershipOfAccountDataAPI } from './api'
import { canTakeOwnership, ownershipText } from './ownership'
import type { AccountDataAPI } from './types'

export function AccountDataOwnership(props: {
  item: AccountDataAPI
  disabled: boolean
  onTaken: (item: AccountDataAPI) => void
}) {
  const { i18n } = useTranslation()
  const t = ownershipText(i18n)
  const user = useAuthStore((state) => state.auth.user)
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const mutation = useMutation({
    mutationFn: async () => {
      if (
        !canTakeOwnership(
          useAuthStore.getState().auth.user,
          props.item.created_by
        )
      ) {
        throw new Error('root_required')
      }
      return takeOwnershipOfAccountDataAPI(props.item.id)
    },
    retry: false,
    onSuccess: async (item) => {
      setOpen(false)
      props.onTaken(item)
      toast.success(t('success'))
      await client.invalidateQueries({ queryKey: ['account-data-apis'] })
    },
    onError: () => toast.error(t('failed')),
  })
  if (user?.role !== ROLE.SUPER_ADMIN) return null
  const allowed = canTakeOwnership(user, props.item.created_by)
  return (
    <div className='flex min-w-0 flex-wrap items-center justify-between gap-2 pt-2'>
      <span className='text-muted-foreground min-w-0 text-xs break-words'>
        {props.item.created_by > 0
          ? t('owner', { id: props.item.created_by })
          : t('legacy')}
      </span>
      {allowed && (
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={props.disabled || mutation.isPending}
          onClick={() => setOpen(true)}
        >
          <ShieldCheck />
          {t('action')}
        </Button>
      )}
      <ConfirmDialog
        open={open && allowed}
        onOpenChange={(value) => {
          if (!mutation.isPending) setOpen(value)
        }}
        title={t('title', { name: props.item.name })}
        desc={t('confirm')}
        confirmText={t('action')}
        isLoading={mutation.isPending}
        disabled={props.disabled || !allowed}
        handleConfirm={() => {
          if (allowed && !props.disabled && !mutation.isPending) {
            mutation.mutate()
          }
        }}
      />
    </div>
  )
}
