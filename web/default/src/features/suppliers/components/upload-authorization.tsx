import { Copy, ExternalLink } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

import type { OAuthFlow } from '../types'
import { Field, Time } from './common'

export function UploadAuthorization(props: {
  flow: OAuthFlow
  url: string | null
  expired: boolean
  pending: boolean
  callback: string
  onCallback: (value: string) => void
  onSubmit: () => void
}) {
  const { t } = useTranslation()
  const copy = async () => {
    if (!props.url || props.expired) return
    try {
      await navigator.clipboard.writeText(props.url)
      toast.success(t('supplier.linkCopied'))
    } catch {
      toast.error(t('supplier.linkCopyFailed'))
    }
  }
  return (
    <div className='grid min-w-0 gap-5'>
      {props.flow.resolved_name && (
        <Field id='supplier-resolved-name' label={t('supplier.resolvedName')}>
          <output
            id='supplier-resolved-name'
            className='block font-mono text-sm [overflow-wrap:anywhere]'
          >
            {props.flow.resolved_name}
          </output>
        </Field>
      )}
      <p className='text-muted-foreground text-sm'>
        {t('supplier.expires')}: <Time value={props.flow.expires_at} />
      </p>
      {props.expired && (
        <p role='alert' className='text-destructive text-sm'>
          {t('supplier.expired')}
        </p>
      )}
      {!props.url && (
        <p role='alert' className='text-destructive text-sm'>
          {t('supplier.invalidUrl')}
        </p>
      )}
      {props.url && (
        <Field
          id='supplier-authorization-url'
          label={t('supplier.authorizationUrl')}
        >
          <div className='flex items-start gap-2'>
            <Textarea
              id='supplier-authorization-url'
              value={props.url}
              readOnly
              rows={4}
              spellCheck={false}
              className='min-w-0 flex-1 resize-none font-mono text-xs break-all'
            />
            <Button
              variant='outline'
              size='icon'
              disabled={props.expired}
              title={t('supplier.copyLink')}
              aria-label={t('supplier.copyLink')}
              onClick={() => void copy()}
            >
              <Copy />
            </Button>
          </div>
        </Field>
      )}
      {props.url && !props.expired && (
        <a
          href={props.url}
          target='_blank'
          rel='noopener noreferrer'
          referrerPolicy='no-referrer'
          className='text-primary inline-flex w-fit items-center gap-2 text-sm font-medium underline underline-offset-4'
        >
          {t('supplier.openAuthorization')}
          <ExternalLink className='size-4' />
        </a>
      )}
      <form
        id='supplier-upload-exchange'
        onSubmit={(event) => {
          event.preventDefault()
          props.onSubmit()
        }}
      >
        <Field id='oauth-callback' label={t('supplier.callback')}>
          <Textarea
            id='oauth-callback'
            value={props.callback}
            onChange={(event) => props.onCallback(event.target.value)}
            spellCheck={false}
            autoComplete='off'
            disabled={props.pending || props.expired || !props.url}
            required
          />
        </Field>
      </form>
    </div>
  )
}
