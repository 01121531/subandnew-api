import { Check, Search, X } from 'lucide-react'
import { useState, type Ref } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { cn } from '@/lib/utils'

export function UploadChoiceField(props: {
  id: string
  label: string
  value: string
  options: { value: string; label: string; disabled?: boolean }[]
  layout?: 'methods' | 'modes' | 'list'
  emptyLabel?: string
  disabled: boolean
  error?: string
  inputRef?: Ref<HTMLSpanElement>
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const isList = (props.layout ?? 'list') === 'list'
  const canSearch = isList && props.options.length > 6
  const query = canSearch ? search.trim().toLowerCase() : ''
  const matches = props.options.filter((option) =>
    option.label.toLowerCase().includes(query)
  )
  const choices: typeof props.options = props.emptyLabel
    ? [{ value: '', label: props.emptyLabel }, ...matches]
    : matches
  const selected = props.options.find((option) => option.value === props.value)
  const clearSearch = () => setSearch('')
  return (
    <div className='supplier-upload-choices grid min-w-0 gap-2'>
      <span id={`${props.id}-label`} className='text-sm font-medium'>
        {props.label}
      </span>
      {isList && (
        <p
          id={`${props.id}-selected`}
          aria-live='polite'
          className='text-muted-foreground text-xs leading-relaxed [overflow-wrap:anywhere]'
        >
          {t('supplier.ui_choiceSelected', {
            name: selected?.label ?? props.emptyLabel,
          })}
        </p>
      )}
      {canSearch && (
        <div className='relative min-w-0'>
          <Search
            aria-hidden='true'
            className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2'
          />
          <Input
            id={`${props.id}-search`}
            aria-label={t('supplier.ui_choiceSearch', { name: props.label })}
            placeholder={t('supplier.ui_choiceSearch', { name: props.label })}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            disabled={props.disabled}
            autoComplete='off'
            className='pr-10 pl-9'
          />
          {search && (
            <Button
              type='button'
              variant='ghost'
              size='icon-sm'
              className='absolute top-1/2 right-1 -translate-y-1/2'
              aria-label={t('supplier.ui_choiceClear')}
              title={t('supplier.ui_choiceClear')}
              disabled={props.disabled}
              onClick={clearSearch}
            >
              <X aria-hidden='true' className='size-4' />
            </Button>
          )}
        </div>
      )}
      <RadioGroup
        id={props.id}
        value={props.value}
        disabled={props.disabled}
        onValueChange={(value) => props.onChange(value as string)}
        aria-labelledby={`${props.id}-label`}
        aria-describedby={props.error ? `${props.id}-error` : undefined}
        aria-invalid={!!props.error}
        className={cn(
          'min-w-0 auto-rows-max gap-2 p-1',
          isList &&
            'max-h-[200px] grid-cols-1 overflow-y-auto overscroll-contain sm:max-h-[240px] sm:grid-cols-2',
          props.layout === 'methods' && 'grid-cols-2 sm:grid-cols-4',
          props.layout === 'modes' && 'grid-cols-2 sm:grid-cols-3'
        )}
      >
        {choices.map((option, index) => (
          <label
            key={option.value}
            htmlFor={`${props.id}-option-${index}`}
            className='supplier-upload-choice'
            data-selected={option.value === props.value}
            data-disabled={props.disabled || option.disabled}
          >
            <RadioGroupItem
              id={`${props.id}-option-${index}`}
              ref={index === 0 ? props.inputRef : undefined}
              value={option.value}
              disabled={props.disabled || option.disabled}
              aria-labelledby={`${props.id}-text-${index}`}
              aria-describedby={props.error ? `${props.id}-error` : undefined}
              aria-invalid={!!props.error}
              className='supplier-upload-choice-radio'
            />
            <span
              id={`${props.id}-text-${index}`}
              className='min-w-0 flex-1 [overflow-wrap:anywhere]'
            >
              {option.label}
            </span>
            <Check
              aria-hidden='true'
              className={cn(
                'text-primary size-4 shrink-0',
                option.value !== props.value && 'invisible'
              )}
            />
          </label>
        ))}
      </RadioGroup>
      {query && matches.length === 0 && (
        <div
          role='status'
          className='flex flex-wrap items-center gap-2 text-sm'
        >
          <span className='text-muted-foreground'>
            {t('supplier.ui_choiceNoResults')}
          </span>
          <Button
            type='button'
            variant='link'
            size='sm'
            disabled={props.disabled}
            onClick={clearSearch}
          >
            {t('supplier.ui_choiceClear')}
          </Button>
        </div>
      )}
      {props.error && (
        <p
          id={`${props.id}-error`}
          role='alert'
          className='text-destructive text-xs'
        >
          {props.error}
        </p>
      )}
    </div>
  )
}
