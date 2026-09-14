import { Eye, EyeOff, KeyRound, ShieldCheck } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { mailboxApi } from '../api'
import { useMailboxMutation, useMailboxQuery } from '../hooks'
import { errorKey } from '../lib/errors'
import { otpRemaining } from '../lib/schemas'
import type { Credential } from '../types'
import { CopyButton, Modal, QueryState, Status, Time } from './common'

export function CredentialDetail(props: {
  id: number
  canView: boolean
  canCredentials: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const account = useMailboxQuery(
    ['account', props.id],
    (signal) => mailboxApi.account(props.id, signal),
    props.canView
  )
  const [password, setPassword] = useState('')
  const [otp, setOtp] = useState<Credential>()
  const [otpEnabled, setOtpEnabled] = useState(false)
  const [tick, setTick] = useState(0)
  const [generation, setGeneration] = useState(0)
  const [otpPending, setOtpPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const offset = useRef(0)
  const passwordController = useRef<AbortController | undefined>(undefined)
  const passwordRequest = useMailboxMutation(async () => {
    passwordController.current?.abort()
    const controller = new AbortController()
    passwordController.current = controller
    setError(undefined)
    try {
      const result = await mailboxApi.credentials(
        props.id,
        'password',
        controller.signal
      )
      if (
        !controller.signal.aborted &&
        document.visibilityState === 'visible'
      ) {
        setPassword(result.password ?? '')
      }
    } catch (failure) {
      if (!controller.signal.aborted) {
        setPassword('')
        setOtp(undefined)
        setOtpEnabled(false)
        setError(failure)
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
    if (!otpEnabled) return
    const controller = new AbortController()
    setOtp(undefined)
    setOtpPending(true)
    setError(undefined)
    void mailboxApi
      .credentials(props.id, 'otp', controller.signal)
      .then((result) => {
        if (
          controller.signal.aborted ||
          document.visibilityState !== 'visible'
        ) {
          return
        }
        offset.current = result.server_time * 1000 - Date.now()
        setOtp(result)
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) {
          setPassword('')
          setOtp(undefined)
          setOtpEnabled(false)
          setError(failure)
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setOtpPending(false)
      })
    return () => controller.abort()
  }, [props.id, otpEnabled, generation])
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
  void tick
  const allowed =
    props.canCredentials &&
    (!props.canView || (!!account.data && !account.isError))
  return (
    <Modal
      title={
        account.data?.email ??
        t('mailbox.admin.accountDetail', { id: props.id })
      }
      onClose={props.onClose}
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
              </dl>
            )}
          </QueryState>
        )}
        {allowed && (
          <>
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
                    {remaining > 0 ? otp?.code : '------'}
                  </output>
                  <span className='text-muted-foreground text-sm'>
                    {otpPending
                      ? t('mailbox.admin.loading')
                      : t('mailbox.admin.expiresIn', { seconds: remaining })}
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
