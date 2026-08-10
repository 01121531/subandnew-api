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
import { ChevronDownIcon, X } from 'lucide-react'
import * as React from 'react'
import { enUS, fr, ja, ru, vi, zhCN } from 'react-day-picker/locale'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'

const calendarLocales = {
  en: enUS,
  zh: zhCN,
  fr,
  ru,
  ja,
  vi,
} as const

interface DateTimePickerProps {
  id?: string
  name?: string
  value?: Date
  onChange?: (date: Date | undefined) => void
  placeholder?: string
  className?: string
  clearable?: boolean
}

export function DateTimePicker({
  id,
  name,
  value,
  onChange,
  placeholder,
  className,
  clearable = true,
}: DateTimePickerProps) {
  const { t, i18n } = useTranslation()
  const placeholderText = placeholder ?? t('Select date')
  const resolvedLanguage = (
    i18n.resolvedLanguage ?? i18n.language
  ).toLowerCase()
  const language = resolvedLanguage.startsWith('zh')
    ? 'zh'
    : resolvedLanguage.split('-')[0]
  const calendarLocale =
    calendarLocales[language as keyof typeof calendarLocales] ?? enUS
  const currentYear = new Date().getFullYear()
  const [open, setOpen] = React.useState(false)
  const [date, setDate] = React.useState<Date | undefined>(value)
  const [month, setMonth] = React.useState<Date | undefined>(value)
  const [time, setTime] = React.useState('00:00')

  React.useEffect(() => {
    setDate(value)
    setMonth(value)
    if (value) {
      const hours = value.getHours().toString().padStart(2, '0')
      const minutes = value.getMinutes().toString().padStart(2, '0')
      setTime(`${hours}:${minutes}`)
    }
  }, [value])

  const handleDateSelect = (selectedDate: Date | undefined) => {
    if (!selectedDate) {
      setDate(undefined)
      setMonth(undefined)
      onChange?.(undefined)
      return
    }
    const [hours, minutes] = time.split(':').map(Number)
    const nextDate = new Date(selectedDate)
    nextDate.setHours(hours, minutes, 0, 0)
    setDate(nextDate)
    setMonth(nextDate)
    onChange?.(nextDate)
    setOpen(false)
  }

  const handleTimeChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const nextTime = event.target.value
    setTime(nextTime)
    if (!date) return

    const [hours, minutes] = nextTime.split(':').map(Number)
    const nextDate = new Date(date)
    nextDate.setHours(hours, minutes, 0, 0)
    setDate(nextDate)
    onChange?.(nextDate)
  }

  const handleClear = () => {
    setDate(undefined)
    setMonth(undefined)
    setTime('00:00')
    onChange?.(undefined)
  }

  return (
    <div className={cn('flex min-w-0 gap-2', className)}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              id={id}
              type='button'
              variant='outline'
              className={cn(
                'min-w-0 flex-1 justify-between font-normal tabular-nums',
                !date && 'text-muted-foreground'
              )}
            />
          }
        >
          <span className='truncate'>
            {date ? dayjs(date).format('YYYY-MM-DD') : placeholderText}
          </span>
          <ChevronDownIcon className='size-4 shrink-0 opacity-50' />
        </PopoverTrigger>
        <PopoverContent className='w-auto overflow-hidden p-0' align='start'>
          <Calendar
            mode='single'
            selected={date}
            month={month}
            onMonthChange={setMonth}
            captionLayout='dropdown'
            onSelect={handleDateSelect}
            locale={calendarLocale}
            startMonth={new Date(currentYear - 100, 0)}
            endMonth={new Date(currentYear + 100, 11)}
          />
        </PopoverContent>
      </Popover>
      <Input
        id={id ? `${id}-time` : undefined}
        name={name ? `${name}-time` : undefined}
        type='time'
        value={time}
        onChange={handleTimeChange}
        aria-label={t('Time')}
        className='w-28 appearance-none tabular-nums [&::-webkit-calendar-picker-indicator]:hidden [&::-webkit-calendar-picker-indicator]:appearance-none'
        disabled={!date}
      />
      {clearable && date && (
        <Button
          type='button'
          variant='outline'
          size='icon'
          onClick={handleClear}
          className='shrink-0'
          aria-label={t('Clear')}
        >
          <X />
        </Button>
      )}
    </div>
  )
}
