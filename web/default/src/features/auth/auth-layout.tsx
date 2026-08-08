/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { BrandWordmark } from '@/components/brand-wordmark'
import { LanguageSwitcher } from '@/components/language-switcher'
import { PageTransition } from '@/components/page-transition'
import { ThemeSwitch } from '@/components/theme-switch'
import { Skeleton } from '@/components/ui/skeleton'
import { useSystemConfig } from '@/hooks/use-system-config'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  const { t } = useTranslation()
  const { systemName, logo, loading } = useSystemConfig()
  const displayName = systemName || 'SubAndNew API'

  return (
    <div className='bg-background text-foreground relative grid min-h-svh overflow-x-hidden lg:grid-cols-[minmax(0,0.9fr)_minmax(32rem,1.1fr)]'>
      <aside className='border-border bg-muted/30 text-foreground relative hidden min-h-svh overflow-hidden border-r lg:flex lg:flex-col'>
        <div
          aria-hidden='true'
          className='absolute inset-0 bg-[linear-gradient(to_right,var(--border)_1px,transparent_1px),linear-gradient(to_bottom,var(--border)_1px,transparent_1px)] bg-[size:52px_52px] opacity-30'
        />

        <Link
          to='/'
          className='relative z-10 flex items-center gap-3 px-10 py-8 transition-opacity hover:opacity-80'
        >
          <img src={logo} alt={t('Logo')} className='size-9 rounded-md' />
          <BrandWordmark
            name={displayName}
            className='text-foreground text-base dark:text-white'
          />
        </Link>

        <div className='relative z-10 my-auto px-10 pb-20 xl:px-16'>
          <p className='text-muted-foreground text-sm font-semibold uppercase'>
            CONTROL PLANE
          </p>
          <h2 className='mt-5 max-w-xl text-4xl leading-tight font-bold xl:text-5xl'>
            {displayName}
          </h2>
        </div>

        <p className='text-muted-foreground relative z-10 px-10 py-7 text-xs xl:px-16'>
          © {new Date().getFullYear()} {displayName}
        </p>
      </aside>

      <section className='relative flex min-h-svh flex-col'>
        <header className='flex h-18 shrink-0 items-center justify-between px-5 sm:px-8 lg:justify-end'>
          <Link
            to='/'
            className='flex items-center gap-2.5 transition-opacity hover:opacity-80 lg:hidden'
          >
            {loading ? (
              <>
                <Skeleton className='size-8 rounded-xl' />
                <Skeleton className='h-5 w-24' />
              </>
            ) : (
              <>
                <img
                  src={logo}
                  alt={t('Logo')}
                  className='size-8 rounded-md object-cover'
                />
                <BrandWordmark name={displayName} className='text-sm' />
              </>
            )}
          </Link>
          <div className='flex items-center gap-1'>
            <LanguageSwitcher />
            <ThemeSwitch />
          </div>
        </header>

        <main className='flex flex-1 items-center justify-center px-5 py-8 sm:px-8 sm:py-12'>
          <PageTransition className='auth-form-card border-border bg-background w-full max-w-[440px] rounded-lg border p-6 shadow-sm sm:p-8'>
            {children}
          </PageTransition>
        </main>

        <div className='h-4 shrink-0' />
      </section>
    </div>
  )
}
