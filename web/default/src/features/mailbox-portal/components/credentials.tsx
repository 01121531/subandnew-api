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
import { readCredential } from '../lib/credentials'
import {
  assertCurrentAssignment,
  canReadCredentials,
  otpSeconds,
} from '../lib/guards'
import { mailboxClient } from '../session'
import type { Account, Credential } from '../types'
import { ErrorMessage } from './common'

function Secret(props: {
  account: Account
  csrf: string
  kind: 'password' | 'otp'
}) {
  const { t } = useTranslation()
  const { ref, visible } = useVisible()
  const [requested, setRequested] = useState(false)
  const [value, setValue] = useState<{
    credential: Credential
    receivedAt: number
  } | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [now, setNow] = useState(Date.now())
  const [copying, setCopying] = useState(false)
  const [copied, setCopied] = useState(false)
  const copyController = useRef<AbortController | null>(null)
  useEffect(() => {
    const controller = new AbortController()
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
    const unsubscribeAccount = onAccountFailure((id) => {
      if (id === props.account.id) clear()
    })
    async function refresh() {
      setValue(null)
      setCopied(false)
      try {
        const credential = await readCredential(
          props.csrf,
          props.account,
          props.kind,
          controller.signal
        )
        if (controller.signal.aborted) return
        const receivedAt = Date.now()
        setNow(receivedAt)
        setValue({ credential, receivedAt })
        if (props.kind === 'otp') {
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
          queryKey: ['mailbox', 'account', props.account.id],
        })
      }
    }
    if (requested && visible && canReadCredentials(props.account)) {
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
      unsubscribeAccount()
    }
  }, [requested, visible, props.account, props.csrf, props.kind])
  const seconds = value
    ? otpSeconds(value.credential, value.receivedAt, now)
    : 0
  let secret = ''
  if (visible && requested && value) {
    if (props.kind === 'password') secret = value.credential.password ?? ''
    else if (seconds > 0) secret = value.credential.code ?? ''
  }
  async function copy() {
    if (!secret || copying) return
    const controller = new AbortController()
    copyController.current = controller
    setCopying(true)
    try {
      assertCurrentAssignment(
        props.account,
        await mailboxApi.account(props.account.id, controller.signal),
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
      await navigator.clipboard.writeText(secret)
      if (!controller.signal.aborted) setCopied(true)
    } catch (failure) {
      invalidateAccountCredentials(props.account.id, failure)
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
          <code className='min-w-0 flex-1 [overflow-wrap:anywhere] whitespace-pre-wrap'>
            {secret}
          </code>
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
            queryKey: ['mailbox', 'account', id],
          })
        }
      }
    })
  }, [props.account.id])
  if (blocked || !canReadCredentials(props.account)) {
    return (
      <p role='status' className='text-muted-foreground border-b py-5 text-sm'>
        {t('mailboxPortal.credentialsBlocked')}
      </p>
    )
  }
  return (
    <section aria-label={t('mailboxPortal.credentials')}>
      <Secret {...props} kind='password' />
      <Secret {...props} kind='otp' />
    </section>
  )
}
