import { Eye, EyeOff, KeyRound, ShieldCheck } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { mailboxApi } from '../api'
import { useMailboxMutation, useMailboxQuery } from '../hooks'
import { errorKey } from '../lib/errors'
import { otpRemaining } from '../lib/schemas'
import type { AccountType, Credential } from '../types'
import { CardCredential } from './card-credential'
import { CopyButton, Modal, QueryState, Status, Time } from './common'
import { TemporaryCvvCredential } from './temporary-cvv-credential'

export function CredentialDetail(props: {
  accountType: AccountType
  id: number
  canView: boolean
  canCredentials: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const account = useMailboxQuery(
    ['account', props.accountType, props.id],
    (signal) => mailboxApi.account(props.id, signal, props.accountType),
    props.canView
  )
  const [password, setPassword] = useState('')
  const [otp, setOtp] = useState<Credential>()
  const [otpEnabled, setOtpEnabled] = useState(false)
  const [tick, setTick] = useState(0)
  const [generation, setGeneration] = useState(0)
  const [cardGeneration, setCardGeneration] = useState(0)
  const [otpPending, setOtpPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const offset = useRef(0)
  const passwordController = useRef<AbortController | undefined>(undefined)
  const allowed =
    props.canCredentials &&
    (!props.canView || (!!account.data && !account.isError))
  const snapshot = `${props.accountType}:${props.id}:${account.data?.version ?? 0}:${account.data?.assignment_id ?? 0}:${account.data?.assignment_version ?? 0}`
  const credentialFailure = useCallback((failure: unknown) => {
    passwordController.current?.abort()
    setPassword('')
    setOtp(undefined)
    setOtpEnabled(false)
    setOtpPending(false)
    setCardGeneration((value) => value + 1)
    setError(failure)
  }, [])
  useEffect(() => {
    passwordController.current?.abort()
    setPassword('')
    setOtp(undefined)
    setOtpEnabled(false)
    setOtpPending(false)
  }, [allowed, snapshot])
  useEffect(() => {
    if (!password) return
    const expiry = window.setTimeout(() => setPassword(''), 60000)
    return () => window.clearTimeout(expiry)
  }, [password])
  const passwordRequest = useMailboxMutation(async () => {
    passwordController.current?.abort()
    const controller = new AbortController()
    passwordController.current = controller
    setError(undefined)
    try {
      const result = await mailboxApi.credentials(
        props.id,
        'password',
        controller.signal,
        props.accountType
      )
      if (
        !controller.signal.aborted &&
        document.visibilityState === 'visible'
      ) {
        setPassword(result.password ?? '')
      }
    } catch (failure) {
      if (!controller.signal.aborted) {
        credentialFailure(failure)
      }
    }
  })
  useEffect(() => {
    function hide() {
      if (document.visibilityState !== 'visible') {
        passwordController.current?.abort()
        setPassword('')
        setOtp(undefined)
        setOtpEnabled(false)
      }
    }
    document.addEventListener('visibilitychange', hide)
    return () => {
      passwordController.current?.abort()
      document.removeEventListener('visibilitychange', hide)
    }
  }, [])
  useEffect(() => {
    if (!otpEnabled || !allowed) return
    const controller = new AbortController()
    setOtp(undefined)
    setOtpPending(true)
    setError(undefined)
    void mailboxApi
      .credentials(props.id, 'otp', controller.signal, props.accountType)
      .then((result) => {
        if (
          controller.signal.aborted ||
          document.visibilityState !== 'visible'
        ) {
          return
        }
        offset.current = result.server_time * 1000 - Date.now()
        setOtp({
          available: result.available,
          code: result.code,
          expires_at: result.expires_at,
          server_time: result.server_time,
        })
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) {
          credentialFailure(failure)
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setOtpPending(false)
      })
    return () => controller.abort()
  }, [
    props.id,
    props.accountType,
    otpEnabled,
    generation,
    allowed,
    snapshot,
    credentialFailure,
  ])
  useEffect(() => {
    if (!otp?.expires_at || !otpEnabled) return
    const expiresAt = otp.expires_at
    const interval = window.setInterval(() => {
      if (otpRemaining(expiresAt, offset.current) === 0) {
        setOtp(undefined)
        setGeneration((value) => value + 1)
      }
      setTick((value) => value + 1)
    }, 1000)
    return () => window.clearInterval(interval)
  }, [otp, otpEnabled])
  const remaining = otp?.expires_at
    ? otpRemaining(otp.expires_at, offset.current)
    : 0
  const otpUnavailable = otp?.available === false
  let otpValue = '------'
  let otpStatus = t('mailbox.admin.expiresIn', { seconds: remaining })
  if (otpUnavailable) {
    otpValue = t('mailbox.admin.notProvided')
    otpStatus = t('mailbox.admin.noOtp')
  } else if (remaining > 0) {
    otpValue = otp?.code ?? '------'
  }
  if (!otpUnavailable && otpPending) {
    otpStatus = t('mailbox.admin.loading')
  }
  void tick
  return (
    <Modal
      title={
        account.data?.email ??
        t('mailbox.admin.accountDetail', { id: props.id })
      }
      onClose={props.onClose}
      description={t(`mailbox.admin.pools.${props.accountType}`)}
    >
      <div className='space-y-5'>
        {props.canCredentials && (
          <p className='text-muted-foreground border-l-2 border-amber-500 pl-3 text-sm'>
            {t('mailbox.admin.revocationWarning')}
          </p>
        )}
        {props.canView && (
          <QueryState
            pending={account.isPending}
            error={account.error}
            retry={() => void account.refetch()}
          >
            {account.data && (
              <dl className='grid grid-cols-2 gap-3 text-sm'>
                <dt>{t('mailbox.admin.status')}</dt>
                <dd>
                  <Status value={account.data.status} />
                </dd>
                <dt>{t('mailbox.admin.operator')}</dt>
                <dd className='break-all'>
                  {account.data.operator_name || '--'}
                </dd>
                <dt>{t('mailbox.admin.assignedAt')}</dt>
                <dd>
                  <Time value={account.data.assigned_at} />
                </dd>
                <dt>{t('mailbox.admin.version')}</dt>
                <dd>{account.data.version}</dd>
                {props.accountType === 'opening' && account.data.card_last4 && (
                  <>
                    <dt>{t('mailbox.admin.card')}</dt>
                    <dd>
                      {t('mailbox.admin.cardEnding', {
                        last4: account.data.card_last4,
                      })}
                    </dd>
                  </>
                )}
              </dl>
            )}
          </QueryState>
        )}
        {allowed && (
          <>
            {props.accountType === 'opening' && (
              <>
                <CardCredential
                  key={`${snapshot}:${cardGeneration}`}
                  id={props.id}
                  onError={credentialFailure}
                />
                <TemporaryCvvCredential key={`cvv:${snapshot}`} id={props.id} />
              </>
            )}
            <section className='space-y-3 border-t pt-4'>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <KeyRound className='size-4' />
                {t('mailbox.admin.password')}
              </h3>
              {password ? (
                <div className='flex items-start gap-2'>
                  <output className='bg-muted/30 min-w-0 flex-1 rounded border p-3 font-mono break-all whitespace-pre-wrap'>
                    {password}
                  </output>
                  <CopyButton value={password} />
                  <Button
                    variant='outline'
                    size='icon'
                    title={t('mailbox.admin.hide')}
                    aria-label={t('mailbox.admin.hide')}
                    onClick={() => setPassword('')}
                  >
                    <EyeOff />
                  </Button>
                </div>
              ) : (
                <Button
                  variant='outline'
                  disabled={passwordRequest.isPending}
                  onClick={() => passwordRequest.submit(undefined)}
                >
                  <Eye />
                  {t('mailbox.admin.revealPassword')}
                </Button>
              )}
            </section>
            <section className='space-y-3 border-t pt-4'>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <ShieldCheck className='size-4' />
                {t('mailbox.admin.otp')}
              </h3>
              {otpEnabled ? (
                <div className='flex flex-wrap items-center gap-3'>
                  <output className='font-mono text-2xl tabular-nums'>
                    {otpValue}
                  </output>
                  <span className='text-muted-foreground text-sm'>
                    {otpStatus}
                  </span>
                  <CopyButton
                    value={otp?.code ?? ''}
                    disabled={!otp?.code || remaining <= 0}
                  />
                  <Button
                    variant='outline'
                    size='icon'
                    title={t('mailbox.admin.hide')}
                    aria-label={t('mailbox.admin.hide')}
                    onClick={() => {
                      setOtpEnabled(false)
                      setOtp(undefined)
                    }}
                  >
                    <EyeOff />
                  </Button>
                </div>
              ) : (
                <Button variant='outline' onClick={() => setOtpEnabled(true)}>
                  <Eye />
                  {t('mailbox.admin.revealOtp')}
                </Button>
              )}
            </section>
          </>
        )}
        {error != null && (
          <p role='alert' className='text-destructive'>
            {t(errorKey(error), {
              defaultValue: t('mailbox.errors.mailbox_request_failed'),
            })}
          </p>
        )}
      </div>
    </Modal>
  )
}
