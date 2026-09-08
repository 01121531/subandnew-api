import { useQuery } from '@tanstack/react-query'
import { Navigate } from '@tanstack/react-router'
import {
  BarChart3,
  KeyRound,
  LogOut,
  Network,
  ShieldCheck,
  Upload,
  Users,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ThemeSwitch } from '@/components/theme-switch'
import { Button } from '@/components/ui/button'
import { NativeSelectOption } from '@/components/ui/native-select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { Accounts } from './components/accounts'
import { QueryState, SelectField } from './components/common'
import { PasswordForm } from './components/password-form'
import { Proxies } from './components/proxies'
import { UploadWizard } from './components/upload-wizard'
import { UsageView } from './components/usage'
import { usePortalQuery, useSupplierMutation } from './hooks/use-portal-query'
import { portalApi } from './portal-api'
import { clearSupplierSession, sessionOptions } from './session'
import type { AuthSession } from './types'

export function SupplierPortal() {
  const session = useQuery(sessionOptions)
  if (session.data && !session.data.authenticated) {
    return <Navigate to='/supplier/sign-in' replace />
  }
  return (
    <QueryState
      pending={session.isPending}
      error={session.error}
      retry={() => void session.refetch()}
    >
      {session.data?.authenticated && (
        <PortalContent key={session.data.supplier.id} session={session.data} />
      )}
    </QueryState>
  )
}

function PortalContent(props: { session: AuthSession }) {
  const { t } = useTranslation()
  const bindings = usePortalQuery(['bindings'], (signal, refresh) =>
    portalApi.bindings(signal, refresh)
  )
  const [bindingId, setBindingId] = useState(0)
  const [view, setView] = useState('')
  const logout = useSupplierMutation(
    () => portalApi.logout(props.session.csrf_token),
    clearSupplierSession
  )
  const supplier = props.session.supplier
  const available = bindings.data?.filter((binding) => binding.enabled) ?? []
  const binding =
    available.find((item) => item.id === bindingId) ??
    (available.length === 1 ? available[0] : undefined)
  const tabs = [
    { id: 'accounts', allowed: supplier.view_accounts, icon: Users },
    { id: 'usage', allowed: supplier.view_usage, icon: BarChart3 },
    { id: 'proxies', allowed: supplier.manage_proxies, icon: Network },
    { id: 'upload', allowed: supplier.upload_accounts, icon: Upload },
    { id: 'security', allowed: true, icon: KeyRound },
  ].filter((tab) => tab.allowed)
  const current = tabs.find((tab) => tab.id === view)?.id ?? tabs[0].id
  return (
    <main className='bg-background text-foreground min-h-dvh min-w-0'>
      <header className='border-b'>
        <div className='mx-auto flex max-w-[1440px] flex-wrap items-center gap-3 px-4 py-4 sm:px-6'>
          <ShieldCheck className='size-8 shrink-0 text-emerald-600 dark:text-emerald-400' />
          <div className='min-w-0 flex-1'>
            <h1 className='text-xl font-semibold'>Claude Gateway</h1>
            <p className='text-muted-foreground truncate text-xs'>
              {t('supplier.portal')} / {supplier.name}
            </p>
          </div>
          <ThemeSwitch />
          <Button
            variant='ghost'
            size='icon'
            aria-label={t('supplier.logout')}
            title={t('supplier.logout')}
            disabled={logout.isPending}
            onClick={() => logout.mutate()}
          >
            <LogOut />
          </Button>
        </div>
      </header>
      <div className='mx-auto grid max-w-[1440px] min-w-0 gap-6 p-4 sm:p-6'>
        <QueryState
          pending={bindings.isPending}
          error={bindings.error}
          retry={bindings.refresh}
        >
          <div className='flex flex-wrap items-end justify-between gap-4'>
            <div className='w-full sm:w-80'>
              <SelectField
                id='portal-binding'
                label={t('supplier.binding')}
                value={binding?.id ?? 0}
                onChange={(value) => setBindingId(Number(value))}
              >
                <NativeSelectOption value={0}>
                  {t('supplier.chooseBinding')}
                </NativeSelectOption>
                {available.map((item) => (
                  <NativeSelectOption key={item.id} value={item.id}>
                    {item.instance_name}
                  </NativeSelectOption>
                ))}
              </SelectField>
            </div>
            {!supplier.manage_proxies && !supplier.upload_accounts && (
              <span className='text-muted-foreground text-xs'>
                {t('supplier.readonly')}
              </span>
            )}
          </div>
          {!available.length && (
            <p className='text-muted-foreground text-sm'>
              {t('supplier.noBindings')}
            </p>
          )}
        </QueryState>
        <Tabs
          value={current}
          onValueChange={(value) => setView(String(value))}
          className='min-w-0'
        >
          <div className='overflow-x-auto border-b pb-2'>
            <TabsList className='w-max'>
              {tabs.map((tab) => (
                <TabsTrigger key={tab.id} value={tab.id}>
                  <tab.icon className='size-4' />
                  {t(`supplier.${tab.id}`)}
                </TabsTrigger>
              ))}
            </TabsList>
          </div>
        </Tabs>
        <section
          key={`${binding?.id ?? 0}-${current}`}
          aria-label={t(`supplier.${current}`)}
          className='min-w-0'
        >
          {current === 'security' && <PasswordForm session={props.session} />}
          {current !== 'security' && !binding && (
            <p className='text-muted-foreground py-16 text-center text-sm'>
              {t('supplier.chooseBinding')}
            </p>
          )}
          {binding && current === 'accounts' && (
            <Accounts bindingId={binding.id} />
          )}
          {binding && current === 'usage' && (
            <UsageView bindingId={binding.id} />
          )}
          {binding && current === 'proxies' && (
            <Proxies bindingId={binding.id} csrf={props.session.csrf_token} />
          )}
          {binding && current === 'upload' && (
            <UploadWizard
              bindingId={binding.id}
              csrf={props.session.csrf_token}
            />
          )}
        </section>
      </div>
    </main>
  )
}
