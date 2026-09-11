import { useTranslation } from 'react-i18next'

import { emptyNaming } from '../lib/naming'
import type { EffectiveNaming } from '../types'

export function NamingSummary({ rule }: { rule?: EffectiveNaming }) {
  const { t } = useTranslation()
  const value = rule ?? emptyNaming
  return (
    <dl className='my-3 grid min-w-0 gap-1 text-xs font-normal [overflow-wrap:anywhere]'>
      <dt className='text-muted-foreground'>
        {t('supplier.namingTitle')}
        {rule &&
          ` · ${t(rule.source === 'binding' ? 'supplier.namingCustom' : 'supplier.namingInherit')}`}
      </dt>
      {(['prefix', 'suffix'] as const).map((part) => (
        <div key={part} className='flex flex-wrap gap-x-2'>
          <dt>{t(`supplier.naming_${part}`)}</dt>
          <dd className='min-w-0 font-mono'>
            {value[part] || t('supplier.none')}
          </dd>
        </div>
      ))}
    </dl>
  )
}
