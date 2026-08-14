/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/

function shiftDecimal(value: string, places: number) {
  const match = value.trim().match(/^(\d+)(?:\.(\d*))?$/)
  if (!match) return ''

  const whole = match[1]
  const fraction = match[2] ?? ''
  const digits = `${whole}${fraction}`
  const point = whole.length + places
  let shifted: string

  if (point <= 0) {
    shifted = `0.${'0'.repeat(-point)}${digits}`
  } else if (point >= digits.length) {
    shifted = `${digits}${'0'.repeat(point - digits.length)}`
  } else {
    shifted = `${digits.slice(0, point)}.${digits.slice(point)}`
  }

  const [shiftedWhole, shiftedFraction = ''] = shifted.split('.')
  const normalizedWhole = shiftedWhole.replace(/^0+(?=\d)/, '') || '0'
  const normalizedFraction = shiftedFraction.replace(/0+$/, '')
  return normalizedFraction
    ? `${normalizedWhole}.${normalizedFraction}`
    : normalizedWhole
}

export function discountMultiplierToPercent(value: string) {
  return shiftDecimal(value, 2)
}

export function discountPercentToMultiplier(value: string) {
  return shiftDecimal(value, -2)
}

export function formatDiscountPercent(value: string) {
  const percent = discountMultiplierToPercent(value)
  return percent ? `${percent}%` : value || '—'
}
