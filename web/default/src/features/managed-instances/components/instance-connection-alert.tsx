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
import { RefreshCw, ServerOff } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

export function InstanceConnectionAlert(props: {
  onRetry: () => void
  retrying?: boolean
}) {
  const { t } = useTranslation()
  return (
    <div
      className='border-destructive/35 bg-destructive/5 text-destructive flex flex-wrap items-center justify-between gap-3 rounded-lg border px-3 py-2.5'
      role='alert'
    >
      <div className='flex min-w-0 items-start gap-2.5'>
        <ServerOff className='mt-0.5 size-4 shrink-0' aria-hidden='true' />
        <div className='min-w-0'>
          <div className='text-sm font-medium'>
            {t('Instance connection failed', {
              defaultValue: '实例连接失败',
            })}
          </div>
          <div className='text-muted-foreground mt-0.5 text-xs'>
            {t(
              'The instance was rechecked but is still unreachable. Automatic checks will resume in one hour.',
              {
                defaultValue:
                  '已重新检测，但实例仍无法连接。后台将在 1 小时后再次自动检测。',
              }
            )}
          </div>
        </div>
      </div>
      <Button
        variant='outline'
        size='sm'
        className='bg-background h-11 w-full md:h-8 md:w-auto'
        onClick={props.onRetry}
        disabled={props.retrying}
      >
        <RefreshCw className={props.retrying ? 'animate-spin' : ''} />
        {t('Retry')}
      </Button>
    </div>
  )
}
