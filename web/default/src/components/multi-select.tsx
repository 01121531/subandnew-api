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
import { Add01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import * as React from 'react'

import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxCollection,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  ComboboxValue,
  useComboboxAnchor,
} from '@/components/ui/combobox'
import { cn } from '@/lib/utils'

export type MultiSelectOption = {
  label: string
  value: string
}

type MultiSelectProps = {
  options: MultiSelectOption[]
  selected: string[]
  onChange: (values: string[]) => void
  placeholder?: string
  className?: string
  allowCreate?: boolean
  id?: string
  disabled?: boolean
  maxVisibleChips?: number
  maxValues?: number
  onLimitExceeded?: (maximum: number) => void
}

const VALUE_SEPARATOR = /[,，\n]/

function splitAllValues(value: string) {
  const seen = new Set<string>()
  return value
    .split(/[,，\n]+/)
    .map((part) => part.trim())
    .filter((part) => {
      const key = part.toLocaleLowerCase()
      if (!part || seen.has(key)) return false
      seen.add(key)
      return true
    })
}

function splitDraft(value: string) {
  if (!VALUE_SEPARATOR.test(value)) return { completed: [], draft: value }
  const parts = value.replaceAll('，', ',').replaceAll('\n', ',').split(',')
  return {
    completed: parts
      .slice(0, -1)
      .map((part) => part.trim())
      .filter(Boolean),
    draft: parts.at(-1) ?? '',
  }
}

export function MultiSelect({
  options,
  selected,
  onChange,
  placeholder = '请选择或输入',
  className,
  allowCreate = true,
  id,
  disabled,
  maxVisibleChips = 2,
  maxValues,
  onLimitExceeded,
}: MultiSelectProps) {
  const anchorRef = useComboboxAnchor()
  const [inputValue, setInputValue] = React.useState('')
  const [open, setOpen] = React.useState(false)
  const selectedSet = React.useMemo(() => new Set(selected), [selected])
  const labels = React.useMemo(
    () => new Map(options.map((option) => [option.value, option.label])),
    [options]
  )
  const trimmedInput = inputValue.trim()
  const inputMatchesExisting =
    trimmedInput.length > 0 &&
    (selectedSet.has(trimmedInput) ||
      options.some(
        (option) =>
          option.value === trimmedInput || option.label === trimmedInput
      ))
  const canCreate =
    allowCreate && trimmedInput.length > 0 && !inputMatchesExisting
  const items = React.useMemo(() => {
    const values = new Set(options.map((option) => option.value))
    selected.forEach((value) => values.add(value))
    if (canCreate) values.add(trimmedInput)
    return [...values]
  }, [canCreate, options, selected, trimmedInput])

  const addValues = React.useCallback(
    (values: string[]) => {
      const next = [...selected]
      const seen = new Set(selected.map((value) => value.toLocaleLowerCase()))
      values.forEach((raw) => {
        const value = raw.trim()
        const key = value.toLocaleLowerCase()
        if (value && !seen.has(key)) {
          seen.add(key)
          next.push(value)
        }
      })
      if (maxValues != null && next.length > maxValues) {
        onLimitExceeded?.(maxValues)
        return
      }
      if (next.length !== selected.length) onChange(next)
    },
    [maxValues, onChange, onLimitExceeded, selected]
  )

  return (
    <Combobox
      multiple
      name={id}
      items={items}
      value={selected}
      onValueChange={(values) => {
        if (maxValues != null && values.length > maxValues) {
          onLimitExceeded?.(maxValues)
          return
        }
        onChange(values)
        if (values.length > selected.length) setInputValue('')
      }}
      inputValue={inputValue}
      onInputValueChange={(value) => {
        if (!allowCreate) {
          setInputValue(value)
          return
        }
        const parsed = splitDraft(value)
        if (parsed.completed.length > 0) addValues(parsed.completed)
        setInputValue(parsed.draft)
      }}
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
    >
      <ComboboxChips
        ref={anchorRef}
        className={cn('w-full min-w-0 max-w-full overflow-hidden', className)}
      >
        <ComboboxValue>
          {(values: string[]) => (
            <>
              {values.slice(0, maxVisibleChips).map((value) => (
                <ComboboxChip key={value}>
                  <span className='max-w-28 truncate'>
                    {labels.get(value) ?? value}
                  </span>
                </ComboboxChip>
              ))}
              {values.length > maxVisibleChips && (
                <span className='bg-muted text-muted-foreground flex h-5 items-center rounded-sm px-1.5 text-xs'>
                  +{values.length - maxVisibleChips}
                </span>
              )}
            </>
          )}
        </ComboboxValue>
        <ComboboxChipsInput
          id={id}
          placeholder={selected.length === 0 ? placeholder : undefined}
          aria-label={placeholder}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && canCreate) {
              const popup = document.querySelector<HTMLElement>(
                '[data-slot="combobox-content"][data-open]'
              )
              if (!popup?.querySelector('[data-highlighted]')) {
                event.preventDefault()
                addValues([trimmedInput])
                setInputValue('')
              }
            }
          }}
          onPaste={(event) => {
            if (!allowCreate) return
            const pasted = event.clipboardData.getData('text')
            if (!VALUE_SEPARATOR.test(pasted)) return
            event.preventDefault()
            addValues(splitAllValues(pasted))
            setInputValue('')
          }}
        />
      </ComboboxChips>
      <ComboboxContent anchor={anchorRef}>
        <ComboboxList>
          <ComboboxCollection>
            {(item: string) => {
              const create = canCreate && item === trimmedInput
              return (
                <ComboboxItem key={item} value={item}>
                  {create && (
                    <HugeiconsIcon
                      icon={Add01Icon}
                      strokeWidth={2}
                      className='text-muted-foreground'
                      aria-hidden='true'
                    />
                  )}
                  <span className='truncate'>
                    {create ? `添加“${item}”` : (labels.get(item) ?? item)}
                  </span>
                </ComboboxItem>
              )
            }}
          </ComboboxCollection>
        </ComboboxList>
        <ComboboxEmpty>没有匹配项，可直接输入并按回车添加</ComboboxEmpty>
      </ComboboxContent>
    </Combobox>
  )
}
