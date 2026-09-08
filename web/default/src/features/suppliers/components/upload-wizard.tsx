import { CheckCircle2, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog'
import { useSupplierUploadPreferences } from '@/stores/supplier-upload-preferences'

import { usePortalQuery, useSupplierMutation } from '../hooks/use-portal-query'
import { safeOAuthUrl, timestampMs } from '../lib/schemas'
import { portalApi, SupplierRequestError } from '../portal-api'
import type { OAuthFlow, UploadInput } from '../types'
import { Confirm, Freshness, QueryState } from './common'
import { UploadAuthorization } from './upload-authorization'
import { UploadConfig } from './upload-config'

export function UploadWizard(props: {
  supplierId: number
  bindingId: number
  bindingName: string
  csrf: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const options = usePortalQuery(
    ['upload-options', props.bindingId],
    (signal, refresh) => portalApi.options(props.bindingId, signal, refresh)
  )
  const [flow, setFlow] = useState<OAuthFlow | null>(null)
  const [callback, setCallback] = useState('')
  const [completed, setCompleted] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [confirmClose, setConfirmClose] = useState(false)
  const [now, setNow] = useState(Date.now())
  const submitted = useRef<UploadInput | null>(null)
  useEffect(() => {
    if (!flow) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [flow])
  const authorize = useSupplierMutation(
    async (data: UploadInput) => {
      submitted.current = data
      return portalApi.authUrl(props.csrf, data)
    },
    (data) => {
      setNow(Date.now())
      setFlow(data)
    }
  )
  const exchange = useSupplierMutation(
    async () => {
      if (!flow || timestampMs(flow.expires_at) <= Date.now()) {
        throw new SupplierRequestError('FLOW_EXPIRED', 400)
      }
      return portalApi.exchange(props.csrf, flow.flow_id, callback.trim())
    },
    () => {
      if (submitted.current) {
        try {
          useSupplierUploadPreferences
            .getState()
            .remember(props.supplierId, props.bindingId, {
              policyId: submitted.current.policy_template_id,
              templateId: submitted.current.cc_template_id,
            })
        } catch {
          // Browser storage restrictions must not turn a successful import into a failure.
        }
      }
      setCompleted(true)
      setFlow(null)
      setCallback('')
      setDirty(false)
      submitted.current = null
    }
  )
  const url = flow ? safeOAuthUrl(flow.url) : null
  const expired =
    !!flow &&
    (!Number.isFinite(timestampMs(flow.expires_at)) ||
      timestampMs(flow.expires_at) <= now)
  const pending = authorize.isPending || exchange.isPending
  const close = () => {
    if (pending) return
    if (!completed && (dirty || flow || callback)) setConfirmClose(true)
    else props.onClose()
  }
  const restart = () => {
    setFlow(null)
    setCallback('')
    setCompleted(false)
    setDirty(false)
    submitted.current = null
    authorize.reset()
    exchange.reset()
  }
  let step = 1
  if (flow) step = 2
  if (completed) step = 3
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close()
      }}
    >
      <DialogContent
        showCloseButton={false}
        className='supplier-portal flex h-dvh max-h-dvh w-screen max-w-none flex-col gap-0 overflow-hidden rounded-none p-0 sm:h-auto sm:max-h-[90dvh] sm:w-[calc(100%-3rem)] sm:max-w-[920px] sm:rounded-lg'
      >
        <header className='flex shrink-0 items-start justify-between gap-4 border-b px-5 py-4 sm:px-6'>
          <div className='grid min-w-0 gap-1.5'>
            <DialogTitle className='text-lg font-semibold'>
              {t('supplier.uploadAccounts')}
            </DialogTitle>
            <DialogDescription className='break-words'>
              {props.bindingName}
            </DialogDescription>
          </div>
          <Button
            variant='ghost'
            size='icon'
            disabled={pending}
            onClick={close}
            aria-label={t('supplier.close')}
          >
            <X />
          </Button>
        </header>
        <ol className='bg-muted/30 grid shrink-0 grid-cols-3 gap-2 border-b px-5 py-3 text-xs sm:px-6 sm:text-sm'>
          {['configure', 'authorize', 'exchange'].map((label, index) => (
            <li
              key={label}
              aria-current={step === index + 1 ? 'step' : undefined}
              className={
                step === index + 1
                  ? 'text-primary font-semibold'
                  : 'text-muted-foreground'
              }
            >
              {index + 1}. {t(`supplier.${label}`)}
            </li>
          ))}
        </ol>
        <div className='min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain p-5 sm:p-6'>
          {completed && (
            <div className='grid justify-items-center gap-4 py-12'>
              <CheckCircle2 className='size-12 text-emerald-600' />
              <h2 className='text-lg font-semibold'>
                {t('supplier.uploadComplete')}
              </h2>
            </div>
          )}
          {!completed && !flow && (
            <QueryState
              pending={options.isPending}
              error={options.error}
              retry={options.refresh}
            >
              <div className='mb-4 flex justify-end'>
                <Freshness
                  data={options.data}
                  pending={options.isFetching || pending}
                  refresh={options.refresh}
                />
              </div>
              {options.data && (
                <UploadConfig
                  supplierId={props.supplierId}
                  bindingId={props.bindingId}
                  options={options.data}
                  pending={pending}
                  onDirtyChange={setDirty}
                  onSubmit={(data) => {
                    if (!pending) authorize.mutate(data)
                  }}
                />
              )}
            </QueryState>
          )}
          {flow && (
            <UploadAuthorization
              flow={flow}
              url={url}
              expired={expired}
              pending={pending}
              callback={callback}
              onCallback={setCallback}
              onSubmit={() => {
                if (!pending && !expired && url && callback.trim()) {
                  exchange.mutate()
                }
              }}
            />
          )}
        </div>
        <footer className='bg-muted/20 flex shrink-0 flex-wrap items-center justify-end gap-2 border-t px-5 py-4 sm:px-6'>
          {!completed && (
            <Button variant='outline' disabled={pending} onClick={close}>
              {t('supplier.cancel')}
            </Button>
          )}
          {!completed && !flow && (
            <Button
              type='submit'
              form='supplier-upload-config'
              disabled={pending || !options.data || !!options.error}
            >
              {t('supplier.generateAuthorization')}
            </Button>
          )}
          {flow && (
            <>
              <Button variant='outline' disabled={pending} onClick={restart}>
                {t('supplier.restart')}
              </Button>
              <Button
                type='submit'
                form='supplier-upload-exchange'
                disabled={pending || expired || !url || !callback.trim()}
              >
                {t('supplier.exchange')}
              </Button>
            </>
          )}
          {completed && (
            <>
              <Button variant='outline' onClick={restart}>
                {t('supplier.continueUpload')}
              </Button>
              <Button onClick={props.onClose}>{t('supplier.done')}</Button>
            </>
          )}
        </footer>
        <Confirm
          open={confirmClose}
          title={t('supplier.discardUpload')}
          description={t('supplier.discardUploadConfirm')}
          pending={pending}
          onClose={() => setConfirmClose(false)}
          onConfirm={props.onClose}
        />
      </DialogContent>
    </Dialog>
  )
}
