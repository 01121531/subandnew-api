import { Copy, RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { useVisible } from '../hooks/use-visible'
import { credentialScope } from '../lib/credentials'
import { canReadCredentials, otpSeconds } from '../lib/guards'
import {
  visibleCredentials,
  type VisibleCredentials,
} from '../lib/visible-credentials'
import type { Account, CredentialKind } from '../types'
import { ErrorMessage } from './common'

export function Credentials(props: { account: Account; csrf: string }) {
  const { t } = useTranslation()
  if (!canReadCredentials(props.account)) {
    return (
      <p className='text-muted-foreground py-3 text-sm'>
        {t('mailboxPortal.credentialsBlocked')}
      </p>
    )
  }
  return (
    <VisibleFields
      key={`${props.csrf}:${credentialScope(props.account)}:${props.account.version}`}
      {...props}
    />
  )
}

function VisibleFields(props: { account: Account; csrf: string }) {
  const { ref, visible } = useVisible()
  const { t } = useTranslation()
  const [entry, setEntry] = useState<VisibleCredentials | null>(null)
  const [, render] = useState(0)
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    if (!visible) {
      setEntry(null)
      return
    }
    const value = visibleCredentials(props.account, props.csrf)
    const unsubscribe = value.subscribe(() => render((n) => n + 1))
    setEntry(value)
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => {
      unsubscribe()
      clearInterval(timer)
      setEntry(null)
    }
  }, [visible, props.account, props.csrf])
  const fields: ('email' | 'expiry' | CredentialKind)[] =
    props.account.account_type === 'opening'
      ? ['email', 'password', 'otp', 'card', 'expiry', 'cvv']
      : ['email', 'password', 'otp']
  return (
    <section
      ref={ref}
      aria-label={t('mailboxPortal.credentials')}
      className='bg-muted/35 grid min-w-0 gap-3 rounded-lg p-3 sm:grid-cols-2'
    >
      {fields.map((field) => {
        const kind = field === 'expiry' ? 'card' : field
        const state = kind === 'email' ? undefined : entry?.snapshot[kind]
        const value = visible ? state?.value : undefined
        const seconds = value
          ? otpSeconds(value, state?.receivedAt ?? now, now)
          : 0
        const texts = {
          email: props.account.email,
          password: value?.password,
          card: value?.card_number,
          expiry: value?.card_expiry,
          otp: seconds > 0 ? value?.code : undefined,
          cvv: value?.persistent || seconds > 0 ? value?.cvv : undefined,
        }
        const text = texts[field]
        const labels = {
          email: 'email',
          password: 'password',
          card: 'cardNumber',
          expiry: 'cardExpiry',
          otp: 'otp',
          cvv: 'cvv',
        }
        const label = labels[field]
        let placeholder = 'mailboxPortal.loading'
        if (value) {
          placeholder = 'mailboxPortal.cardUnavailable'
          if (field === 'otp') placeholder = 'mailboxPortal.otpNotProvided'
          if (field === 'cvv') placeholder = 'mailboxPortal.cvvUnavailable'
        }
        return (
          <CopyField
            key={field}
            label={t(`mailboxPortal.${label}`)}
            value={text}
            enabled={
              visible &&
              !!entry?.readable &&
              (field === 'email' || (!!text && value?.available !== false))
            }
            copy={async () => {
              if (entry) await entry.copy(field)
            }}
          >
            {!!state?.error && (
              <>
                <ErrorMessage error={state.error} />
                <Button
                  size='sm'
                  variant='ghost'
                  onClick={() => {
                    if (kind !== 'email') entry?.retry(kind)
                  }}
                >
                  <RefreshCw />
                  {t('mailboxPortal.retry')}
                </Button>
              </>
            )}
            {!state?.error && field !== 'email' && !text && (
              <span className='text-muted-foreground text-xs'>
                {t(placeholder)}
              </span>
            )}
            {!!text &&
              (field === 'otp' || (field === 'cvv' && !value?.persistent)) && (
                <span className='text-muted-foreground text-xs'>
                  {t(
                    `mailboxPortal.${field === 'otp' ? 'otpCountdown' : 'cvvCountdown'}`,
                    { seconds }
                  )}
                </span>
              )}
          </CopyField>
        )
      })}
    </section>
  )
}

function CopyField(props: {
  label: string
  value?: string
  enabled: boolean
  copy: () => Promise<void>
  children: React.ReactNode
}) {
  const { t } = useTranslation()
  const [copying, setCopying] = useState(false)
  const [message, setMessage] = useState('')
  useEffect(() => setMessage(''), [props.value, props.enabled])
  async function copy() {
    if (!props.enabled || copying) return
    setCopying(true)
    try {
      await props.copy()
      setMessage(t('mailboxPortal.copied'))
    } catch {
      setMessage(t('mailboxPortal.copyFailed'))
    } finally {
      setCopying(false)
    }
  }
  return (
    <div className='grid min-w-0 content-start gap-1.5'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <div className='flex min-w-0 items-start gap-1'>
        <button
          type='button'
          disabled={!props.enabled || copying}
          onClick={() => void copy()}
          aria-label={`${t('mailboxPortal.copy')} ${props.label}`}
          className='hover:bg-muted focus-visible:outline-ring min-w-0 flex-1 rounded px-1 py-1 text-left font-mono text-sm [overflow-wrap:anywhere] whitespace-pre-wrap focus-visible:outline-2 disabled:cursor-default'
        >
          {props.value ?? '—'}
        </button>
        <Button
          size='icon'
          variant='ghost'
          disabled={!props.enabled || copying}
          title={`${t('mailboxPortal.copy')} ${props.label}`}
          aria-label={`${t('mailboxPortal.copy')} ${props.label}`}
          onClick={() => void copy()}
        >
          <Copy />
        </Button>
      </div>
      {props.children}
      <span role='status' className='min-h-4 text-xs'>
        {message}
      </span>
    </div>
  )
}
