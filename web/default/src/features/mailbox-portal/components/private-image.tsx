import { ImageOff, RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'

import { mailboxApi, onAuthFailure } from '../api'
import { useVisible } from '../hooks/use-visible'
import { errorKey } from '../lib/errors'
import type { Attachment } from '../types'

export function PrivateImage(props: { attachment: Attachment }) {
  const { t } = useTranslation()
  const { ref, visible } = useVisible()
  const [url, setUrl] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const [open, setOpen] = useState(false)
  const expired =
    !!props.attachment.deleted_at ||
    props.attachment.expires_at <= Date.now() / 1000
  useEffect(() => {
    const controller = new AbortController()
    let objectUrl = ''
    let timer: ReturnType<typeof setTimeout> | undefined
    const clear = () => {
      controller.abort()
      if (objectUrl) URL.revokeObjectURL(objectUrl)
      setUrl('')
      setOpen(false)
    }
    const unsubscribe = onAuthFailure(clear)
    async function load() {
      setError(null)
      try {
        const blob = await mailboxApi.attachment(
          props.attachment.id,
          controller.signal
        )
        if (controller.signal.aborted) return
        objectUrl = URL.createObjectURL(blob)
        setUrl(objectUrl)
        timer = setTimeout(
          clear,
          Math.min(
            2147483647,
            Math.max(0, props.attachment.expires_at * 1000 - Date.now())
          )
        )
      } catch (failure) {
        if (!controller.signal.aborted) setError(failure)
      }
    }
    if (!expired && visible) void load()
    return () => {
      clear()
      clearTimeout(timer)
      unsubscribe()
    }
  }, [
    props.attachment.id,
    props.attachment.expires_at,
    expired,
    visible,
    attempt,
  ])
  return (
    <div ref={ref} className='min-w-0'>
      {url ? (
        <>
          <button
            type='button'
            className='bg-muted focus-visible:ring-ring block aspect-[4/3] w-full overflow-hidden rounded border focus-visible:ring-2'
            onClick={() => setOpen(true)}
            aria-label={t('mailboxPortal.openImage')}
          >
            <img
              src={url}
              alt={t('mailboxPortal.screenshot')}
              className='size-full object-contain'
            />
          </button>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogContent className='max-h-[90dvh] w-[calc(100%-2rem)] max-w-4xl overflow-auto'>
              <DialogTitle>{t('mailboxPortal.screenshot')}</DialogTitle>
              <img
                src={url}
                alt={t('mailboxPortal.screenshot')}
                className='max-h-[75dvh] w-full object-contain'
              />
            </DialogContent>
          </Dialog>
        </>
      ) : (
        <div className='bg-muted/40 text-muted-foreground grid aspect-[4/3] place-content-center justify-items-center gap-2 rounded border p-3 text-center text-xs'>
          <ImageOff aria-hidden='true' className='size-5' />
          <span>
            {t(
              expired
                ? 'mailboxPortal.imageExpired'
                : 'mailboxPortal.imageLoading'
            )}
          </span>
          {!!error && (
            <>
              <span role='alert'>{t(errorKey(error))}</span>
              <Button
                variant='ghost'
                size='icon'
                title={t('mailboxPortal.retry')}
                aria-label={t('mailboxPortal.retry')}
                onClick={() => setAttempt(attempt + 1)}
              >
                <RefreshCw />
              </Button>
            </>
          )}
        </div>
      )}
    </div>
  )
}
