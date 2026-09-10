import { useQuery } from '@tanstack/react-query'
import { Navigate } from '@tanstack/react-router'
import { LogOut, Menu, Upload } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ThemeSwitch } from '@/components/theme-switch'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'

import { Accounts } from './components/accounts'
import { Empty, QueryState } from './components/common'
import { PasswordForm } from './components/password-form'
import { PortalNavigation } from './components/portal-navigation'
import { Proxies } from './components/proxies'
import { UploadWizard } from './components/upload-wizard'
import { UsageView } from './components/usage'
import { usePortalQuery, useSupplierMutation } from './hooks/use-portal-query'
import { portalViews } from './lib/portal-views'
import { portalApi } from './portal-api'
import { clearSupplierSession, sessionOptions } from './session'
import type { AuthSession } from './types'

import '@/styles/supplier-portal.css'

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
  const [drawer, setDrawer] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)
  const logout = useSupplierMutation(
    () => portalApi.logout(props.session.csrf_token),
    clearSupplierSession
  )
  const supplier = props.session.supplier
  const available = bindings.data?.filter((item) => item.enabled) ?? []
  const binding =
    available.find((item) => item.id === bindingId) ??
    (available.length === 1 ? available[0] : undefined)
  const views = portalViews(supplier)
  const current = views.find((item) => item.id === view)?.id ?? views[0].id
  const navigation = (mobile: boolean) => (
    <PortalNavigation
      mobile={mobile}
      name={supplier.name}
      views={views}
      current={current}
      bindings={available}
      bindingId={binding?.id ?? 0}
      disabled={uploadOpen}
      onBinding={setBindingId}
      onView={(next) => {
        setView(next)
        setDrawer(false)
      }}
    />
  )
  return (
    <div className='supplier-portal supplier-experience supplier-page text-foreground min-h-dvh min-w-0 lg:grid lg:grid-cols-[224px_minmax(0,1fr)]'>
      <aside className='supplier-sidebar sticky top-0 hidden h-dvh min-w-0 border-r lg:block'>
        {navigation(false)}
      </aside>
      <Sheet open={drawer} onOpenChange={setDrawer}>
        <SheetContent
          side='left'
          className='supplier-portal supplier-experience supplier-sidebar w-72 max-w-[85vw] gap-0'
          aria-describedby={undefined}
        >
          <SheetTitle className='sr-only'>
            {t('supplier.navigation')}
          </SheetTitle>
          {navigation(true)}
        </SheetContent>
      </Sheet>
      <div className='min-w-0'>
        <header className='supplier-topbar sticky top-0 z-20 flex min-h-16 items-center gap-3 border-b px-4 lg:px-6'>
          <Button
            variant='ghost'
            size='icon'
            className='lg:hidden'
            aria-label={t('supplier.navigation')}
            onClick={() => setDrawer(true)}
          >
            <Menu />
          </Button>
          <div className='min-w-0 flex-1'>
            <p className='text-muted-foreground text-xs'>
              {t('supplier.portal')}
            </p>
            <p className='text-sm font-medium [overflow-wrap:anywhere] break-words'>
              {binding?.instance_name ?? supplier.name}
            </p>
          </div>
          <ThemeSwitch contentClassName='supplier-portal supplier-experience' />
          <Button
            variant='ghost'
            size='icon'
            aria-label={t('supplier.logout')}
            title={t('supplier.logout')}
            disabled={logout.isPending || uploadOpen}
            onClick={() => logout.mutate()}
          >
            <LogOut />
          </Button>
        </header>
        <main className='mx-auto grid max-w-[1600px] min-w-0 gap-6 p-4 sm:p-6'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <h1 className='text-xl font-semibold'>
              {t(`supplier.${current}`)}
            </h1>
            {current === 'accounts' && supplier.upload_accounts && (
              <Button
                disabled={!binding || uploadOpen}
                onClick={() => setUploadOpen(true)}
              >
                <Upload />
                {t('supplier.uploadAccounts')}
              </Button>
            )}
          </div>
          <QueryState
            pending={bindings.isPending}
            error={bindings.error}
            retry={bindings.refresh}
          >
            <section
              key={`${binding?.id ?? 0}-${current}`}
              aria-label={t(`supplier.${current}`)}
              className='supplier-workspace min-w-0'
            >
              {current === 'security' && (
                <PasswordForm session={props.session} />
              )}
              {current !== 'security' && !binding && (
                <Empty
                  message={t(
                    available.length
                      ? 'supplier.chooseBinding'
                      : 'supplier.noBindings'
                  )}
                />
              )}
              {binding && current === 'accounts' && supplier.view_accounts && (
                <Accounts bindingId={binding.id} />
              )}
              {binding && current === 'accounts' && !supplier.view_accounts && (
                <Empty message={t('supplier.accountsNotPermitted')} />
              )}
              {binding && current === 'usage' && (
                <UsageView bindingId={binding.id} />
              )}
              {binding && current === 'proxies' && (
                <Proxies
                  bindingId={binding.id}
                  csrf={props.session.csrf_token}
                />
              )}
            </section>
          </QueryState>
        </main>
      </div>
      {uploadOpen && binding && supplier.upload_accounts && (
        <UploadWizard
          key={binding.id}
          supplierId={supplier.id}
          bindingId={binding.id}
          bindingName={binding.instance_name}
          csrf={props.session.csrf_token}
          onClose={() => setUploadOpen(false)}
        />
      )}
    </div>
  )
}
