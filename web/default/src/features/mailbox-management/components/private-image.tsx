import { ImageOff, Maximize2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { mailboxApi } from '../api'
import { MailboxError, errorKey } from '../lib/errors'
import type { AccountType, Attachment } from '../types'
import { ImageViewer } from './image-viewer'

export function PrivateImage(props: {
  accountType: AccountType
  attachment: Attachment
  index: number
}) {
  const { t } = useTranslation()
  const [url, setURL] = useState('')
  const [error, setError] = useState<unknown>()
  const [expanded, setExpanded] = useState(false)
  const returnFocus = useRef<HTMLElement | null>(null)
  const [loaded, setLoaded] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    let objectURL = ''
    let expiry: ReturnType<typeof setTimeout> | undefined
    setURL('')
    setExpanded(false)
    setLoaded(false)
    setError(undefined)
    if (
      props.attachment.deleted_at ||
      props.attachment.expires_at * 1000 <= Date.now()
    ) {
      setError(new MailboxError('mailbox_attachment_expired', 410))
      return
    }
    void mailboxApi
      .attachment(props.attachment.id, controller.signal, props.accountType)
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
    props.accountType,
    props.attachment.id,
    props.attachment.deleted_at,
    props.attachment.expires_at,
  ])
  const label = t('mailbox.admin.screenshot', { index: props.index + 1 })
  const invalid = () => {
    setExpanded(false)
    setLoaded(false)
    setError(new MailboxError('mailbox_attachment_invalid'))
  }
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
              <button
                type='button'
                className='focus-visible:ring-ring h-full w-full cursor-zoom-in outline-none focus-visible:ring-2 focus-visible:ring-inset disabled:cursor-wait'
                disabled={!loaded}
                aria-label={`${t('mailbox.admin.expandImage')} · ${label}`}
                onClick={(event) => {
                  returnFocus.current = event.currentTarget
                  setExpanded(true)
                }}
              >
                <img
                  src={url}
                  alt={label}
                  className='h-full w-full object-contain'
                  onLoad={() => setLoaded(true)}
                  onError={invalid}
                />
              </button>
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
                disabled={!loaded}
                onClick={(event) => {
                  returnFocus.current = event.currentTarget
                  setExpanded(true)
                }}
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
      {expanded && url && error == null && (
        <ImageViewer
          url={url}
          label={label}
          returnFocus={returnFocus}
          onClose={() => setExpanded(false)}
          onError={invalid}
        />
      )}
    </figure>
  )
}
