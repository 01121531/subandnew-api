/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import type { AccountFilterField } from '@/features/managed-accounts/account-filtering'

import type { PortalResult, PortalSelection } from './types'

export type PortalSelectionState = {
  allFiltered: boolean
  selected: Map<string, PortalSelection>
  excluded: Map<string, PortalSelection>
}

export function portalSelectionKey(item: PortalSelection) {
  return `${item.instance_id}\u0000${item.account_id}`
}

export function emptyPortalSelection(): PortalSelectionState {
  return {
    allFiltered: false,
    selected: new Map(),
    excluded: new Map(),
  }
}

export function toggleAllPortalSelection(
  state: PortalSelectionState
): PortalSelectionState {
  return {
    allFiltered: !state.allFiltered,
    selected: new Map(),
    excluded: new Map(),
  }
}

export function togglePortalSelection(
  state: PortalSelectionState,
  item: PortalSelection,
  checked: boolean
): PortalSelectionState {
  const key = portalSelectionKey(item)
  if (state.allFiltered) {
    const excluded = new Map(state.excluded)
    if (checked) excluded.delete(key)
    else excluded.set(key, item)
    return { ...state, excluded }
  }
  const selected = new Map(state.selected)
  if (checked) selected.set(key, item)
  else selected.delete(key)
  return { ...state, selected }
}

export function portalSelectionCount(
  state: PortalSelectionState,
  filteredTotal: number
) {
  return state.allFiltered
    ? Math.max(0, filteredTotal - state.excluded.size)
    : state.selected.size
}

export function isPortalSelectionChecked(
  state: PortalSelectionState,
  item: PortalSelection
) {
  const key = portalSelectionKey(item)
  return state.allFiltered ? !state.excluded.has(key) : state.selected.has(key)
}

type FilterOption = { value: string; label: string }

export function portalFilterOptions(
  result: PortalResult | null,
  fields: AccountFilterField[]
): Partial<Record<AccountFilterField, FilterOption[]>> {
  const options: Partial<Record<AccountFilterField, FilterOption[]>> = {}
  for (const field of fields) {
    const serverValues = result?.filter_options?.[field]
    if (serverValues) {
      options[field] = uniqueOptions(
        serverValues.map((option) =>
          typeof option === 'string'
            ? { value: option, label: option }
            : { value: option.value, label: option.label || option.value }
        )
      )
      continue
    }

    let sourceField: string = field
    if (field === 'instance') sourceField = 'instance_name'
    if (field === 'source') sourceField = 'source_name'
    options[field] = uniqueOptions(
      (result?.items ?? []).flatMap((item) => {
        const value = item[sourceField]
        if (value == null || value === '') return []
        return [{ value: String(value), label: String(value) }]
      })
    )
  }
  return options
}

function uniqueOptions(options: FilterOption[]) {
  const seen = new Set<string>()
  return options.filter((option) => {
    if (!option.value || seen.has(option.value)) return false
    seen.add(option.value)
    return true
  })
}

export function isAbortError(error: unknown) {
  return error instanceof DOMException && error.name === 'AbortError'
}

export class PortalRequestGate {
  private sequence = 0

  next() {
    this.sequence += 1
    return this.sequence
  }

  isCurrent(sequence: number) {
    return sequence === this.sequence
  }
}
