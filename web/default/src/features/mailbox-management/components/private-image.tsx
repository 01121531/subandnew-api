import { ImageOff, Maximize2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { mailboxApi } from '../api'
import { MailboxError, errorKey } from '../lib/errors'
import type { Attachment } from '../types'
import { Modal } from './common'

export function PrivateImage(props: { attachment: Attachment; index: number }) {
  const { t } = useTranslation()
  const [url, setURL] = useState('')
  const [error, setError] = useState<unknown>()
  const [expanded, setExpanded] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    let objectURL = ''
    let expiry: ReturnType<typeof setTimeout> | undefined
    setURL('')
    setError(undefined)
    if (
      props.attachment.deleted_at ||
      props.attachment.expires_at * 1000 <= Date.now()
    ) {
      setError(new MailboxError('mailbox_attachment_expired', 410))
      return
    }
    void mailboxApi
      .attachment(props.attachment.id, controller.signal)
      .then((blob) => {
        if (controller.signal.aborted) return
        objectURL = URL.createObjectURL(blob)
        setURL(objectURL)
        // Long retention dates exceed the browser timeout range; check daily at most.
        const expire = () => {
          const delay = props.attachment.expires_at * 1000 - Date.now()
          if (delay > 0) {
            expiry = setTimeout(expire, Math.min(delay, 86400000))
            return
          }
          URL.revokeObjectURL(objectURL)
          setURL('')
          setExpanded(false)
          setError(new MailboxError('mailbox_attachment_expired', 410))
        }
        expire()
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) setError(failure)
      })
    return () => {
      controller.abort()
      if (objectURL) URL.revokeObjectURL(objectURL)
      if (expiry) clearTimeout(expiry)
    }
  }, [
    props.attachment.id,
    props.attachment.deleted_at,
    props.attachment.expires_at,
  ])
  const label = t('mailbox.admin.screenshot', { index: props.index + 1 })
  return (
    <figure className='min-w-0 space-y-2'>
      <div className='bg-muted/30 relative flex aspect-[4/3] items-center justify-center overflow-hidden rounded border'>
        {error != null ? (
          <div
            role='status'
            className='text-muted-foreground grid justify-items-center gap-2 p-3 text-center text-sm'
          >
            <ImageOff />
            <span>
              {t(errorKey(error), {
                defaultValue: t('mailbox.errors.mailbox_request_failed'),
              })}
            </span>
          </div>
        ) : (
          <>
            {url ? (
              <img
                src={url}
                alt={label}
                className='h-full w-full object-contain'
                onError={() =>
                  setError(new MailboxError('mailbox_attachment_invalid'))
                }
              />
            ) : (
              <span role='status'>{t('mailbox.admin.loading')}</span>
            )}
            {url && (
              <Button
                className='absolute right-2 bottom-2'
                variant='secondary'
                size='icon'
                title={t('mailbox.admin.expandImage')}
                aria-label={t('mailbox.admin.expandImage')}
                onClick={() => setExpanded(true)}
              >
                <Maximize2 />
              </Button>
            )}
          </>
        )}
      </div>
      <figcaption className='text-muted-foreground text-xs'>
        {label} · {props.attachment.width} × {props.attachment.height}
      </figcaption>
      {expanded && url && (
        <Modal title={label} onClose={() => setExpanded(false)}>
          <img src={url} alt={label} className='h-auto w-full object-contain' />
        </Modal>
      )}
    </figure>
  )
}
