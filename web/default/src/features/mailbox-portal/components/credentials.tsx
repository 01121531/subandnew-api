import { Copy, Eye, EyeOff, RefreshCw } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import {
  invalidateAccountCredentials,
  mailboxApi,
  onAccountFailure,
  onAuthFailure,
} from '../api'
import { useVisible } from '../hooks/use-visible'
import {
  credentialScope,
  readCredential,
  revealRemaining,
} from '../lib/credentials'
import {
  assertCurrentAssignment,
  canReadCredentials,
  otpSeconds,
} from '../lib/guards'
import { mailboxClient, onMailboxWorkspaceClear } from '../session'
import type { Account, Credential, CredentialKind } from '../types'
import { ErrorMessage } from './common'

function Secret(props: {
  account: Account
  csrf: string
  kind: CredentialKind
}) {
  const { t } = useTranslation()
  const { ref, visible } = useVisible()
  // The parent remounts secrets whenever credential-relevant metadata changes.
  const account = useRef(props.account).current
  const [requested, setRequested] = useState(false)
  const [value, setValue] = useState<{
    credential: Credential
    receivedAt: number
    startedAt: number
  } | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [now, setNow] = useState(Date.now())
  const [copying, setCopying] = useState(false)
  const [copied, setCopied] = useState(false)
  const copyController = useRef<AbortController | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    const startedAt = Date.now()
    let timer: ReturnType<typeof setTimeout> | undefined
    let clock: ReturnType<typeof setInterval> | undefined
    setValue(null)
    setError(null)
    setCopied(false)
    const clear = () => {
      controller.abort()
      copyController.current?.abort()
      clearTimeout(timer)
      clearInterval(clock)
      setValue(null)
      setRequested(false)
    }
    const unsubscribe = onAuthFailure(clear)
    const unsubscribeWorkspace = onMailboxWorkspaceClear(clear)
    const unsubscribeAccount = onAccountFailure((id) => {
      if (id === account.id) clear()
    })
    async function refresh() {
      setValue(null)
      setCopied(false)
      try {
        const credential = await readCredential(
          props.csrf,
          account,
          props.kind,
          controller.signal
        )
        if (controller.signal.aborted) return
        const receivedAt = Date.now()
        setNow(receivedAt)
        setValue({ credential, receivedAt, startedAt })
        if (props.kind === 'otp' && credential.available !== false) {
          const seconds = otpSeconds(credential, receivedAt, receivedAt)
          if (seconds <= 0) {
            setValue(null)
            clearInterval(clock)
            return
          }
          timer = setTimeout(() => void refresh(), seconds * 1000)
        }
      } catch (failure) {
        clearInterval(clock)
        if (controller.signal.aborted) return
        setError(failure)
        setValue(null)
        void mailboxClient.invalidateQueries({
          queryKey: [
            'mailbox',
            'account',
            account.id,
            account.account_type ?? 'refund',
          ],
        })
      }
    }
    if (!visible) setRequested(false)
    if (requested && visible && canReadCredentials(account)) {
      if (props.kind !== 'otp') {
        timer = setTimeout(clear, revealRemaining(startedAt, Date.now()))
      }
      void refresh()
      if (props.kind === 'otp') {
        clock = setInterval(() => setNow(Date.now()), 250)
      }
    }
    return () => {
      controller.abort()
      copyController.current?.abort()
      clearTimeout(timer)
      clearInterval(clock)
      unsubscribe()
      unsubscribeWorkspace()
      unsubscribeAccount()
    }
  }, [requested, visible, account, props.csrf, props.kind])
  const seconds = value
    ? otpSeconds(value.credential, value.receivedAt, now)
    : 0
  let secret = ''
  if (
    visible &&
    requested &&
    value &&
    canReadCredentials(account) &&
    (props.kind === 'otp' || revealRemaining(value.startedAt, Date.now()) > 0)
  ) {
    if (props.kind === 'password') secret = value.credential.password ?? ''
    else if (props.kind === 'card') secret = value.credential.card_number ?? ''
    else if (seconds > 0) secret = value.credential.code ?? ''
  }
  const expiry =
    secret && props.kind === 'card' ? (value?.credential.card_expiry ?? '') : ''
  async function copy() {
    if (!secret || copying) return
    const controller = new AbortController()
    copyController.current = controller
    setCopying(true)
    try {
      assertCurrentAssignment(
        account,
        await mailboxApi.account(
          account.id,
          controller.signal,
          account.account_type ?? 'refund'
        ),
        'credentials'
      )
      if (controller.signal.aborted || document.visibilityState !== 'visible') {
        return
      }
      if (
        props.kind === 'otp' &&
        value &&
        otpSeconds(value.credential, value.receivedAt, Date.now()) <= 0
      ) {
        return
      }
      if (
        props.kind !== 'otp' &&
        value &&
        revealRemaining(value.startedAt, Date.now()) <= 0
      ) {
        setValue(null)
        setRequested(false)
        return
      }
      await navigator.clipboard.writeText(
        expiry ? `${secret}\n${expiry}` : secret
      )
      if (!controller.signal.aborted) setCopied(true)
    } catch (failure) {
      invalidateAccountCredentials(account.id, failure)
      if (!controller.signal.aborted) {
        setValue(null)
        setError(failure)
      }
    } finally {
      setCopying(false)
    }
  }
  return (
    <div ref={ref} className='grid gap-3 border-b py-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h3 className='text-sm font-medium'>
          {t(`mailboxPortal.${props.kind}`)}
        </h3>
        <Button
          variant='outline'
          onClick={() => {
            copyController.current?.abort()
            setRequested(!requested)
            setValue(null)
          }}
          aria-pressed={requested}
        >
          {requested ? <EyeOff /> : <Eye />}
          {t(requested ? 'mailboxPortal.hide' : 'mailboxPortal.reveal')}
        </Button>
      </div>
      {secret && (
        <div className='bg-muted/50 flex min-w-0 items-start gap-2 rounded p-3'>
          <div className='grid min-w-0 flex-1 gap-2'>
            {props.kind === 'card' && (
              <span className='text-muted-foreground text-xs'>
                {t('mailboxPortal.cardNumber')}
              </span>
            )}
            <code className='[overflow-wrap:anywhere] whitespace-pre-wrap'>
              {secret}
            </code>
            {expiry && (
              <div className='grid gap-1'>
                <span className='text-muted-foreground text-xs'>
                  {t('mailboxPortal.cardExpiry')}
                </span>
                <code className='[overflow-wrap:anywhere]'>{expiry}</code>
              </div>
            )}
          </div>
          <Button
            variant='ghost'
            size='icon'
            disabled={copying}
            aria-label={t('mailboxPortal.copy')}
            title={t('mailboxPortal.copy')}
            onClick={() => void copy()}
          >
            <Copy />
          </Button>
        </div>
      )}
      {requested && visible && !value && !error && (
        <p role='status' className='text-muted-foreground text-xs'>
          {t('mailboxPortal.loading')}
        </p>
      )}
      {secret && props.kind === 'otp' && (
        <p className='text-muted-foreground text-xs'>
          {t('mailboxPortal.otpCountdown', { seconds })}
        </p>
      )}
      {props.kind === 'otp' && value?.credential.available === false && (
        <p role='status' className='text-muted-foreground text-sm'>
          {t('mailboxPortal.otpNotProvided')}
        </p>
      )}
      {copied && secret && (
        <p
          role='status'
          className='text-xs text-emerald-700 dark:text-emerald-400'
        >
          {t('mailboxPortal.copied')}
        </p>
      )}
      <ErrorMessage error={error} />
      {!!error && (
        <Button
          variant='ghost'
          onClick={() => {
            setRequested(false)
            setValue(null)
          }}
        >
          <RefreshCw />
          {t('mailboxPortal.reset')}
        </Button>
      )}
    </div>
  )
}

export function Credentials(props: { account: Account; csrf: string }) {
  const { t } = useTranslation()
  const [blocked, setBlocked] = useState(false)
  useEffect(() => {
    let invalidated = false
    return onAccountFailure((id) => {
      if (id === props.account.id) {
        setBlocked(true)
        if (!invalidated) {
          invalidated = true
          void mailboxClient.invalidateQueries({
            queryKey: [
              'mailbox',
              'account',
              id,
              props.account.account_type ?? 'refund',
            ],
          })
        }
      }
    })
  }, [props.account.id, props.account.account_type])
  if (blocked || !canReadCredentials(props.account)) {
    return (
      <p role='status' className='text-muted-foreground border-b py-5 text-sm'>
        {t('mailboxPortal.credentialsBlocked')}
      </p>
    )
  }
  return (
    <section
      key={credentialScope(props.account)}
      aria-label={t('mailboxPortal.credentials')}
    >
      <Secret {...props} kind='password' />
      <Secret {...props} kind='otp' />
      {props.account.account_type === 'opening' && (
        <Secret {...props} kind='card' />
      )}
    </section>
  )
}
