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
import { Calendar, CalendarDays, ChevronDown, RotateCcw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DateTimePicker } from '@/components/datetime-picker'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'

import {
  createFleetPresetRange,
  FLEET_TIME_PRESETS,
  resolveFleetTimeRange,
  type FleetTimeRange,
} from './time-range'

function SectionDivider({ label }: { label: string }) {
  return (
    <div className='relative py-1'>
      <div className='absolute inset-0 flex items-center'>
        <span className='border-border w-full border-t' />
      </div>
      <div className='relative flex justify-center text-xs uppercase'>
        <span className='bg-popover text-muted-foreground px-2'>{label}</span>
      </div>
    </div>
  )
}

export function FleetTimeRangeFilter(props: {
  value: FleetTimeRange
  onChange: (value: FleetTimeRange) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState(props.value)
  const invalid = draft.start.getTime() > draft.end.getTime()

  const appliedLabel = useMemo(() => {
    if (props.value.presetDays) {
      const preset = FLEET_TIME_PRESETS.find(
        (item) => item.days === props.value.presetDays
      )
      return preset ? t(preset.label) : ''
    }
    return `${dayjs(props.value.start).format('MM-DD HH:mm')} ~ ${dayjs(props.value.end).format('MM-DD HH:mm')}`
  }, [props.value, t])

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) setDraft(resolveFleetTimeRange(props.value))
    setOpen(nextOpen)
  }

  const apply = () => {
    if (invalid) return
    props.onChange(draft)
    setOpen(false)
  }

  const reset = () => {
    const nextRange = createFleetPresetRange(7)
    setDraft(nextRange)
    props.onChange(nextRange)
    setOpen(false)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          <Button
            type='button'
            variant='outline'
            size='sm'
            className='h-11 min-w-0 flex-1 justify-start md:h-8 md:max-w-72 md:min-w-40 md:flex-none'
            aria-label={`${t('Time Range')}: ${appliedLabel}`}
          />
        }
      >
        <CalendarDays className='text-muted-foreground' />
        <span className='min-w-0 flex-1 truncate text-left font-normal tabular-nums'>
          {appliedLabel}
        </span>
        <ChevronDown className='text-muted-foreground ml-auto size-3.5' />
      </DialogTrigger>
      <DialogContent className='max-sm:h-dvh max-sm:w-screen max-sm:max-w-none max-sm:rounded-none sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Time Range')}</DialogTitle>
          <DialogDescription>
            {t('Choose a quick range or set a custom start and end time.')}
          </DialogDescription>
        </DialogHeader>

        <div className='grid gap-4 py-1'>
          <div className='grid gap-2'>
            <div className='flex items-center gap-2 text-sm font-medium'>
              <Calendar className='size-4' />
              {t('Quick Range')}
            </div>
            <div className='grid grid-cols-2 gap-2 sm:flex'>
              {FLEET_TIME_PRESETS.map((preset) => (
                <Button
                  key={preset.days}
                  type='button'
                  size='sm'
                  variant={
                    draft.presetDays === preset.days ? 'default' : 'outline'
                  }
                  aria-pressed={draft.presetDays === preset.days}
                  onClick={() => setDraft(createFleetPresetRange(preset.days))}
                  className={cn('flex-1')}
                >
                  {t(preset.label)}
                </Button>
              ))}
            </div>
          </div>

          <SectionDivider label={t('Custom Time Range')} />

          <div className='grid gap-3'>
            <div className='grid gap-1.5'>
              <Label htmlFor='fleet-range-start'>{t('Start Time')}</Label>
              <DateTimePicker
                id='fleet-range-start'
                name='fleet-range-start'
                value={draft.start}
                clearable={false}
                onChange={(start) => {
                  if (start) setDraft({ ...draft, start, presetDays: null })
                }}
                placeholder={t('Select start time')}
              />
            </div>
            <div className='grid gap-1.5'>
              <Label htmlFor='fleet-range-end'>{t('End Time')}</Label>
              <DateTimePicker
                id='fleet-range-end'
                name='fleet-range-end'
                value={draft.end}
                clearable={false}
                onChange={(end) => {
                  if (end) setDraft({ ...draft, end, presetDays: null })
                }}
                placeholder={t('Select end time')}
              />
            </div>
            {invalid && (
              <p className='text-destructive text-xs' role='alert'>
                {t('Start time cannot be later than end time')}
              </p>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button type='button' variant='outline' onClick={reset}>
            <RotateCcw />
            {t('Reset')}
          </Button>
          <Button type='button' onClick={apply} disabled={invalid}>
            {t('Confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
