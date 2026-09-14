import { useMutation, useQuery } from '@tanstack/react-query'
import { Navigate } from '@tanstack/react-router'
import { ClipboardList, KeyRound, LogOut, Mail, Menu, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ThemeSwitch } from '@/components/theme-switch'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'

import { mailboxApi } from './api'
import { Accounts } from './components/accounts'
import { ErrorMessage, QueryState } from './components/common'
import { History } from './components/history'
import { Security } from './components/security'
import { clearMailboxSession, sessionOptions } from './session'
import type { Session } from './types'

type Tab = 'accounts' | 'submissions' | 'security'
const tabs = [
  { id: 'accounts', icon: Mail },
  { id: 'submissions', icon: ClipboardList },
  { id: 'security', icon: KeyRound },
] as const

function Navigation(props: { tab: Tab; onTab: (tab: Tab) => void }) {
  const { t } = useTranslation()
  return (
    <nav aria-label={t('mailboxPortal.navigation')} className='grid gap-1 p-3'>
      {tabs.map((tab) => (
        <Button
          key={tab.id}
          variant={props.tab === tab.id ? 'secondary' : 'ghost'}
          className='h-10 w-full justify-start gap-3'
          aria-current={props.tab === tab.id ? 'page' : undefined}
          onClick={() => props.onTab(tab.id)}
        >
          <tab.icon />
          {t(`mailboxPortal.${tab.id}`)}
        </Button>
      ))}
    </nav>
  )
}

function Workspace(props: {
  session: Extract<Session, { authenticated: true }>
}) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<Tab>('accounts')
  const [drawer, setDrawer] = useState(false)
  const logout = useMutation({
    mutationFn: () => mailboxApi.logout(props.session.csrf_token),
    onSuccess: clearMailboxSession,
  })
  useEffect(() => {
    const timer = setTimeout(
      clearMailboxSession,
      Math.max(0, props.session.expires_at * 1000 - Date.now())
    )
    return () => clearTimeout(timer)
  }, [props.session.expires_at])
  function select(value: Tab) {
    setTab(value)
    setDrawer(false)
  }
  return (
    <div className='bg-muted/30 text-foreground flex min-h-dvh min-w-0'>
      <aside className='bg-background fixed inset-y-0 left-0 hidden w-56 flex-col border-r lg:flex'>
        <div className='flex min-h-16 items-center gap-2 border-b px-4 font-semibold'>
          <Mail
            className='size-5 shrink-0 text-emerald-600'
            aria-hidden='true'
          />
          <span className='min-w-0 [overflow-wrap:anywhere]'>
            {t('mailboxPortal.title')}
          </span>
        </div>
        <Navigation tab={tab} onTab={select} />
        <div className='text-muted-foreground mt-auto border-t p-4 text-xs [overflow-wrap:anywhere]'>
          {props.session.operator.username}
        </div>
      </aside>
      <div className='flex min-w-0 flex-1 flex-col lg:ml-56'>
        <header className='bg-background flex min-h-16 flex-wrap items-center justify-between gap-2 border-b px-4 py-3 sm:px-6'>
          <div className='flex min-w-0 items-center gap-3'>
            <Button
              variant='ghost'
              size='icon'
              className='lg:hidden'
              aria-label={t('mailboxPortal.navigation')}
              title={t('mailboxPortal.navigation')}
              aria-expanded={drawer}
              onClick={() => setDrawer(true)}
            >
              <Menu />
            </Button>
            <h1 className='text-lg font-semibold'>
              {t(`mailboxPortal.${tab}`)}
            </h1>
          </div>
          <div className='flex min-w-0 items-center gap-2'>
            <span className='text-muted-foreground hidden max-w-48 truncate text-sm sm:inline'>
              {props.session.operator.display_name ||
                props.session.operator.username}
            </span>
            <ThemeSwitch />
            <Button
              variant='ghost'
              size='icon'
              disabled={logout.isPending}
              title={t('mailboxPortal.signOut')}
              aria-label={t('mailboxPortal.signOut')}
              onClick={() => logout.mutate()}
            >
              <LogOut />
            </Button>
          </div>
        </header>
        <main className='bg-background m-3 min-w-0 flex-1 px-3 sm:m-6 sm:px-5'>
          <ErrorMessage error={logout.error} />
          {tab === 'accounts' && (
            <Accounts
              csrf={props.session.csrf_token}
              onSubmitted={() => setTab('submissions')}
            />
          )}
          {tab === 'submissions' && <History />}
          {tab === 'security' && <Security csrf={props.session.csrf_token} />}
        </main>
      </div>
      <Sheet open={drawer} onOpenChange={setDrawer}>
        <SheetContent
          side='left'
          className='w-56 gap-0'
          showCloseButton={false}
        >
          <div className='flex min-h-16 items-center justify-between gap-2 border-b px-4'>
            <SheetTitle className='min-w-0 text-sm [overflow-wrap:anywhere]'>
              {t('mailboxPortal.title')}
            </SheetTitle>
            <Button
              variant='ghost'
              size='icon'
              title={t('mailboxPortal.close')}
              aria-label={t('mailboxPortal.close')}
              onClick={() => setDrawer(false)}
            >
              <X />
            </Button>
          </div>
          <Navigation tab={tab} onTab={select} />
        </SheetContent>
      </Sheet>
    </div>
  )
}

export function MailboxPortal() {
  const session = useQuery(sessionOptions)
  if (session.data && !session.data.authenticated) {
    return <Navigate to='/mailbox/sign-in' replace />
  }
  return (
    <QueryState
      pending={session.isPending}
      error={session.error}
      retry={() => void session.refetch()}
    >
      {session.data?.authenticated && (
        <Workspace
          key={`${session.data.operator.id}:${session.data.csrf_token}`}
          session={session.data}
        />
      )}
    </QueryState>
  )
}
