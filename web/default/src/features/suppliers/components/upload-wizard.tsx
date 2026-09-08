import { CheckCircle2, ExternalLink } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

import { usePortalQuery, useSupplierMutation } from '../hooks/use-portal-query'
import { safeOAuthUrl, timestampMs } from '../lib/schemas'
import { portalApi, SupplierRequestError } from '../portal-api'
import type { OAuthFlow, UploadInput } from '../types'
import { Field, Freshness, QueryState, Time } from './common'
import { UploadConfig } from './upload-config'

export function UploadWizard(props: { bindingId: number; csrf: string }) {
  const { t } = useTranslation()
  const options = usePortalQuery(
    ['upload-options', props.bindingId],
    (signal, refresh) => portalApi.options(props.bindingId, signal, refresh)
  )
  const [flow, setFlow] = useState<OAuthFlow | null>(null)
  const [callback, setCallback] = useState('')
  const [completed, setCompleted] = useState(false)
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    if (!flow) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [flow])
  const authorize = useSupplierMutation(
    (data: UploadInput) => portalApi.authUrl(props.csrf, data),
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
      setCompleted(true)
      setFlow(null)
      setCallback('')
    }
  )
  const url = flow ? safeOAuthUrl(flow.url) : null
  const expired = flow
    ? !Number.isFinite(timestampMs(flow.expires_at)) ||
      timestampMs(flow.expires_at) <= now
    : false
  const restart = () => {
    setFlow(null)
    setCallback('')
    setCompleted(false)
    authorize.reset()
    exchange.reset()
  }
  let step = 1
  if (flow) step = 2
  if (completed) step = 3
  return (
    <div className='grid gap-6'>
      <ol className='grid gap-3 border-b pb-4 text-sm sm:grid-cols-3'>
        {['configure', 'authorize', 'exchange'].map((label, index) => (
          <li
            key={label}
            aria-current={step === index + 1 ? 'step' : undefined}
            className={
              step === index + 1
                ? 'font-semibold text-emerald-700 dark:text-emerald-400'
                : 'text-muted-foreground'
            }
          >
            {index + 1}. {t(`supplier.${label}`)}
          </li>
        ))}
      </ol>
      {completed && (
        <div className='grid justify-items-start gap-4 py-8'>
          <CheckCircle2 className='size-10 text-emerald-600' />
          <h2 className='text-lg font-semibold'>
            {t('supplier.uploadComplete')}
          </h2>
          <Button variant='outline' onClick={restart}>
            {t('supplier.restart')}
          </Button>
        </div>
      )}
      {!completed && !flow && (
        <QueryState
          pending={options.isPending}
          error={options.error}
          retry={options.refresh}
        >
          <Freshness pending={options.isFetching} refresh={options.refresh} />
          {options.data && (
            <UploadConfig
              bindingId={props.bindingId}
              options={options.data}
              pending={authorize.isPending}
              onSubmit={(data) => authorize.mutate(data)}
            />
          )}
        </QueryState>
      )}
      {flow && (
        <div className='grid max-w-xl gap-5'>
          <p className='text-muted-foreground text-sm'>
            {t('supplier.expires')}: <Time value={flow.expires_at} />
          </p>
          {expired && (
            <p role='alert' className='text-destructive text-sm'>
              {t('supplier.expired')}
            </p>
          )}
          {!url && (
            <p role='alert' className='text-destructive text-sm'>
              {t('supplier.invalidUrl')}
            </p>
          )}
          {url && !expired && (
            <a
              className='inline-flex w-fit items-center gap-2 text-sm font-medium text-emerald-700 underline underline-offset-4 dark:text-emerald-400'
              href={url}
              target='_blank'
              rel='noopener noreferrer'
              referrerPolicy='no-referrer'
            >
              {t('supplier.openAuthorization')}
              <ExternalLink className='size-4' />
            </a>
          )}
          <form
            className='grid gap-4'
            onSubmit={(event) => {
              event.preventDefault()
              if (!expired && url && callback.trim()) exchange.mutate()
            }}
          >
            <Field id='oauth-callback' label={t('supplier.callback')}>
              <Textarea
                id='oauth-callback'
                value={callback}
                onChange={(event) => setCallback(event.target.value)}
                spellCheck={false}
                autoComplete='off'
                disabled={expired || !url}
                required
              />
            </Field>
            <div className='flex flex-wrap gap-2'>
              <Button
                type='submit'
                disabled={
                  exchange.isPending || expired || !url || !callback.trim()
                }
              >
                {t('supplier.exchange')}
              </Button>
              <Button
                type='button'
                variant='outline'
                disabled={exchange.isPending}
                onClick={restart}
              >
                {t('supplier.restart')}
              </Button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}
