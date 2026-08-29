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
export const ACCOUNT_FILTER_FIELDS = [
  'name',
  'email',
  'account_id',
  'note',
  'ownership',
  'instance',
  'platform',
  'type',
  'group',
  'status',
  'source',
  'available',
] as const

export type AccountFilterField = (typeof ACCOUNT_FILTER_FIELDS)[number]
export type AccountFilterMatchMode = 'all' | 'any'
export type AccountFilterValueMode = 'all' | 'any'
export type AccountTextFilterOperator =
  | 'contains'
  | 'not_contains'
  | 'is_empty'
  | 'is_not_empty'
export type AccountCategoryFilterOperator =
  | 'is'
  | 'is_not'
  | 'is_empty'
  | 'is_not_empty'
export type AccountFilterOperator =
  | AccountTextFilterOperator
  | AccountCategoryFilterOperator

export type AccountFilterRule = {
  id: string
  field: AccountFilterField
  operator: AccountFilterOperator
  values: string[]
  value_mode: AccountFilterValueMode
}

export type AccountFilterRuleInput = Omit<AccountFilterRule, 'id'>

export type AccountAdvancedFilter = {
  match_mode: AccountFilterMatchMode
  rules: AccountFilterRule[]
}

export type AccountFilterTemplate = {
  id: number
  name: string
  match_mode: AccountFilterMatchMode
  rules: AccountFilterRuleInput[]
  created_at: number
  updated_at: number
}

export type AccountFilterTemplateInput = {
  name: string
  match_mode: AccountFilterMatchMode
  rules: AccountFilterRuleInput[]
}

export type AccountFilterDocument = Record<AccountFilterField, string[]>

export const TEXT_ACCOUNT_FILTER_FIELDS = new Set<AccountFilterField>([
  'name',
  'email',
  'account_id',
  'note',
  'ownership',
])

const QUICK_ACCOUNT_FILTER_FIELDS: AccountFilterField[] = [
  'name',
  'email',
  'account_id',
  'note',
  'ownership',
]

export function createAccountFilterRule(
  field: AccountFilterField = 'name'
): AccountFilterRule {
  return {
    id: globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`,
    field,
    operator: TEXT_ACCOUNT_FILTER_FIELDS.has(field) ? 'contains' : 'is',
    values: [],
    value_mode: 'any',
  }
}

export function parseAccountFilterTerms(value: string) {
  const seen = new Set<string>()
  return value
    .split(/[,，\n]+/)
    .map((term) => term.trim().toLocaleLowerCase())
    .filter((term) => {
      if (!term || seen.has(term)) return false
      seen.add(term)
      return true
    })
}

export function parseAccountFilterDisplayValues(value: string) {
  const seen = new Set<string>()
  return value
    .split(/[,，\n]+/)
    .map((term) => term.trim())
    .filter((term) => {
      const key = term.toLocaleLowerCase()
      if (!term || seen.has(key)) return false
      seen.add(key)
      return true
    })
}

export function normalizeAccountFilterValues(values: unknown[]) {
  return values
    .filter((value) => value != null)
    .map((value) => String(value).trim())
    .filter(Boolean)
}

export function accountFilterDocument(
  input: Partial<Record<AccountFilterField, unknown | unknown[]>>
): AccountFilterDocument {
  return Object.fromEntries(
    ACCOUNT_FILTER_FIELDS.map((field) => {
      const raw = input[field]
      return [
        field,
        normalizeAccountFilterValues(Array.isArray(raw) ? raw : [raw]),
      ]
    })
  ) as AccountFilterDocument
}

export function matchesQuickAccountFilter(
  document: AccountFilterDocument,
  included: string[],
  excluded: string[]
) {
  const searchable = QUICK_ACCOUNT_FILTER_FIELDS.flatMap(
    (field) => document[field]
  )
    .join(' ')
    .toLocaleLowerCase()
  return (
    (included.length === 0 ||
      included.some((term) => searchable.includes(term))) &&
    !excluded.some((term) => searchable.includes(term))
  )
}

export function isAccountFilterRuleComplete(rule: AccountFilterRule) {
  if (rule.operator === 'is_empty' || rule.operator === 'is_not_empty') {
    return true
  }
  return rule.values.some((value) => value.trim() !== '')
}

export function matchesAdvancedAccountFilter(
  document: AccountFilterDocument,
  filter: AccountAdvancedFilter
) {
  const activeRules = filter.rules.filter(isAccountFilterRuleComplete)
  if (activeRules.length === 0) return true
  const matches = activeRules.map((rule) =>
    matchesAccountFilterRule(document, rule)
  )
  return filter.match_mode === 'all'
    ? matches.every(Boolean)
    : matches.some(Boolean)
}

export function matchesAccountFilterRule(
  document: AccountFilterDocument,
  rule: AccountFilterRule
) {
  const fieldValues = document[rule.field]
    .map((value) => value.trim().toLocaleLowerCase())
    .filter(Boolean)
  if (rule.operator === 'is_empty') return fieldValues.length === 0
  if (rule.operator === 'is_not_empty') return fieldValues.length > 0

  const expected = parseAccountFilterTerms(rule.values.join('\n'))
  if (expected.length === 0) return true
  const valueMatches = expected.map((term) =>
    fieldValues.some((value) =>
      rule.operator === 'contains' || rule.operator === 'not_contains'
        ? value.includes(term)
        : value === term
    )
  )
  const positive =
    rule.value_mode === 'all'
      ? valueMatches.every(Boolean)
      : valueMatches.some(Boolean)
  return rule.operator === 'not_contains' || rule.operator === 'is_not'
    ? !positive
    : positive
}

export function accountFilterTemplateInput(
  name: string,
  filter: AccountAdvancedFilter
): AccountFilterTemplateInput {
  return {
    name: name.trim(),
    match_mode: filter.match_mode,
    rules: filter.rules.map(({ id: _id, ...rule }) => ({
      ...rule,
      values:
        rule.operator === 'is_empty' || rule.operator === 'is_not_empty'
          ? []
          : normalizeAccountFilterValues(rule.values),
    })),
  }
}

export function accountFilterFromTemplate(
  template: AccountFilterTemplate
): AccountAdvancedFilter {
  return {
    match_mode: template.match_mode,
    rules: template.rules.map((rule) => ({
      ...rule,
      id: createAccountFilterRule(rule.field).id,
      values: [...rule.values],
    })),
  }
}

export function accountFilterSnapshot(filter: AccountAdvancedFilter) {
  const effectiveFilter = {
    ...filter,
    rules: filter.rules.filter(isAccountFilterRuleComplete),
  }
  return {
    match_mode: filter.match_mode,
    rules: accountFilterTemplateInput('snapshot', effectiveFilter).rules,
  }
}
