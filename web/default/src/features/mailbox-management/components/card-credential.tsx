import { CreditCard, Eye, EyeOff } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { mailboxApi } from '../api'

// Mounted only while the current account has credential permission.
export function CardCredential(props: {
  id: number
  onError: (error: unknown) => void
}) {
  const { t } = useTranslation()
  const [card, setCard] = useState<{ number: string; expiry: string }>()
  const [pending, setPending] = useState(false)
  const controller = useRef<AbortController | undefined>(undefined)
  useEffect(() => {
    if (!card) return
    const expiry = window.setTimeout(() => setCard(undefined), 60000)
    return () => window.clearTimeout(expiry)
  }, [card])
  useEffect(() => {
    function hide() {
      if (document.visibilityState !== 'visible') {
        controller.current?.abort()
        setCard(undefined)
        setPending(false)
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
    try {
      const result = await mailboxApi.credentials(
        props.id,
        'card',
        request.signal,
        'opening'
      )
      if (!request.signal.aborted && document.visibilityState === 'visible') {
        setCard({
          number: result.card_number ?? '',
          expiry: result.card_expiry ?? '',
        })
      }
    } catch (failure) {
      if (!request.signal.aborted) {
        setCard(undefined)
        props.onError(failure)
      }
    } finally {
      if (!request.signal.aborted) setPending(false)
      if (controller.current === request) controller.current = undefined
    }
  }
  return (
    <section className='space-y-3 border-t pt-4'>
      <h3 className='flex items-center gap-2 text-sm font-medium'>
        <CreditCard className='size-4' aria-hidden='true' />
        {t('mailbox.admin.card')}
      </h3>
      <p className='text-muted-foreground text-sm'>
        {t('mailbox.admin.screenshotRedaction')}
      </p>
      {card ? (
        <div className='flex items-start gap-2'>
          <dl className='min-w-0 flex-1 space-y-2 rounded border p-3 text-sm'>
            <dt>{t('mailbox.admin.cardNumber')}</dt>
            <dd className='font-mono break-all'>{card.number}</dd>
            <dt>{t('mailbox.admin.cardExpiry')}</dt>
            <dd className='font-mono'>{card.expiry}</dd>
          </dl>
          <Button
            variant='outline'
            size='icon'
            title={t('mailbox.admin.hide')}
            aria-label={t('mailbox.admin.hide')}
            onClick={() => setCard(undefined)}
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
          {t('mailbox.admin.revealCard')}
        </Button>
      )}
    </section>
  )
}
