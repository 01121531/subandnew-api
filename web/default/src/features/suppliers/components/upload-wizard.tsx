import { useQueryClient } from '@tanstack/react-query'
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
import { errorKey, sessionExpired } from '../lib/errors'
import { safeOAuthUrl, timestampMs } from '../lib/schemas'
import { portalApi, SupplierRequestError } from '../portal-api'
import { clearSupplierSession } from '../session'
import type {
  AccountImportCredentials,
  AccountImportResult,
  OAuthFlow,
  UploadInput,
  UploadMethod,
} from '../types'
import { Confirm, Freshness, QueryState } from './common'
import { UploadAuthorization } from './upload-authorization'
import { UploadConfig } from './upload-config'
import { ImportResultView } from './upload-import-result'

export function UploadWizard(props: {
  supplierId: number
  bindingId: number
  bindingName: string
  csrf: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [method, setMethod] = useState<UploadMethod>('login')
  const [draft, setDraft] = useState<UploadInput>()
  const [importResult, setImportResult] = useState<AccountImportResult | null>(
    null
  )
  const [importError, setImportError] = useState<string | null>(null)
  const [importing, setImporting] = useState(false)
  const importLock = useRef(false)
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
  const remember = (data: UploadInput) => {
    try {
      useSupplierUploadPreferences
        .getState()
        .remember(props.supplierId, props.bindingId, {
          policyId: data.policy_template_id,
          templateId: data.cc_template_id,
        })
    } catch {
      // Storage restrictions must not invalidate a successful remote import.
    }
  }
  const importAccounts = async (
    data: UploadInput,
    credentials: AccountImportCredentials
  ) => {
    if (importLock.current || (method !== 'rt' && method !== 'sk')) return
    importLock.current = true
    setImporting(true)
    setImportError(null)
    try {
      // Do not put secret-bearing arguments into the React Query mutation cache.
      const result = await portalApi.importAccounts(props.csrf, method, {
        ...data,
        ...credentials,
      })
      setImportResult(result)
      setCompleted(true)
      setDirty(false)
      if (result.ok > 0) {
        remember(data)
        void client.invalidateQueries({ queryKey: ['supplier'] })
      }
    } catch (error) {
      if (sessionExpired(error)) clearSupplierSession()
      setImportError(errorKey(error))
    } finally {
      importLock.current = false
      setImporting(false)
    }
  }
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
      if (submitted.current) remember(submitted.current)
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
  const pending = authorize.isPending || exchange.isPending || importing
  const close = () => {
    if (pending) return
    if (!completed && (dirty || flow || callback)) setConfirmClose(true)
    else props.onClose()
  }
  const restart = () => {
    if (completed) setDraft(undefined)
    setImportResult(null)
    setImportError(null)
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
  if (importing) step = 2
  if (completed) step = 3
  let submitLabel = 'supplier.generateAuthorization'
  if (method === 'rt' || method === 'sk') {
    submitLabel = 'supplier.importAccounts'
  }
  if (importing) submitLabel = 'supplier.importing'
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
          {(method === 'rt' || method === 'sk'
            ? ['configure', 'importAccounts', 'accountImportResult']
            : ['configure', 'authorize', 'exchange']
          ).map((label, index) => (
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
          {completed && !importResult && (
            <div className='grid justify-items-center gap-4 py-12'>
              <CheckCircle2 className='size-12 text-emerald-600' />
              <h2 className='text-lg font-semibold'>
                {t('supplier.uploadComplete')}
              </h2>
            </div>
          )}
          {completed && importResult && (
            <ImportResultView result={importResult} />
          )}
          {importError && (
            <div
              role='alert'
              className='text-destructive mb-4 grid gap-1 text-sm'
            >
              <p>{t(importError)}</p>
              <p>{t('supplier.importCheckBeforeRetry')}</p>
            </div>
          )}
          {!completed && !flow && (
            <QueryState
              pending={options.isPending}
              error={options.error}
              hasData={!!options.data}
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
                  method={method}
                  initial={draft}
                  onMethodChange={(value) => {
                    setMethod(value)
                    setFlow(null)
                    setCallback('')
                    setImportError(null)
                    submitted.current = null
                  }}
                  bindingId={props.bindingId}
                  options={options.data}
                  pending={pending}
                  onDirtyChange={setDirty}
                  onSubmit={(data, credentials) => {
                    if (pending) return
                    setDraft(data)
                    if (method === 'rt' || method === 'sk') {
                      void importAccounts(data, credentials)
                    } else {
                      authorize.mutate({ ...data, oauth_flow: method })
                    }
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
              {t(submitLabel)}
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
