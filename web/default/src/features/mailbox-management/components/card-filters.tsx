import { Filter, RotateCcw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

import type { CardFilter } from '../types'

const operators = [
  'starts_with',
  'not_starts_with',
  'ends_with',
  'not_ends_with',
] as const
export function CardFilters(props: {
  onApply: (filters: CardFilter[]) => void
}) {
  const { t } = useTranslation()
  const [values, setValues] = useState<Record<string, string>>({})
  const [invalid, setInvalid] = useState(false)
  function apply() {
    const filters: CardFilter[] = []
    for (const operator of operators) {
      const entries = [
        ...new Set(
          (values[operator] ?? '')
            .split(/[,，\r\n]/)
            .map((v) => v.trim())
            .filter(Boolean)
        ),
      ]
      if (entries.some((v) => !/^\d{1,19}$/.test(v)) || entries.length > 1000) {
        setInvalid(true)
        return
      }
      if (entries.length) filters.push({ operator, values: entries })
    }
    setInvalid(false)
    props.onApply(filters)
  }
  return (
    <fieldset className='my-3 min-w-0 space-y-3 border-y py-3'>
      <legend className='text-sm font-medium'>
        {t('mailbox.cardFilters.title')}
      </legend>
      <div className='grid min-w-0 gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        {operators.map((operator) => (
          <label key={operator} className='min-w-0 space-y-1 text-sm'>
            <span>{t(`mailbox.cardFilters.${operator}`)}</span>
            <Textarea
              aria-label={t(`mailbox.cardFilters.${operator}`)}
              value={values[operator] ?? ''}
              inputMode='numeric'
              autoComplete='off'
              placeholder={t('mailbox.cardFilters.values')}
              className='min-h-16'
              onChange={(e) =>
                setValues({ ...values, [operator]: e.target.value })
              }
            />
          </label>
        ))}
      </div>
      <div className='flex flex-wrap gap-2'>
        <Button variant='outline' onClick={apply}>
          <Filter />
          {t('mailbox.cardFilters.apply')}
        </Button>
        <Button
          variant='ghost'
          onClick={() => {
            setValues({})
            setInvalid(false)
            props.onApply([])
          }}
        >
          <RotateCcw />
          {t('mailbox.cardFilters.reset')}
        </Button>
      </div>
      {invalid && (
        <p role='alert' className='text-destructive text-sm'>
          {t('mailbox.errors.mailbox_invalid_card_filter')}
        </p>
      )}
    </fieldset>
  )
}
