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
import { ChevronDown, Filter, Plus, Save, Trash2 } from 'lucide-react'
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
import { cn } from '@/lib/utils'

import {
  ACCOUNT_FILTER_FIELDS,
  TEXT_ACCOUNT_FILTER_FIELDS,
  accountFilterFromTemplate,
  accountFilterTemplateInput,
  createAccountFilterRule,
  isAccountFilterRuleComplete,
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
}

const operatorLabels: Record<AccountFilterOperator, string> = {
  contains: 'Contains',
  not_contains: 'Does not contain',
  is: 'Is one of',
  is_not: 'Is not one of',
  is_empty: 'Is empty',
  is_not_empty: 'Is not empty',
}

function operatorsFor(field: AccountFilterField): AccountFilterOperator[] {
  return TEXT_ACCOUNT_FILTER_FIELDS.has(field)
    ? ['contains', 'not_contains', 'is_empty', 'is_not_empty']
    : ['is', 'is_not', 'is_empty', 'is_not_empty']
}

export function AccountFilterPanel(props: {
  value: AccountAdvancedFilter
  onChange: (value: AccountAdvancedFilter) => void
  options: Partial<Record<AccountFilterField, MultiSelectOption[]>>
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
  const templatesQuery = useQuery({
    queryKey: TEMPLATE_QUERY_KEY,
    queryFn: listAccountFilterTemplates,
  })
  const templates = templatesQuery.data?.data ?? []
  const selectedTemplate = templates.find(
    (template) => template.id === selectedTemplateID
  )
  const completeRules = props.value.rules.filter(isAccountFilterRuleComplete)
  const rulesValid =
    props.value.rules.length > 0 &&
    props.value.rules.every(isAccountFilterRuleComplete)

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

  return (
    <>
      <Collapsible open={open} onOpenChange={setOpen}>
        <div className='border-border/70 bg-muted/15 rounded-md border'>
          <div className='flex flex-col gap-2 p-2.5 sm:flex-row sm:flex-wrap sm:items-center'>
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
            <Select
              value={
                selectedTemplateID == null ? 'none' : String(selectedTemplateID)
              }
              onValueChange={applyTemplate}
            >
              <SelectTrigger className='min-h-11 w-full sm:min-h-9 sm:w-56'>
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
            <div className='flex flex-wrap gap-2 sm:ms-auto'>
              <Button
                variant='outline'
                size='sm'
                className='min-h-11 flex-1 sm:min-h-9 sm:flex-none'
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
                    className='min-h-11 sm:min-h-9'
                    disabled={!rulesValid || updateMutation.isPending}
                    onClick={() => updateMutation.mutate()}
                  >
                    {t('Update template')}
                  </Button>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    className='text-destructive min-h-11 min-w-11 sm:min-h-9 sm:min-w-9'
                    aria-label={t('Delete template')}
                    title={t('Delete template')}
                    onClick={() => setDeleteOpen(true)}
                  >
                    <Trash2 />
                  </Button>
                </>
              )}
            </div>
          </div>
          <CollapsibleContent>
            <div className='border-border/70 space-y-3 border-t p-3'>
              <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
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
                  return (
                    <div
                      key={rule.id}
                      className='bg-background grid gap-2 rounded-md border p-2.5 lg:grid-cols-[2.2rem_10rem_11rem_minmax(13rem,1fr)_9rem_2.75rem] lg:items-start'
                    >
                      <span className='text-muted-foreground hidden pt-2 text-center text-sm tabular-nums lg:block'>
                        {index + 1}
                      </span>
                      <div className='space-y-1'>
                        <Label className='text-xs lg:sr-only'>
                          {t('Field')}
                        </Label>
                        <Select
                          value={rule.field}
                          onValueChange={(value) => {
                            if (!value) return
                            updateRule(rule.id, {
                              field: value,
                              operator: TEXT_ACCOUNT_FILTER_FIELDS.has(value)
                                ? 'contains'
                                : 'is',
                              values: [],
                            })
                          }}
                        >
                          <SelectTrigger className='min-h-11 w-full lg:min-h-9'>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {ACCOUNT_FILTER_FIELDS.map((field) => (
                              <SelectItem key={field} value={field}>
                                {t(fieldLabels[field])}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                      <div className='space-y-1'>
                        <Label className='text-xs lg:sr-only'>
                          {t('Operator')}
                        </Label>
                        <Select
                          value={rule.operator}
                          onValueChange={(operator) => {
                            if (!operator) return
                            updateRule(rule.id, {
                              operator,
                              values:
                                operator === 'is_empty' ||
                                operator === 'is_not_empty'
                                  ? []
                                  : rule.values,
                            })
                          }}
                        >
                          <SelectTrigger className='min-h-11 w-full lg:min-h-9'>
                            <SelectValue />
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
                      <div className='space-y-1'>
                        <Label className='text-xs lg:sr-only'>
                          {t('Values')}
                        </Label>
                        {emptyOperator ? (
                          <div className='text-muted-foreground flex min-h-11 items-center rounded-md border px-3 text-sm lg:min-h-9'>
                            {t('No value required')}
                          </div>
                        ) : (
                          <>
                            <MultiSelect
                              options={props.options[rule.field] ?? []}
                              selected={rule.values}
                              onChange={(values) =>
                                updateRule(rule.id, {
                                  values: values.slice(0, 50),
                                })
                              }
                              allowCreate={TEXT_ACCOUNT_FILTER_FIELDS.has(
                                rule.field
                              )}
                              maxVisibleChips={3}
                              placeholder={t('Enter one or more values')}
                              className='min-h-11 lg:min-h-9'
                            />
                            {!isAccountFilterRuleComplete(rule) && (
                              <p className='text-destructive text-xs'>
                                {t('Add at least one filter value')}
                              </p>
                            )}
                          </>
                        )}
                      </div>
                      <div className='space-y-1'>
                        <Label className='text-xs lg:sr-only'>
                          {t('Value matching')}
                        </Label>
                        <Select
                          value={rule.value_mode}
                          disabled={emptyOperator}
                          onValueChange={(value) => {
                            if (!value) return
                            updateRule(rule.id, { value_mode: value })
                          }}
                        >
                          <SelectTrigger className='min-h-11 w-full lg:min-h-9'>
                            <SelectValue />
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
                        className='text-destructive min-h-11 min-w-11 lg:min-h-9 lg:min-w-9'
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

              <div className='flex flex-col-reverse gap-2 sm:flex-row sm:justify-between'>
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
    </>
  )
}
