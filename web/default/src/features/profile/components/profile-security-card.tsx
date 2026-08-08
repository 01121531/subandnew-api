import { Shield } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import { useDialogs } from '@/hooks/use-dialog'

import type { UserProfile } from '../types'
import { ChangePasswordDialog } from './dialogs/change-password-dialog'

export function ProfileSecurityCard({
  profile,
  loading,
}: {
  profile: UserProfile | null
  loading: boolean
}) {
  const { t } = useTranslation()
  const dialogs = useDialogs<'password'>()
  if (loading)
    return (
      <Card data-card-hover='false'>
        <CardContent className='p-5'>
          <Skeleton className='h-16 w-full' />
        </CardContent>
      </Card>
    )
  if (!profile) return null
  return (
    <>
      <TitledCard
        title={t('Security')}
        description={t('Manage your control-plane password')}
        icon={<Shield className='h-4 w-4' />}
        iconTone='success'
        disableHoverEffect
      >
        <button
          type='button'
          onClick={() => dialogs.open('password')}
          className='flex w-full items-center gap-3 rounded-md border p-3 text-left'
        >
          <IconBadge tone='neutral' size='sm'>
            <Shield />
          </IconBadge>
          <div>
            <p className='text-sm font-medium'>{t('Change Password')}</p>
            <p className='text-muted-foreground text-xs'>
              {t('Update your password to keep your account secure')}
            </p>
          </div>
        </button>
      </TitledCard>
      <ChangePasswordDialog
        open={dialogs.isOpen('password')}
        onOpenChange={(open) =>
          open ? dialogs.open('password') : dialogs.close('password')
        }
        username={profile.username}
      />
    </>
  )
}
