import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import type { ManagedInstanceStatus } from '../types'

const statusClassNames: Record<ManagedInstanceStatus, string> = {
  healthy:
    'border-emerald-600/20 bg-emerald-600/10 text-emerald-700 dark:text-emerald-400',
  degraded:
    'border-amber-600/20 bg-amber-600/10 text-amber-700 dark:text-amber-400',
  offline: 'border-red-600/20 bg-red-600/10 text-red-700 dark:text-red-400',
  auth_failed:
    'border-fuchsia-600/20 bg-fuchsia-600/10 text-fuchsia-700 dark:text-fuchsia-400',
  unknown: 'border-border bg-muted text-muted-foreground',
}

export function StatusBadge(props: { status: ManagedInstanceStatus }) {
  const { t } = useTranslation()
  return (
    <Badge
      variant='outline'
      className={cn('capitalize', statusClassNames[props.status])}
    >
      {t(
        props.status
          .split('_')
          .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
          .join(' ')
      )}
    </Badge>
  )
}
