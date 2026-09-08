import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { Navigate, useNavigate } from '@tanstack/react-router'
import { ShieldCheck } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ThemeSwitch } from '@/components/theme-switch'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { Field, QueryState } from './components/common'
import { useSupplierMutation } from './hooks/use-portal-query'
import { errorKey } from './lib/errors'
import { loginSchema } from './lib/schemas'
import { portalApi } from './portal-api'
import { sessionOptions, supplierClient } from './session'

export function SupplierSignIn() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const session = useQuery(sessionOptions)
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
    <main className='bg-background text-foreground flex min-h-dvh flex-col'>
      <header className='flex items-center justify-between gap-3 border-b px-5 py-3'>
        <span className='flex items-center gap-2 font-semibold'>
          <ShieldCheck className='size-5 text-emerald-600 dark:text-emerald-400' />
          Claude Gateway
        </span>
        <ThemeSwitch />
      </header>
      <div className='m-auto w-full max-w-sm px-5 py-12'>
        <h1 className='mb-8 text-2xl font-semibold'>{t('supplier.signIn')}</h1>
        <QueryState
          pending={session.isPending}
          error={session.error}
          retry={() => void session.refetch()}
        >
          <form
            className='grid gap-5'
            onSubmit={form.handleSubmit((data) => login.mutate(data))}
          >
            <Field
              id='supplier-username'
              label={t('supplier.username')}
              error={!!form.formState.errors.username}
            >
              <Input
                id='supplier-username'
                autoComplete='username'
                {...form.register('username')}
              />
            </Field>
            <Field
              id='supplier-password'
              label={t('supplier.password')}
              error={!!form.formState.errors.password}
            >
              <Input
                id='supplier-password'
                type='password'
                autoComplete='current-password'
                {...form.register('password')}
              />
            </Field>
            {login.error && (
              <p role='alert' className='text-destructive text-sm'>
                {t(errorKey(login.error))}
              </p>
            )}
            <Button type='submit' disabled={login.isPending}>
              {login.isPending ? t('supplier.loading') : t('supplier.signIn')}
            </Button>
          </form>
        </QueryState>
      </div>
    </main>
  )
}
