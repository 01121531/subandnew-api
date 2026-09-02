/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/

export type NormalizedFilterEntry = {
  key: string
  values: string[]
}

function normalizeFilterValues(value: unknown): string[] {
  let values: unknown[] = []
  if (Array.isArray(value)) {
    values = value
  } else if (typeof value === 'string') {
    values = [value]
  }

  return values.filter(
    (item): item is string => typeof item === 'string' && item.length > 0
  )
}

export function normalizedFilterEntries(
  filters: unknown
): NormalizedFilterEntry[] {
  if (
    filters == null ||
    typeof filters !== 'object' ||
    Array.isArray(filters)
  ) {
    return []
  }

  return Object.entries(filters).flatMap(([key, value]) => {
    const values = normalizeFilterValues(value)
    return values.length > 0 ? [{ key, values }] : []
  })
}
