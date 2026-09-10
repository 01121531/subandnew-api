import { useTranslation } from 'react-i18next'

import type { AccountImportResult } from '../types'

export function ImportResultView({ result }: { result: AccountImportResult }) {
  const { t } = useTranslation()
  return (
    <div className='grid min-w-0 gap-5'>
      <h2 className='text-lg font-semibold'>
        {t('supplier.accountImportResult')}
      </h2>
      <dl className='grid grid-cols-2 gap-4 border-y py-4 sm:grid-cols-4'>
        {(['ok', 'duplicate', 'failed', 'unknown'] as const).map((key) => (
          <div key={key}>
            <dt className='text-muted-foreground text-xs'>
              {t(`supplier.importCount_${key}`)}
            </dt>
            <dd className='mt-1 text-xl font-semibold tabular-nums'>
              {result[key]}
            </dd>
          </div>
        ))}
      </dl>
      {result.unknown > 0 && (
        <p role='status' className='text-sm text-amber-700 dark:text-amber-400'>
          {t('supplier.importCheckBeforeRetry')}
        </p>
      )}
      <ul className='divide-y'>
        {result.results.map((row) => (
          <li key={row.index} className='flex items-start gap-4 py-3 text-sm'>
            <span className='text-muted-foreground w-8 shrink-0 tabular-nums'>
              #{row.index + 1}
            </span>
            <span className='min-w-0 break-words'>
              {t(`supplier.importStatus_${row.status}`, {
                defaultValue: t('supplier.importStatus_unknown'),
              })}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}
