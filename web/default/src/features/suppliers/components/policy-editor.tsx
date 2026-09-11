import { RotateCcw } from 'lucide-react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { NativeSelectOption } from '@/components/ui/native-select'

import { applyOverrides, policyGroups } from '../lib/permissions'
import type { EffectivePolicy, PolicyOverrides } from '../types'
import { SelectField } from './common'

export function PolicyEditor(props: {
  value: PolicyOverrides
  parent: EffectivePolicy
  level: 'global' | 'supplier' | 'binding'
  disabled?: boolean
  onChange: (value: PolicyOverrides) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const effective =
    props.level === 'global'
      ? props.parent
      : applyOverrides(props.parent, props.value, props.level)
  return (
    <div className='grid min-w-0 gap-5'>
      {props.level !== 'global' && (
        <Button
          type='button'
          variant='outline'
          disabled={props.disabled}
          className='justify-self-start'
          onClick={() => props.onChange({})}
        >
          <RotateCcw />
          {t('supplier.policyReset')}
        </Button>
      )}
      {policyGroups.map((group) => (
        <fieldset key={group.name} className='grid min-w-0 gap-2 border-t pt-3'>
          <legend className='text-sm font-semibold'>
            {t(`supplier.policyGroup_${group.name}`)}
          </legend>
          {group.keys.map((key) => {
            const value = props.value[key]
            const state = value == null ? 'inherit' : String(value)
            const label = t(`supplier.policyField_${key.replaceAll('.', '_')}`)
            return (
              <div
                key={key}
                className='bg-muted/30 grid min-w-0 gap-2 rounded-md p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]'
              >
                {props.level === 'global' ? (
                  <label className='flex min-h-10 items-center gap-2 text-sm'>
                    <input
                      type='checkbox'
                      checked={value === true}
                      disabled={props.disabled}
                      onChange={(e) =>
                        props.onChange({
                          ...props.value,
                          [key]: e.target.checked,
                        })
                      }
                    />
                    {label}
                  </label>
                ) : (
                  <SelectField
                    id={`${id}-${key}`}
                    label={label}
                    value={state}
                    disabled={props.disabled}
                    onChange={(next) =>
                      props.onChange({
                        ...props.value,
                        [key]: next === 'inherit' ? null : next === 'true',
                      })
                    }
                  >
                    <NativeSelectOption value='inherit'>
                      {t('supplier.policyInherit')}
                    </NativeSelectOption>
                    <NativeSelectOption value='true'>
                      {t('supplier.policyAllow')}
                    </NativeSelectOption>
                    <NativeSelectOption value='false'>
                      {t('supplier.policyDeny')}
                    </NativeSelectOption>
                  </SelectField>
                )}
                {props.level !== 'global' && (
                  <p
                    className='text-muted-foreground self-center text-xs leading-relaxed'
                    aria-live='polite'
                  >
                    {t('supplier.policyEffective', {
                      value: t(
                        effective.values[key]
                          ? 'supplier.policyAllow'
                          : 'supplier.policyDeny'
                      ),
                      source: t(
                        `supplier.policySource_${effective.sources[key]}`
                      ),
                    })}
                  </p>
                )}
              </div>
            )
          })}
        </fieldset>
      ))}
    </div>
  )
}
