/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import {
  Ban,
  CheckCircle2,
  CircleX,
  Clock3,
  LoaderCircle,
  TimerOff,
  type LucideIcon,
} from 'lucide-react'

import type { UsageRecordExportTask } from '@/features/usage-records/api'

type ExportStatusMeta = {
  label: string
  icon: LucideIcon
  badgeClassName: string
  accentClassName: string
}

export const EXPORT_STATUS_META: Record<
  UsageRecordExportTask['status'],
  ExportStatusMeta
> = {
  pending: {
    label: '等待中',
    icon: Clock3,
    badgeClassName:
      'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/70 dark:bg-amber-950/40 dark:text-amber-300',
    accentClassName: 'border-l-amber-400 dark:border-l-amber-500',
  },
  running: {
    label: '导出中',
    icon: LoaderCircle,
    badgeClassName:
      'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/70 dark:bg-blue-950/40 dark:text-blue-300',
    accentClassName: 'border-l-blue-500 dark:border-l-blue-400',
  },
  succeeded: {
    label: '已完成',
    icon: CheckCircle2,
    badgeClassName:
      'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/70 dark:bg-emerald-950/40 dark:text-emerald-300',
    accentClassName: 'border-l-emerald-500 dark:border-l-emerald-400',
  },
  failed: {
    label: '失败',
    icon: CircleX,
    badgeClassName:
      'border-red-200 bg-red-50 text-red-700 dark:border-red-900/70 dark:bg-red-950/40 dark:text-red-300',
    accentClassName: 'border-l-red-500 dark:border-l-red-400',
  },
  cancelled: {
    label: '已取消',
    icon: Ban,
    badgeClassName:
      'border-zinc-200 bg-zinc-100 text-zinc-600 dark:border-zinc-700 dark:bg-zinc-800/70 dark:text-zinc-300',
    accentClassName: 'border-l-zinc-400 dark:border-l-zinc-500',
  },
  expired: {
    label: '文件已过期',
    icon: TimerOff,
    badgeClassName:
      'border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-900/70 dark:bg-orange-950/40 dark:text-orange-300',
    accentClassName: 'border-l-orange-500 dark:border-l-orange-400',
  },
}

export function exportInstanceKindLabel(kind: string) {
  if (kind === 'sub2api') return 'Sub2API'
  if (kind === 'conductor') return 'Conductor'
  return 'New API'
}

export function exportInstanceKindClassName(kind: string) {
  if (kind === 'sub2api') {
    return 'border-cyan-200 bg-cyan-50 text-cyan-700 dark:border-cyan-900/70 dark:bg-cyan-950/40 dark:text-cyan-300'
  }
  if (kind === 'conductor') {
    return 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/70 dark:bg-emerald-950/40 dark:text-emerald-300'
  }
  return 'border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-900/70 dark:bg-violet-950/40 dark:text-violet-300'
}
