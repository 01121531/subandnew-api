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
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { getUserAvatarFallback, getUserAvatarStyle } from '@/lib/avatar'
import { getRoleLabel } from '@/lib/roles'

import type { UserProfile } from '../types'

export function ProfileHeader({
  profile,
  loading,
}: {
  profile: UserProfile | null
  loading: boolean
}) {
  const { t } = useTranslation()
  if (loading) {
    return (
      <Card data-card-hover='false'>
        <CardContent className='flex gap-4 p-5'>
          <Skeleton className='h-16 w-16 rounded-xl' />
          <div className='space-y-2'>
            <Skeleton className='h-7 w-48' />
            <Skeleton className='h-4 w-64' />
          </div>
        </CardContent>
      </Card>
    )
  }
  if (!profile) return null
  const name = profile.display_name || profile.username
  return (
    <Card data-card-hover='false'>
      <CardContent className='flex items-center gap-4 p-5'>
        <Avatar className='h-16 w-16 rounded-xl'>
          <AvatarFallback
            className='rounded-xl font-semibold text-white'
            style={getUserAvatarStyle(name)}
          >
            {getUserAvatarFallback(name)}
          </AvatarFallback>
        </Avatar>
        <div className='min-w-0 space-y-2'>
          <div className='flex flex-wrap items-center gap-2'>
            <h1 className='truncate text-2xl font-semibold'>{name}</h1>
            <StatusBadge
              label={getRoleLabel(profile.role)}
              variant='neutral'
              copyable={false}
            />
            <StatusBadge
              label={`${t('User ID')} ${profile.id}`}
              variant='info'
              copyText={String(profile.id)}
            />
          </div>
          <div className='text-muted-foreground flex flex-wrap gap-3 text-sm'>
            <span>@{profile.username}</span>
            {profile.email && <span>{profile.email}</span>}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
