import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { adminApi } from '../admin-api'
import {
  canTakeSupplierOwnership,
  supplierOwnershipText,
} from '../lib/ownership'
import type { Supplier } from '../types'

export function SupplierOwnership(props: {
  supplier: Supplier
  disabled: boolean
  onTaken: (supplier: Supplier) => void
}) {
  const { i18n } = useTranslation()
  const t = supplierOwnershipText(i18n)
  const user = useAuthStore((state) => state.auth.user)
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const mutation = useMutation({
    mutationFn: async () => {
      const actor = useAuthStore.getState().auth.user
      if (
        !actor ||
        !canTakeSupplierOwnership(actor, props.supplier.responsible_admin_id)
      ) {
        throw new Error('root_required')
      }
      const result = await adminApi.takeOwnership(props.supplier.id)
      if (!result.completed) throw new Error('supplier_takeover_failed')
      return actor.id
    },
    retry: false,
    onSuccess: async (actorId) => {
      setOpen(false)
      props.onTaken({ ...props.supplier, responsible_admin_id: actorId })
      toast.success(t('success'))
      await client.invalidateQueries({ queryKey: ['supplier-admin'] })
      try {
        props.onTaken(await adminApi.get(props.supplier.id))
      } catch {
        toast.error(t('refreshFailed'))
      }
    },
    onError: () => toast.error(t('failed')),
  })
  if (user?.role !== ROLE.SUPER_ADMIN) return null
  const allowed = canTakeSupplierOwnership(
    user,
    props.supplier.responsible_admin_id
  )
  return (
    <div className='flex min-w-0 shrink-0 flex-wrap items-center justify-between gap-2 border-b px-4 py-2'>
      <span className='text-muted-foreground min-w-0 text-xs break-words'>
        {props.supplier.responsible_admin_id
          ? t('owner', { id: props.supplier.responsible_admin_id })
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
        title={t('title', { name: props.supplier.name })}
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
