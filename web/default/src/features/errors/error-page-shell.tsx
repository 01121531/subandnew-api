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
import type { ReactNode } from 'react'

import { BrandWordmark } from '@/components/brand-wordmark'
import { PageTransition } from '@/components/page-transition'
import { ThemeSwitch } from '@/components/theme-switch'
import { useSystemConfig } from '@/hooks/use-system-config'

interface ErrorPageShellProps {
  status: number
  title: string
  description: ReactNode
  actions?: ReactNode
  supportingText?: ReactNode
}

export function ErrorPageShell(props: ErrorPageShellProps) {
  const { systemName, logo } = useSystemConfig()
  const displayName = systemName || 'SubAndNew API'

  return (
    <div className='bg-background text-foreground relative isolate min-h-svh overflow-hidden'>
      <div
        aria-hidden='true'
        className='absolute inset-0 -z-20 bg-[linear-gradient(to_right,var(--border)_1px,transparent_1px),linear-gradient(to_bottom,var(--border)_1px,transparent_1px)] [mask-image:radial-gradient(circle_at_center,black,transparent_80%)] bg-[size:54px_54px] opacity-[0.15]'
      />
      <div
        aria-hidden='true'
        className='absolute top-[-16rem] left-1/2 -z-20 h-[42rem] w-[64rem] max-w-none -translate-x-1/2 rounded-full bg-[radial-gradient(circle,rgba(124,58,237,0.22),rgba(79,70,229,0.10)_40%,transparent_72%)] blur-3xl'
      />

      <header className='absolute inset-x-0 top-0 z-10 flex h-18 items-center justify-between px-5 sm:px-8 lg:px-12'>
        <Link
          to='/'
          className='group flex items-center gap-2.5 transition-opacity hover:opacity-80'
        >
          <img src={logo} alt='' className='size-8 rounded-xl object-contain' />
          <BrandWordmark name={displayName} className='text-sm' />
        </Link>
        <ThemeSwitch />
      </header>

      <main className='flex min-h-svh items-center justify-center px-5 py-24'>
        <PageTransition className='w-full max-w-2xl text-center'>
          <div className='relative mx-auto mb-7 flex size-20 items-center justify-center'>
            <span className='absolute inset-0 animate-pulse rounded-3xl bg-violet-500/12 motion-reduce:animate-none' />
            <span className='bg-background/70 absolute inset-2 rounded-2xl border border-violet-500/20 backdrop-blur-xl' />
            <span className='relative bg-gradient-to-br from-indigo-500 via-violet-500 to-fuchsia-500 bg-clip-text text-2xl font-bold text-transparent'>
              {props.status}
            </span>
          </div>

          <p className='text-sm font-semibold tracking-[0.24em] text-violet-600 uppercase dark:text-violet-300'>
            SYSTEM STATUS
          </p>
          <h1 className='mt-4 text-3xl font-bold tracking-[-0.03em] sm:text-5xl'>
            {props.title}
          </h1>
          <div className='text-muted-foreground mx-auto mt-5 max-w-xl text-sm leading-7 sm:text-base'>
            {props.description}
          </div>
          {props.supportingText && (
            <div className='text-muted-foreground/70 mx-auto mt-3 max-w-xl text-sm'>
              {props.supportingText}
            </div>
          )}
          {props.actions && (
            <div className='mt-8 flex flex-col justify-center gap-3 sm:flex-row'>
              {props.actions}
            </div>
          )}
        </PageTransition>
      </main>
    </div>
  )
}
