import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { Navigate, useNavigate } from '@tanstack/react-router'
import { Eye, EyeOff, LogIn, ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ThemeSwitch } from '@/components/theme-switch'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { Field, QueryState } from './components/common'
import { useSupplierMutation } from './hooks/use-portal-query'
import { usePortalTitle } from './hooks/use-portal-title'
import { errorKey } from './lib/errors'
import { loginSchema } from './lib/schemas'
import { portalApi } from './portal-api'
import { sessionOptions, supplierClient } from './session'

import '@/styles/supplier-portal.css'

export function SupplierSignIn() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const session = useQuery(sessionOptions)
  const title = usePortalTitle(session.data?.portal?.title)
  const [showPassword, setShowPassword] = useState(false)
  const form = useForm({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: '', password: '' },
  })
  const login = useSupplierMutation(portalApi.login, (data) => {
    form.reset()
    supplierClient.removeQueries({ queryKey: ['supplier'] })
    supplierClient.setQueryData(sessionOptions.queryKey, data)
    if (data.authenticated) void navigate({ to: '/supplier', replace: true })
  })
  if (session.data?.authenticated) return <Navigate to='/supplier' replace />
  return (
    <main className='supplier-portal supplier-experience supplier-page text-foreground flex min-h-dvh flex-col gap-8 pb-8'>
      <header className='supplier-topbar flex items-center justify-between gap-3 border-b px-5 py-3 sm:px-8'>
        <span className='flex min-w-0 items-center gap-2 font-semibold [overflow-wrap:anywhere]'>
          <ShieldCheck className='text-primary size-5 shrink-0' />
          {title}
        </span>
        <ThemeSwitch contentClassName='supplier-portal supplier-experience' />
      </header>
      <div className='supplier-login-form m-auto w-[calc(100%-2rem)] max-w-[420px] p-6 sm:p-8'>
        <div className='mb-8 grid gap-3'>
          <h1 className='text-2xl font-semibold'>{t('supplier.signIn')}</h1>
        </div>
        <QueryState
          pending={session.isPending}
          error={session.error}
          retry={() => void session.refetch()}
        >
          <form
            className='grid gap-5'
            noValidate
            onSubmit={form.handleSubmit((data) => {
              if (!login.isPending) login.mutate(data)
            })}
          >
            <fieldset disabled={login.isPending} className='grid min-w-0 gap-5'>
              <Field
                id='supplier-username'
                label={t('supplier.username')}
                error={!!form.formState.errors.username}
              >
                <Input
                  id='supplier-username'
                  autoComplete='username'
                  autoCapitalize='none'
                  spellCheck={false}
                  aria-invalid={!!form.formState.errors.username}
                  aria-describedby={
                    form.formState.errors.username
                      ? 'supplier-username-error'
                      : undefined
                  }
                  {...form.register('username')}
                />
              </Field>
              <Field
                id='supplier-password'
                label={t('supplier.password')}
                error={!!form.formState.errors.password}
              >
                <div className='relative'>
                  <Input
                    id='supplier-password'
                    type={showPassword ? 'text' : 'password'}
                    className='pr-12'
                    autoComplete='current-password'
                    aria-invalid={!!form.formState.errors.password}
                    aria-describedby={
                      form.formState.errors.password
                        ? 'supplier-password-error'
                        : undefined
                    }
                    {...form.register('password')}
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='absolute top-0 right-0'
                    aria-label={t(
                      showPassword
                        ? 'supplier.ui_hidePassword'
                        : 'supplier.ui_showPassword'
                    )}
                    title={t(
                      showPassword
                        ? 'supplier.ui_hidePassword'
                        : 'supplier.ui_showPassword'
                    )}
                    aria-pressed={showPassword}
                    onClick={() => setShowPassword((value) => !value)}
                  >
                    {showPassword ? <EyeOff /> : <Eye />}
                  </Button>
                </div>
              </Field>
              {login.error && (
                <p role='alert' className='text-destructive text-sm'>
                  {t(errorKey(login.error))}
                </p>
              )}
              <Button
                type='submit'
                className='mt-1 w-full'
                disabled={login.isPending}
              >
                <LogIn className='size-4' />
                {login.isPending ? t('supplier.loading') : t('supplier.signIn')}
              </Button>
            </fieldset>
          </form>
        </QueryState>
      </div>
    </main>
  )
}
