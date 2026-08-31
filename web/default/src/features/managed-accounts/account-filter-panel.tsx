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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, Filter, ListPlus, Plus, Save, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { MultiSelect, type MultiSelectOption } from '@/components/multi-select'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  ACCOUNT_FILTER_FIELDS,
  TEXT_ACCOUNT_FILTER_FIELDS,
  accountFilterFromTemplate,
  accountFilterTemplateInput,
  createAccountFilterRule,
  isAccountFilterRuleComplete,
  isMetricAccountFilterField,
  isTimeAccountFilterField,
  accountFilterDateTimeInputValue,
  parseAccountFilterDisplayValues,
  type AccountAdvancedFilter,
  type AccountFilterField,
  type AccountFilterOperator,
  type AccountFilterRule,
} from './account-filtering'
import {
  createAccountFilterTemplate,
  deleteAccountFilterTemplate,
  listAccountFilterTemplates,
  updateAccountFilterTemplate,
} from './api'

const TEMPLATE_QUERY_KEY = ['managed-account-filter-templates'] as const

const fieldLabels: Record<AccountFilterField, string> = {
  name: 'Account name',
  email: 'Email',
  account_id: 'Account ID',
  note: 'Note',
  ownership: 'Ownership',
  instance: 'Instance',
  platform: 'Platform',
  type: 'Account type',
  group: 'Group',
  status: 'Status',
  source: 'Work node',
  available: 'Availability',
  requests: 'Requests',
  tokens: 'Tokens',
  amount: 'Amount',
  rpm: 'RPM',
  active_sessions: 'Active sessions',
  utilization_5h: '5-hour utilization (%)',
  utilization_7d: '7-day utilization (%)',
  created_at: 'Created at',
  last_activity_at: 'Last activity',
}

const operatorLabels: Record<AccountFilterOperator, string> = {
  contains: 'Contains anywhere',
  starts_with: 'Starts with',
  ends_with: 'Ends with',
  not_contains: 'Does not contain',
  is: 'Is one of',
  is_not: 'Is not one of',
  is_empty: 'Is empty',
  is_not_empty: 'Is not empty',
  eq: 'Equals',
  gt: 'Greater than',
  gte: 'Greater than or equal to',
  lt: 'Less than',
  lte: 'Less than or equal to',
  between: 'Between (inclusive)',
}

function operatorsFor(field: AccountFilterField): AccountFilterOperator[] {
  if (isMetricAccountFilterField(field)) {
    return [
      'eq',
      'gt',
      'gte',
      'lt',
      'lte',
      'between',
      'is_empty',
      'is_not_empty',
    ]
  }
  return TEXT_ACCOUNT_FILTER_FIELDS.has(field)
    ? [
        'contains',
        'starts_with',
        'ends_with',
        'not_contains',
        'is_empty',
        'is_not_empty',
      ]
    : ['is', 'is_not', 'is_empty', 'is_not_empty']
}

function defaultOperatorFor(field: AccountFilterField): AccountFilterOperator {
  if (TEXT_ACCOUNT_FILTER_FIELDS.has(field)) return 'contains'
  if (isMetricAccountFilterField(field)) return 'gte'
  return 'is'
}

function valuesForOperator(
  operator: AccountFilterOperator,
  values: string[],
  metricField: boolean
) {
  if (operator === 'is_empty' || operator === 'is_not_empty') return []
  if (operator === 'between') return values.slice(0, 2)
  if (metricField) return values.slice(0, 1)
  return values
}

export function AccountFilterPanel(props: {
  value: AccountAdvancedFilter
  onChange: (value: AccountAdvancedFilter) => void
  options: Partial<Record<AccountFilterField, MultiSelectOption[]>>
  templatesEnabled?: boolean
  allowedFields?: AccountFilterField[]
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [selectedTemplateID, setSelectedTemplateID] = useState<number | null>(
    null
  )
  const [saveOpen, setSaveOpen] = useState(false)
  const [templateName, setTemplateName] = useState('')
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [bulkRuleID, setBulkRuleID] = useState<string | null>(null)
  const [bulkValues, setBulkValues] = useState('')
  const templatesQuery = useQuery({
    queryKey: TEMPLATE_QUERY_KEY,
    queryFn: listAccountFilterTemplates,
    enabled: props.templatesEnabled !== false,
  })
  const templates = templatesQuery.data?.data ?? []
  const selectedTemplate = templates.find(
    (template) => template.id === selectedTemplateID
  )
  const completeRules = props.value.rules.filter(isAccountFilterRuleComplete)
  const rulesValid =
    props.value.rules.length > 0 &&
    props.value.rules.every(isAccountFilterRuleComplete)
  const allowedFields = props.allowedFields ?? [...ACCOUNT_FILTER_FIELDS]

  const refreshTemplates = async () => {
    await queryClient.invalidateQueries({ queryKey: TEMPLATE_QUERY_KEY })
  }
  const createMutation = useMutation({
    mutationFn: () =>
      createAccountFilterTemplate(
        accountFilterTemplateInput(templateName, props.value)
      ),
    onSuccess: async (response) => {
      setSelectedTemplateID(response.data.id)
      setSaveOpen(false)
      setTemplateName('')
      await refreshTemplates()
      toast.success(t('Filter template saved'))
    },
    onError: () => toast.error(t('Could not save filter template')),
  })
  const updateMutation = useMutation({
    mutationFn: () =>
      updateAccountFilterTemplate(
        selectedTemplate?.id ?? 0,
        accountFilterTemplateInput(selectedTemplate?.name ?? '', props.value)
      ),
    onSuccess: async () => {
      await refreshTemplates()
      toast.success(t('Filter template updated'))
    },
    onError: () => toast.error(t('Could not update filter template')),
  })
  const deleteMutation = useMutation({
    mutationFn: () => deleteAccountFilterTemplate(selectedTemplate?.id ?? 0),
    onSuccess: async () => {
      setSelectedTemplateID(null)
      setDeleteOpen(false)
      await refreshTemplates()
      toast.success(t('Filter template deleted'))
    },
    onError: () => toast.error(t('Could not delete filter template')),
  })

  const updateRule = (id: string, patch: Partial<AccountFilterRule>) => {
    props.onChange({
      ...props.value,
      rules: props.value.rules.map((rule) =>
        rule.id === id ? { ...rule, ...patch } : rule
      ),
    })
  }
  const removeRule = (id: string) => {
    props.onChange({
      ...props.value,
      rules: props.value.rules.filter((rule) => rule.id !== id),
    })
  }
  const applyTemplate = (value: string | null) => {
    if (!value) return
    if (value === 'none') {
      setSelectedTemplateID(null)
      return
    }
    const template = templates.find((item) => item.id === Number(value))
    if (!template) return
    props.onChange(accountFilterFromTemplate(template))
    setSelectedTemplateID(template.id)
    setOpen(true)
    toast.success(t('Filter template applied'))
  }
  const applyBulkValues = () => {
    if (!bulkRuleID) return
    const incoming = parseAccountFilterDisplayValues(bulkValues)
    if (incoming.some((value) => value.length > 200)) {
      toast.error(t('单个筛选值不能超过 200 个字符'))
      return
    }
    const rule = props.value.rules.find((item) => item.id === bulkRuleID)
    if (!rule) return
    const merged = parseAccountFilterDisplayValues(
      [...rule.values, ...incoming].join('\n')
    )
    if (merged.length > 50) {
      toast.error(t('每条筛选规则最多包含 50 个值'))
      return
    }
    updateRule(bulkRuleID, { values: merged })
    setBulkRuleID(null)
    setBulkValues('')
  }

  return (
    <>
      <Collapsible open={open} onOpenChange={setOpen}>
        <div className='border-border/70 bg-muted/15 @container/account-filter max-w-full min-w-0 rounded-md border'>
          <div className='flex flex-col gap-2 p-2.5 @xl/account-filter:flex-row @xl/account-filter:flex-wrap @xl/account-filter:items-center'>
            <CollapsibleTrigger
              render={
                <Button
                  variant='ghost'
                  className='min-h-11 justify-between sm:min-h-9'
                />
              }
            >
              <span className='flex items-center gap-2'>
                <Filter className='size-4' />
                {t('Advanced filters')}
                {completeRules.length > 0 && (
                  <Badge variant='secondary'>{completeRules.length}</Badge>
                )}
              </span>
              <ChevronDown
                className={cn(
                  'size-4 transition-transform',
                  open && 'rotate-180'
                )}
              />
            </CollapsibleTrigger>
            {props.templatesEnabled !== false && (
              <Select
                value={
                  selectedTemplateID == null
                    ? 'none'
                    : String(selectedTemplateID)
                }
                onValueChange={applyTemplate}
              >
                <SelectTrigger className='min-h-11 w-full @xl/account-filter:min-h-9 @xl/account-filter:w-56'>
                  <SelectValue placeholder={t('Filter templates')} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='none'>
                    {t('No template selected')}
                  </SelectItem>
                  {templates.map((template) => (
                    <SelectItem key={template.id} value={String(template.id)}>
                      {template.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            {props.templatesEnabled !== false && (
              <div className='flex flex-wrap gap-2 @xl/account-filter:ms-auto'>
                <Button
                  variant='outline'
                  size='sm'
                  className='min-h-11 flex-1 @xl/account-filter:min-h-9 @xl/account-filter:flex-none'
                  disabled={!rulesValid}
                  onClick={() => setSaveOpen(true)}
                >
                  <Save />
                  {t('Save as template')}
                </Button>
                {selectedTemplate && (
                  <>
                    <Button
                      variant='outline'
                      size='sm'
                      className='min-h-11 @xl/account-filter:min-h-9'
                      disabled={!rulesValid || updateMutation.isPending}
                      onClick={() => updateMutation.mutate()}
                    >
                      {t('Update template')}
                    </Button>
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      className='text-destructive min-h-11 min-w-11 @xl/account-filter:min-h-9 @xl/account-filter:min-w-9'
                      aria-label={t('Delete template')}
                      title={t('Delete template')}
                      onClick={() => setDeleteOpen(true)}
                    >
                      <Trash2 />
                    </Button>
                  </>
                )}
              </div>
            )}
          </div>
          <CollapsibleContent>
            <div className='border-border/70 space-y-3 border-t p-3'>
              <div className='flex flex-col gap-2 @lg/account-filter:flex-row @lg/account-filter:items-center @lg/account-filter:justify-between'>
                <div>
                  <p className='text-sm font-medium'>{t('Rule combination')}</p>
                  <p className='text-muted-foreground text-xs'>
                    {t('Quick search and advanced filters must both match.')}
                  </p>
                </div>
                <div className='bg-muted grid grid-cols-2 rounded-md p-1'>
                  {(['all', 'any'] as const).map((mode) => (
                    <Button
                      key={mode}
                      type='button'
                      size='sm'
                      variant={
                        props.value.match_mode === mode ? 'secondary' : 'ghost'
                      }
                      className='min-h-10 sm:min-h-8'
                      onClick={() =>
                        props.onChange({ ...props.value, match_mode: mode })
                      }
                    >
                      {t(mode === 'all' ? 'Match all rules' : 'Match any rule')}
                    </Button>
                  ))}
                </div>
              </div>

              <div className='space-y-2'>
                {props.value.rules.map((rule, index) => {
                  const emptyOperator =
                    rule.operator === 'is_empty' ||
                    rule.operator === 'is_not_empty'
                  const metricField = isMetricAccountFilterField(rule.field)
                  const timeField = isTimeAccountFilterField(rule.field)
                  let maxValues = 50
                  let limitMessage = '每条筛选规则最多包含 50 个值'
                  let valuePlaceholder = 'Enter one or more values'
                  let invalidMessage = 'Add at least one filter value'
                  if (metricField) {
                    maxValues = 1
                    limitMessage = '该指标只需要一个比较值'
                    valuePlaceholder = '输入指标值'
                    invalidMessage = '请输入一个有效的数值或时间'
                  }
                  if (rule.operator === 'between') {
                    maxValues = 2
                    limitMessage = '区间筛选需要两个值'
                    valuePlaceholder = '输入起始值和结束值'
                    invalidMessage = '请输入两个有效的区间值'
                  }
                  return (
                    <div
                      key={rule.id}
                      className='bg-background grid w-full min-w-0 gap-2 rounded-md border p-2.5 @lg/account-filter:grid-cols-2 @5xl/account-filter:grid-cols-[2.2rem_10rem_11rem_minmax(13rem,1fr)_9rem_2.75rem] @5xl/account-filter:items-start'
                    >
                      <span className='text-muted-foreground hidden pt-2 text-center text-sm tabular-nums @5xl/account-filter:block'>
                        {index + 1}
                      </span>
                      <div className='min-w-0 space-y-1'>
                        <Label className='text-xs @5xl/account-filter:sr-only'>
                          {t('Field')}
                        </Label>
                        <Select
                          value={rule.field}
                          onValueChange={(value) => {
                            if (!value) return
                            updateRule(rule.id, {
                              field: value,
                              operator: defaultOperatorFor(value),
                              values: [],
                              value_mode: 'any',
                            })
                          }}
                        >
                          <SelectTrigger className='min-h-11 w-full min-w-0 @5xl/account-filter:min-h-9'>
                            <SelectValue>
                              {t(fieldLabels[rule.field])}
                            </SelectValue>
                          </SelectTrigger>
                          <SelectContent>
                            {allowedFields.map((field) => (
                              <SelectItem key={field} value={field}>
                                {t(fieldLabels[field])}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                      <div className='min-w-0 space-y-1'>
                        <Label className='text-xs @5xl/account-filter:sr-only'>
                          {t('Operator')}
                        </Label>
                        <Select
                          value={rule.operator}
                          onValueChange={(operator) => {
                            if (!operator) return
                            updateRule(rule.id, {
                              operator,
                              values: valuesForOperator(
                                operator,
                                rule.values,
                                metricField
                              ),
                            })
                          }}
                        >
                          <SelectTrigger className='min-h-11 w-full min-w-0 @5xl/account-filter:min-h-9'>
                            <SelectValue>
                              {t(operatorLabels[rule.operator])}
                            </SelectValue>
                          </SelectTrigger>
                          <SelectContent>
                            {operatorsFor(rule.field).map((operator) => (
                              <SelectItem key={operator} value={operator}>
                                {t(operatorLabels[operator])}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                      <div className='min-w-0 space-y-1 @lg/account-filter:col-span-2 @5xl/account-filter:col-span-1'>
                        <Label className='text-xs @5xl/account-filter:sr-only'>
                          {t('Values')}
                        </Label>
                        {emptyOperator && (
                          <div className='text-muted-foreground flex min-h-11 items-center rounded-md border px-3 text-sm @5xl/account-filter:min-h-9'>
                            {t('No value required')}
                          </div>
                        )}
                        {!emptyOperator && timeField && (
                          <>
                            <div className='grid gap-2'>
                              {Array.from({
                                length: rule.operator === 'between' ? 2 : 1,
                              }).map((_, valueIndex) => {
                                const inputID = `account-filter-${rule.id}-${valueIndex}`
                                let inputLabel = t(fieldLabels[rule.field])
                                if (rule.operator === 'between') {
                                  inputLabel = t(
                                    valueIndex === 0 ? 'Start Time' : 'End Time'
                                  )
                                }
                                return (
                                  <div
                                    key={inputID}
                                    className='min-w-0 space-y-1'
                                  >
                                    <Label
                                      htmlFor={inputID}
                                      className='text-muted-foreground text-xs'
                                    >
                                      {inputLabel}
                                    </Label>
                                    <Input
                                      id={inputID}
                                      type='datetime-local'
                                      step={1}
                                      value={accountFilterDateTimeInputValue(
                                        rule.values[valueIndex]
                                      )}
                                      aria-invalid={
                                        !accountFilterDateTimeInputValue(
                                          rule.values[valueIndex]
                                        )
                                      }
                                      className='min-h-11 text-sm tabular-nums [color-scheme:light] @5xl/account-filter:min-h-9 dark:[color-scheme:dark]'
                                      onChange={(event) => {
                                        const values = Array.from(
                                          {
                                            length:
                                              rule.operator === 'between'
                                                ? 2
                                                : 1,
                                          },
                                          (_, index) => rule.values[index] ?? ''
                                        )
                                        values[valueIndex] =
                                          event.target.value.replace('T', ' ')
                                        updateRule(rule.id, { values })
                                      }}
                                    />
                                  </div>
                                )
                              })}
                            </div>
                            {!isAccountFilterRuleComplete(rule) && (
                              <p className='text-destructive text-xs'>
                                {t(invalidMessage)}
                              </p>
                            )}
                          </>
                        )}
                        {!emptyOperator && !timeField && (
                          <>
                            <MultiSelect
                              options={props.options[rule.field] ?? []}
                              selected={rule.values}
                              onChange={(values) =>
                                updateRule(rule.id, { values })
                              }
                              allowCreate={
                                TEXT_ACCOUNT_FILTER_FIELDS.has(rule.field) ||
                                metricField
                              }
                              maxVisibleChips={3}
                              maxValues={maxValues}
                              onLimitExceeded={() =>
                                toast.error(t(limitMessage))
                              }
                              placeholder={t(valuePlaceholder)}
                              className='min-h-11 min-w-0 @5xl/account-filter:min-h-9'
                            />
                            {TEXT_ACCOUNT_FILTER_FIELDS.has(rule.field) && (
                              <Button
                                type='button'
                                variant='ghost'
                                size='sm'
                                className='min-h-10'
                                onClick={() => {
                                  setBulkRuleID(rule.id)
                                  setBulkValues('')
                                }}
                              >
                                <ListPlus />
                                {t('批量添加')}
                              </Button>
                            )}
                            {!isAccountFilterRuleComplete(rule) && (
                              <p className='text-destructive text-xs'>
                                {t(invalidMessage)}
                              </p>
                            )}
                          </>
                        )}
                      </div>
                      <div className='min-w-0 space-y-1'>
                        <Label className='text-xs @5xl/account-filter:sr-only'>
                          {t('Value matching')}
                        </Label>
                        <Select
                          value={rule.value_mode}
                          disabled={emptyOperator || metricField}
                          onValueChange={(value) => {
                            if (!value) return
                            updateRule(rule.id, { value_mode: value })
                          }}
                        >
                          <SelectTrigger className='min-h-11 w-full min-w-0 @5xl/account-filter:min-h-9'>
                            <SelectValue>
                              {t(
                                rule.value_mode === 'all'
                                  ? 'All values'
                                  : 'Any value'
                              )}
                            </SelectValue>
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value='any'>
                              {t('Any value')}
                            </SelectItem>
                            <SelectItem value='all'>
                              {t('All values')}
                            </SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                      <Button
                        variant='ghost'
                        size='icon'
                        className='text-destructive min-h-11 min-w-11 justify-self-end @5xl/account-filter:min-h-9 @5xl/account-filter:min-w-9'
                        aria-label={t('Remove filter rule')}
                        title={t('Remove filter rule')}
                        onClick={() => removeRule(rule.id)}
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  )
                })}
              </div>

              <div className='flex flex-col-reverse gap-2 @md/account-filter:flex-row @md/account-filter:justify-between'>
                <Button
                  variant='ghost'
                  className='min-h-11'
                  disabled={props.value.rules.length === 0}
                  onClick={() =>
                    props.onChange({ match_mode: 'all', rules: [] })
                  }
                >
                  {t('Reset advanced filters')}
                </Button>
                <Button
                  variant='outline'
                  className='min-h-11'
                  disabled={props.value.rules.length >= 20}
                  onClick={() =>
                    props.onChange({
                      ...props.value,
                      rules: [...props.value.rules, createAccountFilterRule()],
                    })
                  }
                >
                  <Plus />
                  {t('Add filter rule')}
                </Button>
              </div>
            </div>
          </CollapsibleContent>
        </div>
      </Collapsible>

      <Dialog open={saveOpen} onOpenChange={setSaveOpen}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('Save filter template')}</DialogTitle>
            <DialogDescription>
              {t(
                'The template is private to your account and available across devices.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-2'>
            <Label htmlFor='account-filter-template-name'>
              {t('Template name')}
            </Label>
            <Input
              id='account-filter-template-name'
              value={templateName}
              maxLength={64}
              autoFocus
              onChange={(event) => setTemplateName(event.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setSaveOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={!templateName.trim() || createMutation.isPending}
              onClick={() => createMutation.mutate()}
            >
              {t('Save template')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete filter template?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This only deletes the saved template. Current filter rules remain active.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={() => deleteMutation.mutate()}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog
        open={bulkRuleID != null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setBulkRuleID(null)
            setBulkValues('')
          }
        }}
      >
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('批量添加筛选值')}</DialogTitle>
            <DialogDescription>
              {t('每行输入一个值，也支持英文逗号或中文逗号分隔。')}
            </DialogDescription>
          </DialogHeader>
          <Textarea
            value={bulkValues}
            onChange={(event) => setBulkValues(event.target.value)}
            className='min-h-64 resize-y font-mono'
            placeholder={'allen\nhh\njack\ncc\nchan'}
            autoFocus
          />
          <p className='text-muted-foreground text-xs'>
            {t('自动去除空值和大小写重复项；每条规则最多 50 个值。')}
          </p>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => {
                setBulkRuleID(null)
                setBulkValues('')
              }}
            >
              {t('Cancel')}
            </Button>
            <Button disabled={!bulkValues.trim()} onClick={applyBulkValues}>
              {t('添加筛选值')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
