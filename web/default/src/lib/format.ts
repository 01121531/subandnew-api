/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import dayjs from '@/lib/dayjs'

export function formatTimestamp(timestamp: number): string {
  if (timestamp === -1) return 'Never'
  return formatTimestampToDate(timestamp)
}

export function formatTimestampToDate(
  timestamp?: number,
  unit: 'seconds' | 'milliseconds' = 'seconds'
): string {
  if (!timestamp || timestamp === -1) return '-'
  const milliseconds = unit === 'seconds' ? timestamp * 1000 : timestamp
  return dayjs(milliseconds).format('YYYY-MM-DD HH:mm:ss')
}

export function formatTimestampRelative(
  timestamp?: number,
  unit: 'seconds' | 'milliseconds' = 'seconds',
  locales?: Intl.LocalesArgument
): string {
  if (!timestamp || timestamp === -1) return '-'

  const milliseconds = unit === 'seconds' ? timestamp * 1000 : timestamp
  const seconds = Math.round((milliseconds - Date.now()) / 1000)
  const absoluteSeconds = Math.abs(seconds)
  const formatter = new Intl.RelativeTimeFormat(locales, { numeric: 'always' })

  if (absoluteSeconds < 60) return formatter.format(seconds, 'second')
  if (absoluteSeconds < 3600)
    return formatter.format(Math.round(seconds / 60), 'minute')
  if (absoluteSeconds < 86400)
    return formatter.format(Math.round(seconds / 3600), 'hour')
  if (absoluteSeconds < 2592000)
    return formatter.format(Math.round(seconds / 86400), 'day')
  if (absoluteSeconds < 31536000)
    return formatter.format(Math.round(seconds / 2592000), 'month')
  return formatter.format(Math.round(seconds / 31536000), 'year')
}
