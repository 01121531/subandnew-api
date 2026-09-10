import { useEffect, useImperativeHandle, useState, type Ref } from 'react'
import { useTranslation } from 'react-i18next'

import { Confirm } from '../components/common'

export type FormLeaveGuard = { requestLeave: (next: () => void) => void }

export function useFormLeaveGuard(props: {
  dirty: boolean
  pending: boolean
  guardRef?: Ref<FormLeaveGuard>
  onPendingChange?: (pending: boolean) => void
}) {
  const { t } = useTranslation()
  const [leave, setLeave] = useState<(() => void) | null>(null)
  const requestLeave = (next: () => void) => {
    if (props.pending) return
    if (props.dirty) setLeave(() => next)
    else next()
  }
  useImperativeHandle(props.guardRef, () => ({ requestLeave }))
  const { onPendingChange, pending } = props
  useEffect(() => {
    onPendingChange?.(pending)
    return () => onPendingChange?.(false)
  }, [pending, onPendingChange])
  return {
    requestLeave,
    discardConfirmation: (
      <Confirm
        open={!!leave}
        title={t('supplier.ui_discardTitle')}
        description={t('supplier.ui_discardDescription')}
        pending={props.pending}
        destructive={false}
        onClose={() => setLeave(null)}
        onConfirm={() => {
          if (props.pending) return
          setLeave(null)
          leave?.()
        }}
      />
    ),
  }
}
