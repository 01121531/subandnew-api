import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Navigate } from '@tanstack/react-router'
import { Eye, EyeOff, LogIn, Mail } from 'lucide-react'
import { useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { MailboxAssistantDownload } from '@/components/mailbox-assistant-download'
import { ThemeSwitch } from '@/components/theme-switch'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { mailboxApi } from './api'
import { ErrorMessage, Field, QueryState } from './components/common'
import { loginSchema } from './lib/schemas'
import { acceptSession, mailboxClient, sessionOptions } from './session'

export function MailboxSignIn() {
  const { t } = useTranslation()
  const session = useQuery(sessionOptions)
  const [show, setShow] = useState(false)
  const lock = useRef(false)
  const form = useForm({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: '', password: '' },
  })
  const login = useMutation({
    mutationFn: () => mailboxApi.login(form.getValues()),
    onSuccess: (data) => {
      mailboxClient.setQueryData(sessionOptions.queryKey, acceptSession(data))
    },
    onSettled: () => {
      form.setValue('password', '')
      setShow(false)
      lock.current = false
    },
  })
  if (session.data?.authenticated) return <Navigate to='/mailbox' replace />
  return (
    <main className='bg-muted/30 text-foreground flex min-h-dvh flex-col'>
      <header className='bg-background flex min-h-16 items-center justify-between gap-3 border-b px-4 sm:px-6'>
        <span className='flex min-w-0 items-center gap-2 font-semibold [overflow-wrap:anywhere]'>
          <Mail
            className='size-5 shrink-0 text-emerald-600'
            aria-hidden='true'
          />
          {t('mailboxPortal.title')}
        </span>
        <ThemeSwitch />
      </header>
      <div className='m-auto w-full max-w-[420px] px-5 py-10 sm:px-8'>
        <h1 className='mb-8 text-2xl font-semibold'>
          {t('mailboxPortal.signIn')}
        </h1>
        <QueryState
          pending={session.isPending}
          error={session.error}
          retry={() => void session.refetch()}
        >
          <form
            noValidate
            onSubmit={form.handleSubmit(() => {
              if (!lock.current) {
                lock.current = true
                login.mutate()
              }
            })}
          >
            <fieldset disabled={login.isPending} className='grid min-w-0 gap-5'>
              <Field
                id='mailbox-username'
                label={t('mailboxPortal.username')}
                error={
                  form.formState.errors.username && 'mailboxPortal.required'
                }
              >
                <Input
                  id='mailbox-username'
                  autoComplete='username'
                  autoCapitalize='none'
                  spellCheck={false}
                  aria-invalid={!!form.formState.errors.username}
                  aria-describedby={
                    form.formState.errors.username
                      ? 'mailbox-username-error'
                      : undefined
                  }
                  {...form.register('username')}
                />
              </Field>
              <Field
                id='mailbox-password'
                label={t('mailboxPortal.password')}
                error={
                  form.formState.errors.password && 'mailboxPortal.required'
                }
              >
                <div className='relative'>
                  <Input
                    id='mailbox-password'
                    type={show ? 'text' : 'password'}
                    className='pr-10'
                    autoComplete='current-password'
                    aria-invalid={!!form.formState.errors.password}
                    aria-describedby={
                      form.formState.errors.password
                        ? 'mailbox-password-error'
                        : undefined
                    }
                    {...form.register('password')}
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='absolute top-0 right-0'
                    title={t(
                      show
                        ? 'mailboxPortal.hidePassword'
                        : 'mailboxPortal.showPassword'
                    )}
                    aria-label={t(
                      show
                        ? 'mailboxPortal.hidePassword'
                        : 'mailboxPortal.showPassword'
                    )}
                    aria-pressed={show}
                    onClick={() => setShow(!show)}
                  >
                    {show ? <EyeOff /> : <Eye />}
                  </Button>
                </div>
              </Field>
              <ErrorMessage error={login.error} />
              <Button type='submit' className='mt-2 h-10 w-full'>
                <LogIn />
                {t(
                  login.isPending
                    ? 'mailboxPortal.loading'
                    : 'mailboxPortal.signIn'
                )}
              </Button>
            </fieldset>
          </form>
        </QueryState>
        <MailboxAssistantDownload className='mt-5 w-full' />
      </div>
    </main>
  )
}
