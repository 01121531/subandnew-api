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
import { useNavigate, useRouter } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import { ErrorPageShell } from './error-page-shell'

const FEEDBACK_URL = 'https://github.com/01121531/subandnew-api/issues'

type GeneralErrorProps = React.HTMLAttributes<HTMLDivElement> & {
  minimal?: boolean
  error?: unknown
}

function getHttpStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  const response = (error as Record<string, unknown>).response
  if (typeof response !== 'object' || response === null) return undefined
  const status = (response as Record<string, unknown>).status
  return typeof status === 'number' ? status : undefined
}

export function GeneralError({
  className,
  minimal = false,
  error,
}: GeneralErrorProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  const status = getHttpStatus(error)
  const isRateLimited = status === 429
  const title = isRateLimited
    ? t('Too many requests')
    : `${t('Oops! Something went wrong')} ${`:')`}`
  const description = isRateLimited
    ? t('Please wait a moment before trying again.')
    : t('Please try again later.')

  if (minimal) {
    return (
      <div
        className={cn(
          'border-border/60 bg-background/80 flex min-h-64 w-full flex-col items-center justify-center rounded-3xl border px-6 py-12 text-center shadow-sm backdrop-blur-sm',
          className
        )}
      >
        <span className='text-lg font-semibold'>{title}</span>
        <p className='text-muted-foreground mt-2 text-sm leading-6'>
          {t('We apologize for the inconvenience.')} {description}
        </p>
      </div>
    )
  }

  return (
    <div className={className}>
      <ErrorPageShell
        status={status ?? 500}
        title={title}
        description={
          <>
            {t('We apologize for the inconvenience.')} <br /> {description}
          </>
        }
        supportingText={
          <>
            {t('If this keeps happening, please report it on GitHub Issues.')}
          </>
        }
        actions={
          <>
            <Button variant='outline' onClick={() => history.go(-1)}>
              {t('Go Back')}
            </Button>
            <Button
              variant='outline'
              render={
                <a
                  href={FEEDBACK_URL}
                  target='_blank'
                  rel='noopener noreferrer'
                />
              }
            >
              {t('Report an issue')}
            </Button>
            <Button
              className='bg-gradient-to-r from-indigo-600 to-violet-600 text-white shadow-lg shadow-violet-500/20 hover:opacity-90'
              onClick={() => navigate({ to: '/' })}
            >
              {t('Back to Home')}
            </Button>
          </>
        }
      />
    </div>
  )
}
