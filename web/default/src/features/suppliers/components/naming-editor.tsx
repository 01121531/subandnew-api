import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'

import { emptyNaming, uploadName, validNamingRule } from '../lib/naming'
import type { NamingRule } from '../types'
import { Field } from './common'

export function NamingEditor(props: {
  id: string
  value: NamingRule | null
  parent?: NamingRule
  disabled?: boolean
  onChange: (value: NamingRule | null) => void
}) {
  const { t } = useTranslation()
  const inherited = props.parent !== undefined && props.value === null
  const effective = props.value ?? props.parent ?? emptyNaming
  const error = !validNamingRule(props.value)
  return (
    <fieldset
      className='grid min-w-0 gap-3 border-t pt-4'
      disabled={props.disabled}
    >
      <legend className='px-1 text-sm font-semibold'>
        {t('supplier.namingTitle')}
      </legend>
      {props.parent && (
        <RadioGroup
          aria-label={t('supplier.namingSource')}
          value={inherited ? 'inherit' : 'custom'}
          disabled={props.disabled}
          onValueChange={(value) =>
            props.onChange(value === 'inherit' ? null : { ...effective })
          }
          className='flex flex-wrap gap-4'
        >
          <label className='flex items-center gap-2 text-sm'>
            <RadioGroupItem value='inherit' />
            {t('supplier.namingInherit')}
          </label>
          <label className='flex items-center gap-2 text-sm'>
            <RadioGroupItem value='custom' />
            {t('supplier.namingCustom')}
          </label>
        </RadioGroup>
      )}
      <div className='grid min-w-0 gap-3 sm:grid-cols-2'>
        {(['prefix', 'suffix'] as const).map((part) => (
          <Field
            key={part}
            id={`${props.id}-${part}`}
            label={t(`supplier.naming_${part}`)}
          >
            <Input
              id={`${props.id}-${part}`}
              value={effective[part]}
              disabled={props.disabled || inherited}
              aria-invalid={error}
              autoComplete='off'
              onChange={(event) =>
                props.onChange({ ...effective, [part]: event.target.value })
              }
            />
          </Field>
        ))}
      </div>
      {error && (
        <p role='alert' className='text-destructive text-xs'>
          {t('supplier.namingInvalidRule')}
        </p>
      )}
      <div className='bg-muted/50 grid min-w-0 gap-1 rounded-md p-3 text-sm'>
        <span className='text-muted-foreground text-xs'>
          {t('supplier.namingPreview')}
        </span>
        <output className='font-mono [overflow-wrap:anywhere]'>
          {uploadName(effective, t('supplier.namingExample'))}
        </output>
      </div>
    </fieldset>
  )
}
