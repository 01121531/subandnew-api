import { CheckCircle2, CircleHelp, CopyCheck, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { AccountImportResult } from '../types'

const resultStyles = {
  ok: { icon: CheckCircle2, color: 'text-emerald-700 dark:text-emerald-400' },
  duplicate: { icon: CopyCheck, color: 'text-muted-foreground' },
  failed: { icon: XCircle, color: 'text-red-700 dark:text-red-400' },
  unknown: { icon: CircleHelp, color: 'text-amber-700 dark:text-amber-400' },
}

export function ImportResultView({ result }: { result: AccountImportResult }) {
  const { t } = useTranslation()
  return (
    <div className='grid min-w-0 gap-5'>
      <h2 className='text-lg font-semibold'>
        {t('supplier.accountImportResult')}
      </h2>
      {(result.resolved_name || result.resolved_name_prefix) && (
        <dl className='grid min-w-0 gap-1 text-sm'>
          <dt className='text-muted-foreground'>
            {t(
              result.resolved_name_prefix
                ? 'supplier.namingBatchPreview'
                : 'supplier.resolvedName'
            )}
          </dt>
          <dd className='font-mono [overflow-wrap:anywhere]'>
            {result.resolved_name_prefix ?? result.resolved_name}
          </dd>
        </dl>
      )}
      <dl className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
        {(['ok', 'duplicate', 'failed', 'unknown'] as const).map((key) => {
          const Icon = resultStyles[key].icon
          return (
            <div
              key={key}
              data-import-result={key}
              className='min-w-0 rounded-lg p-3'
            >
              <dt
                className={`flex items-center gap-2 text-xs ${resultStyles[key].color}`}
              >
                <Icon className='size-4' aria-hidden='true' />
                {t(`supplier.importCount_${key}`)}
              </dt>
              <dd className='mt-1 text-xl font-semibold tabular-nums'>
                {result[key]}
              </dd>
            </div>
          )
        })}
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
