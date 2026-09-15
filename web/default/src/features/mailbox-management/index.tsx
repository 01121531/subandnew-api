import { ExternalLink } from 'lucide-react'
import { lazy, Suspense, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { buttonVariants } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { adminDataAuthorizationKey } from '@/lib/admin-data-policy'
import { useAuthStore } from '@/stores/auth-store'

import { mailboxTabs } from './lib/permissions'

const Accounts = lazy(() =>
  import('./components/accounts').then((module) => ({
    default: module.Accounts,
  }))
)
const Operators = lazy(() =>
  import('./components/operators').then((module) => ({
    default: module.Operators,
  }))
)
const Reviews = lazy(() =>
  import('./components/reviews').then((module) => ({ default: module.Reviews }))
)
const Audits = lazy(() =>
  import('./components/audits').then((module) => ({ default: module.Audits }))
)
const Issues = lazy(() =>
  import('./components/issues').then((module) => ({ default: module.Issues }))
)

export function MailboxManagement() {
  const user = useAuthStore((state) => state.auth.user)
  return <MailboxPage key={adminDataAuthorizationKey(user)} />
}
function MailboxPage() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const tabs = mailboxTabs(user)
  const [active, setActive] = useState(tabs[0] ?? '')
  if (!tabs.length) {
    return (
      <p role='alert' className='p-6'>
        {t('mailbox.errors.mailbox_permission_denied')}
      </p>
    )
  }
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('mailbox.admin.title')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions className='w-full justify-start sm:w-auto sm:justify-end'>
        <a
          href='/mailbox/sign-in'
          target='_blank'
          rel='noopener noreferrer'
          className={buttonVariants({ variant: 'outline' })}
        >
          <ExternalLink className='size-4' aria-hidden='true' />
          {t('mailbox.admin.portalEntry')}
        </a>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <Tabs
          value={active}
          onValueChange={(value) => setActive(String(value))}
          className='min-w-0'
        >
          <div className='overflow-x-auto border-b'>
            <TabsList variant='line' className='min-h-10'>
              {tabs.map((tab) => (
                <TabsTrigger key={tab} value={tab} className='px-3'>
                  {t(`mailbox.admin.tabs.${tab}`)}
                </TabsTrigger>
              ))}
            </TabsList>
          </div>
          <Suspense
            fallback={
              <p role='status' className='py-12 text-center'>
                {t('mailbox.admin.loading')}
              </p>
            }
          >
            {tabs.map((tab) => (
              <TabsContent key={tab} value={tab} className='min-w-0'>
                {active === tab && (
                  <>
                    {tab === 'pool' && <Accounts />}
                    {tab === 'operators' && <Operators />}
                    {tab === 'review' && <Reviews />}
                    {tab === 'issues' && <Issues />}
                    {tab === 'audit' && <Audits />}
                  </>
                )}
              </TabsContent>
            ))}
          </Suspense>
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
