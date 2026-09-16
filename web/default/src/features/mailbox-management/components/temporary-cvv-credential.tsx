import { Eye, EyeOff, ShieldEllipsis } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { mailboxApi } from '../api'
import { errorKey } from '../lib/errors'
import { CopyButton } from './common'

export function TemporaryCvvCredential(props: { id: number }) {
  const { t } = useTranslation()
  const [cvv, setCvv] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const controller = useRef<AbortController | undefined>(undefined)
  useEffect(() => {
    if (!cvv) return
    const timer = window.setTimeout(() => setCvv(''), 60000)
    return () => window.clearTimeout(timer)
  }, [cvv])
  useEffect(() => {
    function hide() {
      if (document.visibilityState !== 'visible') {
        controller.current?.abort()
        setCvv('')
      }
    }
    document.addEventListener('visibilitychange', hide)
    return () => {
      controller.current?.abort()
      document.removeEventListener('visibilitychange', hide)
    }
  }, [])
  async function reveal() {
    if (controller.current && !controller.current.signal.aborted) return
    const request = new AbortController()
    controller.current = request
    setPending(true)
    setError(undefined)
    try {
      const result = await mailboxApi.credentials(
        props.id,
        'cvv',
        request.signal,
        'opening'
      )
      if (!request.signal.aborted && document.visibilityState === 'visible') {
        setCvv(result.cvv ?? '')
      }
    } catch (failure) {
      if (!request.signal.aborted) {
        setCvv('')
        setError(failure)
      }
    } finally {
      if (!request.signal.aborted) setPending(false)
      if (controller.current === request) controller.current = undefined
    }
  }
  return (
    <section className='space-y-3 border-t pt-4'>
      <h3 className='flex items-center gap-2 text-sm font-medium'>
        <ShieldEllipsis className='size-4' aria-hidden='true' />
        {t('mailbox.admin.cvv')}
      </h3>
      <p className='text-muted-foreground text-sm'>
        {t('mailbox.admin.adminCvvHint')}
      </p>
      {cvv ? (
        <div className='flex items-center gap-2'>
          <output className='bg-muted/30 min-w-0 flex-1 rounded border p-3 font-mono text-xl tabular-nums'>
            {cvv}
          </output>
          <CopyButton value={cvv} />
          <Button
            variant='outline'
            size='icon'
            title={t('mailbox.admin.hide')}
            aria-label={t('mailbox.admin.hide')}
            onClick={() => setCvv('')}
          >
            <EyeOff />
          </Button>
        </div>
      ) : (
        <Button
          variant='outline'
          disabled={pending}
          onClick={() => void reveal()}
        >
          <Eye />
          {t('mailbox.admin.revealCvv')}
        </Button>
      )}
      {error != null && (
        <p role='alert' className='text-destructive text-sm'>
          {t(errorKey(error), {
            defaultValue: t('mailbox.errors.mailbox_request_failed'),
          })}
        </p>
      )}
    </section>
  )
}
