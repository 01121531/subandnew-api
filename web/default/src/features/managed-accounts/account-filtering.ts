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
  'requests',
  'tokens',
  'amount',
  'rpm',
  'active_sessions',
  'utilization_5h',
  'utilization_7d',
  'created_at',
  'last_activity_at',
] as const

export type AccountFilterField = (typeof ACCOUNT_FILTER_FIELDS)[number]
export type AccountFilterMatchMode = 'all' | 'any'
type AccountFilterValueMode = 'all' | 'any'
type AccountTextFilterOperator =
  | 'contains'
  | 'starts_with'
  | 'ends_with'
  | 'not_contains'
  | 'is_empty'
  | 'is_not_empty'
type AccountCategoryFilterOperator =
  | 'is'
  | 'is_not'
  | 'is_empty'
  | 'is_not_empty'
type AccountMetricFilterOperator =
  | 'eq'
  | 'gt'
  | 'gte'
  | 'lt'
  | 'lte'
  | 'between'
  | 'is_empty'
  | 'is_not_empty'
export type AccountFilterOperator =
  | AccountTextFilterOperator
  | AccountCategoryFilterOperator
  | AccountMetricFilterOperator

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

const NUMBER_ACCOUNT_FILTER_FIELDS = new Set<AccountFilterField>([
  'requests',
  'tokens',
  'amount',
  'rpm',
  'active_sessions',
  'utilization_5h',
  'utilization_7d',
])

const TIME_ACCOUNT_FILTER_FIELDS = new Set<AccountFilterField>([
  'created_at',
  'last_activity_at',
])

export function isMetricAccountFilterField(field: AccountFilterField) {
  return (
    NUMBER_ACCOUNT_FILTER_FIELDS.has(field) ||
    TIME_ACCOUNT_FILTER_FIELDS.has(field)
  )
}

const QUICK_ACCOUNT_FILTER_FIELDS: AccountFilterField[] = [
  'name',
  'account_id',
  'note',
  'ownership',
]

export function createAccountFilterRule(
  field: AccountFilterField = 'name'
): AccountFilterRule {
  let operator: AccountFilterOperator = 'is'
  if (TEXT_ACCOUNT_FILTER_FIELDS.has(field)) operator = 'contains'
  if (isMetricAccountFilterField(field)) operator = 'gte'
  return {
    id: globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`,
    field,
    operator,
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

function normalizeAccountFilterValues(values: unknown[]) {
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
  ).map((value) => value.toLocaleLowerCase())
  const emails = document.email.map((value) => value.toLocaleLowerCase())
  const matches = (term: string) =>
    searchable.some((value) => value.includes(term)) ||
    emails.some((email) => {
      const candidate = /[@.]/.test(term) ? email : email.split('@', 1)[0]
      return candidate.includes(term)
    })
  return (
    (included.length === 0 || included.some(matches)) && !excluded.some(matches)
  )
}

export function isAccountFilterRuleComplete(rule: AccountFilterRule) {
  if (rule.operator === 'is_empty' || rule.operator === 'is_not_empty') {
    return true
  }
  const values = rule.values.filter((value) => value.trim() !== '')
  if (rule.operator === 'between') {
    return (
      values.length === 2 &&
      values.every((value) => parseMetricFilterValue(rule.field, value) != null)
    )
  }
  if (isMetricAccountFilterField(rule.field)) {
    return (
      values.length === 1 &&
      parseMetricFilterValue(rule.field, values[0]) != null
    )
  }
  return values.length > 0
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

function matchesAccountFilterRule(
  document: AccountFilterDocument,
  rule: AccountFilterRule
) {
  if (isMetricAccountFilterField(rule.field)) {
    return matchesMetricAccountFilterRule(document, rule)
  }
  const fieldValues = document[rule.field]
    .map((value) => value.trim().toLocaleLowerCase())
    .filter(Boolean)
  if (rule.operator === 'is_empty') return fieldValues.length === 0
  if (rule.operator === 'is_not_empty') return fieldValues.length > 0

  const expected = parseAccountFilterTerms(rule.values.join('\n'))
  if (expected.length === 0) return true
  const valueMatches = expected.map((term) =>
    fieldValues.some((value) => {
      if (rule.operator === 'starts_with') return value.startsWith(term)
      if (rule.operator === 'ends_with') return value.endsWith(term)
      if (rule.operator === 'contains' || rule.operator === 'not_contains') {
        return value.includes(term)
      }
      return value === term
    })
  )
  const positive =
    rule.value_mode === 'all'
      ? valueMatches.every(Boolean)
      : valueMatches.some(Boolean)
  return rule.operator === 'not_contains' || rule.operator === 'is_not'
    ? !positive
    : positive
}

function matchesMetricAccountFilterRule(
  document: AccountFilterDocument,
  rule: AccountFilterRule
) {
  const values = document[rule.field]
    .map((value) => parseMetricFilterValue(rule.field, value))
    .filter((value): value is number => value != null)
  if (rule.operator === 'is_empty') return values.length === 0
  if (rule.operator === 'is_not_empty') return values.length > 0
  if (values.length === 0) return false

  const expected = rule.values
    .map((value) => parseMetricFilterValue(rule.field, value))
    .filter((value): value is number => value != null)
  if (rule.operator === 'between') {
    if (expected.length !== 2) return false
    const minimum = Math.min(expected[0], expected[1])
    const maximum = Math.max(expected[0], expected[1])
    return values.some((value) => value >= minimum && value <= maximum)
  }
  if (expected.length !== 1) return false
  const target = expected[0]
  return values.some((value) => {
    switch (rule.operator) {
      case 'eq':
        return value === target
      case 'gt':
        return value > target
      case 'gte':
        return value >= target
      case 'lt':
        return value < target
      case 'lte':
        return value <= target
      default:
        return false
    }
  })
}

function parseMetricFilterValue(field: AccountFilterField, raw: string) {
  const value = raw.trim()
  if (!value) return null
  if (!TIME_ACCOUNT_FILTER_FIELDS.has(field)) {
    const numeric = Number(value)
    return Number.isFinite(numeric) ? numeric : null
  }

  const numeric = Number(value)
  if (Number.isFinite(numeric)) {
    return numeric > 100_000_000_000 ? numeric / 1000 : numeric
  }
  const localMatch = value.match(
    /^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{2}):(\d{2})(?::(\d{2}))?)?$/
  )
  const timestamp = localMatch
    ? Date.parse(
        `${localMatch[1]}-${localMatch[2]}-${localMatch[3]}T${localMatch[4] ?? '00'}:${localMatch[5] ?? '00'}:${localMatch[6] ?? '00'}+08:00`
      )
    : Date.parse(value)
  return Number.isNaN(timestamp) ? null : timestamp / 1000
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
    rules: (template.rules ?? []).map((rule) => ({
      ...rule,
      id: createAccountFilterRule(rule.field).id,
      values: [...(rule.values ?? [])],
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
