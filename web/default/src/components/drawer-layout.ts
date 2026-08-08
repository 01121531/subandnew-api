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
import { createElement, type ReactNode } from 'react'

import { cn } from '@/lib/utils'

export const sideDrawerContentClassName = (className?: string) =>
  cn(
    'bg-background text-foreground flex h-dvh w-full flex-col gap-0 overflow-hidden p-0 shadow-none',
    className
  )

export const sideDrawerHeaderClassName = (className?: string) =>
  cn(
    'border-border/70 bg-background/95 border-b px-4 py-3 text-start backdrop-blur supports-[backdrop-filter]:bg-background/80 sm:px-6 sm:py-4',
    className
  )

export const sideDrawerFormClassName = (className?: string) =>
  cn(
    'flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto overscroll-contain px-4 py-4 sm:px-6 sm:py-5',
    className
  )

export const sideDrawerFooterClassName = (className?: string) =>
  cn(
    'border-border/70 bg-background/95 grid grid-cols-2 gap-2 border-t px-4 py-3 backdrop-blur supports-[backdrop-filter]:bg-background/80 sm:flex sm:flex-row sm:justify-end sm:px-6 sm:py-4',
    className
  )

const sideDrawerSectionClassName = (className?: string) =>
  cn(
    'border-border/60 flex flex-col gap-4 border-b pb-6 last:border-b-0 last:pb-0',
    className
  )

export function SideDrawerSection(props: {
  children: ReactNode
  className?: string
}) {
  return createElement(
    'section',
    { className: sideDrawerSectionClassName(props.className) },
    props.children
  )
}
