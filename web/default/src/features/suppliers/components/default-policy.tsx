import { useQuery } from '@tanstack/react-query'
import { Save } from 'lucide-react'
import { useRef, useState, type Ref } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'

import { adminApi } from '../admin-api'
import { useAdminMutation } from '../hooks/use-admin-mutation'
import {
  useFormLeaveGuard,
  type FormLeaveGuard,
} from '../hooks/use-form-leave-guard'
import type { DefaultPolicy as Policy, PolicyOverrides } from '../types'
import { Confirm, QueryState } from './common'
import { PolicyEditor } from './policy-editor'

export function DefaultPolicyDialog(props: {
  canManage: boolean
  onClose: () => void
}) {
  const query = useQuery({
    queryKey: ['supplier-admin', 'defaults'],
    queryFn: adminApi.defaults,
  })
  const { t } = useTranslation()
  const guardRef = useRef<FormLeaveGuard>(null)
  const close = () => {
    if (guardRef.current) guardRef.current.requestLeave(props.onClose)
    else props.onClose()
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close()
      }}
    >
      <DialogContent
        className='supplier-portal flex max-h-[90dvh] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl'
        aria-describedby={undefined}
        showCloseButton={false}
      >
        <DialogTitle className='shrink-0 border-b p-4'>
          {t('supplier.defaultPolicy')}
        </DialogTitle>
        <QueryState
          pending={query.isPending}
          error={query.error}
          retry={() => void query.refetch()}
        >
          {query.data && (
            <DefaultPolicyForm
              initial={query.data}
              guardRef={guardRef}
              {...props}
            />
          )}
        </QueryState>
        {!query.data && (
          <Button className='m-4' onClick={props.onClose}>
            {t('supplier.close')}
          </Button>
        )}
      </DialogContent>
    </Dialog>
  )
}
function DefaultPolicyForm(props: {
  initial: Policy
  canManage: boolean
  onClose: () => void
  guardRef: Ref<FormLeaveGuard>
}) {
  const { t } = useTranslation()
  const [initial] = useState(props.initial)
  const [value, setValue] = useState<PolicyOverrides>(initial.policy)
  const [confirm, setConfirm] = useState(false)
  const mutation = useAdminMutation(
    (data: Policy) => adminApi.saveDefaults(data),
    () => {
      toast.success(t('supplier.saved'))
      props.onClose()
    }
  )
  const guard = useFormLeaveGuard({
    dirty: JSON.stringify(value) !== JSON.stringify(initial.policy),
    pending: mutation.isPending || confirm,
    guardRef: props.guardRef,
  })
  return (
    <form
      className='flex min-h-0 flex-1 flex-col'
      onSubmit={(e) => {
        e.preventDefault()
        if (props.canManage && !mutation.isPending) setConfirm(true)
      }}
    >
      <div className='min-h-0 flex-1 overflow-y-auto p-4'>
        <PolicyEditor
          value={value}
          parent={{
            values: {},
            sources: {},
            version: String(initial.revision),
          }}
          level='global'
          disabled={!props.canManage || mutation.isPending || confirm}
          onChange={setValue}
        />
      </div>
      <footer className='flex shrink-0 justify-end gap-2 border-t p-4'>
        <Button
          type='button'
          variant='outline'
          disabled={mutation.isPending || confirm}
          onClick={() => guard.requestLeave(props.onClose)}
        >
          {t('supplier.close')}
        </Button>
        {props.canManage && (
          <Button type='submit' disabled={mutation.isPending || confirm}>
            <Save />
            {t('supplier.save')}
          </Button>
        )}
      </footer>
      {guard.discardConfirmation}
      <Confirm
        open={confirm}
        title={t('supplier.permissions')}
        description={t('supplier.policySaveConfirm')}
        pending={mutation.isPending}
        onClose={() => setConfirm(false)}
        onConfirm={() =>
          mutation.mutate(
            { policy: value, revision: initial.revision },
            { onError: () => setConfirm(false) }
          )
        }
      />
    </form>
  )
}
