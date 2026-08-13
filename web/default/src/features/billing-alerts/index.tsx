/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useQuery } from '@tanstack/react-query'
import {
  BellRing,
  CircleDollarSign,
  Mail,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  SlidersHorizontal,
  Trash2,
} from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { getManagedInstances } from '@/features/managed-instances/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  type BillingFilters,
  type BillingRule,
  type BillingRuleInput,
  type BillingTemplate,
  type BillingThreshold,
  type RuleImpact,
  type SMTPSetting,
  type TemplateImpact,
  createBillingRule,
  createBillingTemplate,
  deleteBillingRule,
  deleteBillingTemplate,
  evaluateBillingRule,
  getExchangeSettings,
  getSMTPSettings,
  listBillingRules,
  listBillingTemplates,
  listExchangeRates,
  previewBillingRule,
  previewBillingTemplate,
  refreshExchangeRate,
  testSMTPSettings,
  updateBillingRule,
  updateBillingTemplate,
  updateExchangeSettings,
  updateSMTPSettings,
} from './api'

const dateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'short',
})

const EMPTY_THRESHOLD: BillingThreshold = {
  id: -1,
  name: '提醒',
  severity: 'warning',
  currency: 'USD',
  amount: '',
  reminder_mode: 'once_per_cycle',
  repeat_interval_seconds: 3600,
  repeat_increment: '',
}

function parseJSON<T>(value: string, fallback: T): T {
  try {
    return JSON.parse(value) as T
  } catch {
    return fallback
  }
}

function normalizeFilters(text: string): BillingFilters {
  const lines = text.split('\n')
  const result: BillingFilters = {}
  for (const line of lines) {
    const [rawKey, ...parts] = line.split('=')
    const key = rawKey.trim()
    const values = parts
      .join('=')
      .split(',')
      .map((value) => value.trim())
      .filter(Boolean)
    if (key && values.length) result[key] = values
  }
  return result
}

function filterText(filters: BillingFilters) {
  return Object.entries(filters)
    .map(([key, values]) => `${key}=${values.join(', ')}`)
    .join('\n')
}

function defaultCycleConfig(type: string): Record<string, unknown> {
  const now = Math.floor(Date.now() / 1000)
  if (type === 'monthly_day') return { day_of_month: 1, hour: 0, minute: 0 }
  if (type === 'rolling_days') return { anchor: now, days: 30 }
  if (type === 'fixed') return { start: now, end: now + 30 * 24 * 3600 }
  return {}
}

function localDateTime(value: unknown) {
  const timestamp = Number(value)
  if (!timestamp) return ''
  const date = new Date(timestamp * 1000)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

function localTimestamp(value: string) {
  return Math.floor(new Date(value).getTime() / 1000)
}

function TemplateDialog({
  open,
  template,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  template: BillingTemplate | null
  onOpenChange: (open: boolean) => void
  onSaved: () => Promise<unknown>
}) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [filters, setFilters] = useState('')
  const [busy, setBusy] = useState(false)
  const [impact, setImpact] = useState<TemplateImpact | null>(null)
  const [previewSignature, setPreviewSignature] = useState('')

  useEffect(() => {
    if (!open) return
    setName(template?.name ?? '')
    setDescription(template?.description ?? '')
    setFilters(filterText(template?.filters ?? {}))
    setImpact(null)
    setPreviewSignature('')
  }, [open, template])

  const save = async () => {
    if (!name.trim()) return toast.error('请输入模板名称')
    setBusy(true)
    try {
      const input = {
        name: name.trim(),
        description: description.trim(),
        filters: normalizeFilters(filters),
      }
      const signature = JSON.stringify(input)
      if (template && (!impact || previewSignature !== signature)) {
        const preview = await previewBillingTemplate(template.id, input)
        setImpact(preview.data)
        setPreviewSignature(signature)
        return
      }
      if (template) await updateBillingTemplate(template.id, input)
      else await createBillingTemplate(input)
      toast.success(template ? '筛选模板已更新' : '筛选模板已创建')
      onOpenChange(false)
      await onSaved()
    } catch {
      toast.error('保存筛选模板失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>
            {template ? '编辑筛选模板' : '新建筛选模板'}
          </DialogTitle>
          <DialogDescription>
            每行填写一个筛选字段，多个值使用英文逗号分隔。
          </DialogDescription>
        </DialogHeader>
        <div className='grid gap-4'>
          <Field label='名称'>
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </Field>
          {impact && (
            <div className='rounded-md border border-amber-300 bg-amber-50/70 p-3 text-xs text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-100'>
              将生成 v{impact.next_version}，影响 {impact.rule_count} 条规则、
              {impact.instance_count} 个实例，并重置 {impact.reset_cycle_count}{' '}
              个账期的档位状态。
            </div>
          )}
          <Field label='说明'>
            <Input
              value={description}
              onChange={(event) => setDescription(event.target.value)}
            />
          </Field>
          <Field label='筛选条件'>
            <Textarea
              value={filters}
              onChange={(event) => setFilters(event.target.value)}
              placeholder={
                'username=alice, bob\nmodel_name=gpt-5\ngroup=default'
              }
              className='min-h-40 font-mono text-xs'
            />
          </Field>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button disabled={busy} onClick={() => void save()}>
            {impact ? '确认更新' : '保存模板'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function RuleDialog({
  open,
  rule,
  templates,
  instances,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  rule: BillingRule | null
  templates: BillingTemplate[]
  instances: { id: number; name: string; kind: string }[]
  onOpenChange: (open: boolean) => void
  onSaved: () => Promise<unknown>
}) {
  const [input, setInput] = useState<BillingRuleInput>(() => blankRule())
  const [recipientText, setRecipientText] = useState('')
  const [busy, setBusy] = useState(false)
  const [impact, setImpact] = useState<RuleImpact | null>(null)
  const [previewSignature, setPreviewSignature] = useState('')

  useEffect(() => {
    if (!open) return
    setImpact(null)
    setPreviewSignature('')
    if (rule) {
      setInput({
        ...rule,
        cycle_config: parseJSON(rule.cycle_config, {}),
        schedule_config: parseJSON(rule.schedule_config, { seconds: 300 }),
        thresholds: rule.thresholds.map((threshold) => ({ ...threshold })),
      })
      setRecipientText(rule.recipients.join(', '))
    } else {
      setInput(blankRule(templates[0]?.id))
      setRecipientText('')
    }
  }, [open, rule, templates])

  const update = <K extends keyof BillingRuleInput>(
    key: K,
    value: BillingRuleInput[K]
  ) => setInput((current) => ({ ...current, [key]: value }))

  const updateThreshold = (index: number, patch: Partial<BillingThreshold>) =>
    setInput((current) => ({
      ...current,
      thresholds: current.thresholds.map((item, itemIndex) =>
        itemIndex === index ? { ...item, ...patch } : item
      ),
    }))

  const save = async () => {
    const recipients = recipientText
      .split(',')
      .map((value) => value.trim())
      .filter(Boolean)
    if (
      !input.name.trim() ||
      !input.template_id ||
      !input.instance_ids.length ||
      !recipients.length
    ) {
      return toast.error('请完整填写名称、模板、实例和收件人')
    }
    if (input.thresholds.some((item) => !item.name.trim() || !item.amount)) {
      return toast.error('请完整填写预警档位')
    }
    setBusy(true)
    try {
      const payload = { ...input, recipients }
      const signature = JSON.stringify(payload)
      if (!impact || previewSignature !== signature) {
        const preview = await previewBillingRule(rule?.id ?? null, payload)
        setImpact(preview.data)
        setPreviewSignature(signature)
        return
      }
      if (rule) await updateBillingRule(rule.id, payload)
      else await createBillingRule(payload)
      toast.success(rule ? '预警规则已更新' : '预警规则已创建')
      onOpenChange(false)
      await onSaved()
    } catch {
      toast.error('保存预警规则失败，请检查配置')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-4xl'>
        <DialogHeader>
          <DialogTitle>{rule ? '编辑账单预警' : '新建账单预警'}</DialogTitle>
          <DialogDescription>
            每个所选实例独立统计并独立触发预警。
          </DialogDescription>
        </DialogHeader>
        <div className='grid gap-6'>
          <FormSection title='基本信息'>
            <div className='grid gap-4 sm:grid-cols-2'>
              <Field label='规则名称'>
                <Input
                  value={input.name}
                  onChange={(e) => update('name', e.target.value)}
                />
              </Field>
              <Field label='筛选模板'>
                <NativeSelect
                  value={String(input.template_id)}
                  onChange={(e) =>
                    update('template_id', Number(e.target.value))
                  }
                >
                  <NativeSelectOption value=''>请选择</NativeSelectOption>
                  {templates.map((template) => (
                    <NativeSelectOption
                      key={template.id}
                      value={String(template.id)}
                    >
                      {template.name}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
              <Field label='说明' className='sm:col-span-2'>
                <Input
                  value={input.description}
                  onChange={(e) => update('description', e.target.value)}
                />
              </Field>
            </div>
          </FormSection>
          {impact && (
            <div className='rounded-md border border-blue-300 bg-blue-50/70 p-3 text-xs text-blue-950 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-100'>
              即将绑定 {impact.instance_count} 个实例、{impact.threshold_count}{' '}
              个预警档位，使用筛选模板 v{impact.template_version}。
              {impact.reset_cycle_count > 0 &&
                ` 当前 ${impact.reset_cycle_count} 个账期状态将重置。`}
            </div>
          )}

          <FormSection title='实例绑定'>
            <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-3'>
              {instances.map((instance) => (
                <label
                  key={instance.id}
                  className='hover:bg-muted/50 flex items-center gap-3 rounded-md border px-3 py-2'
                >
                  <Checkbox
                    checked={input.instance_ids.includes(instance.id)}
                    onCheckedChange={(checked) =>
                      update(
                        'instance_ids',
                        checked
                          ? [...input.instance_ids, instance.id]
                          : input.instance_ids.filter(
                              (id) => id !== instance.id
                            )
                      )
                    }
                  />
                  <span className='min-w-0'>
                    <span className='block truncate font-medium'>
                      {instance.name}
                    </span>
                    <span className='text-muted-foreground text-xs'>
                      {instance.kind}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          </FormSection>

          <FormSection title='账期与金额'>
            <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
              <Field label='账期'>
                <NativeSelect
                  value={input.cycle_type}
                  onChange={(e) => {
                    const cycleType = e.target.value
                    setInput((current) => ({
                      ...current,
                      cycle_type: cycleType,
                      cycle_config: defaultCycleConfig(cycleType),
                    }))
                  }}
                >
                  <NativeSelectOption value='natural_day'>
                    自然日
                  </NativeSelectOption>
                  <NativeSelectOption value='natural_week'>
                    自然周
                  </NativeSelectOption>
                  <NativeSelectOption value='natural_month'>
                    自然月
                  </NativeSelectOption>
                  <NativeSelectOption value='monthly_day'>
                    每月指定日
                  </NativeSelectOption>
                  <NativeSelectOption value='rolling_days'>
                    每 N 天
                  </NativeSelectOption>
                  <NativeSelectOption value='fixed'>
                    固定起止时间
                  </NativeSelectOption>
                </NativeSelect>
              </Field>
              <Field label='时区'>
                <Input
                  value={input.timezone}
                  onChange={(e) => update('timezone', e.target.value)}
                />
              </Field>
              <Field label='折扣比例'>
                <Input
                  type='number'
                  step='0.01'
                  value={input.discount_rate}
                  onChange={(e) => update('discount_rate', e.target.value)}
                />
              </Field>
              <Field label='汇率模式'>
                <NativeSelect
                  value={input.exchange_mode}
                  onChange={(e) => update('exchange_mode', e.target.value)}
                >
                  <NativeSelectOption value='latest'>
                    最新汇率
                  </NativeSelectOption>
                  <NativeSelectOption value='record_date'>
                    记录日期汇率
                  </NativeSelectOption>
                  <NativeSelectOption value='cycle_fixed'>
                    账期固定汇率
                  </NativeSelectOption>
                  <NativeSelectOption value='manual'>
                    手工汇率
                  </NativeSelectOption>
                </NativeSelect>
              </Field>
              {input.exchange_mode === 'manual' && (
                <Field label='手工 USD/CNY'>
                  <Input
                    value={input.manual_exchange_rate}
                    onChange={(e) =>
                      update('manual_exchange_rate', e.target.value)
                    }
                  />
                </Field>
              )}
              <label className='flex items-center gap-2 pt-6 text-sm'>
                <Switch
                  checked={input.exchange_override}
                  onCheckedChange={(checked) =>
                    update('exchange_override', checked)
                  }
                />
                覆盖全局汇率策略
              </label>
              <label className='flex items-center gap-2 pt-6 text-sm'>
                <Switch
                  checked={input.enabled}
                  onCheckedChange={(checked) => update('enabled', checked)}
                />
                启用规则
              </label>
              {input.cycle_type === 'monthly_day' && (
                <>
                  <Field label='每月开始日'>
                    <Input
                      type='number'
                      min={1}
                      max={31}
                      value={Number(input.cycle_config.day_of_month ?? 1)}
                      onChange={(e) =>
                        update('cycle_config', {
                          ...input.cycle_config,
                          day_of_month: Number(e.target.value),
                        })
                      }
                    />
                  </Field>
                  <Field label='开始小时'>
                    <Input
                      type='number'
                      min={0}
                      max={23}
                      value={Number(input.cycle_config.hour ?? 0)}
                      onChange={(e) =>
                        update('cycle_config', {
                          ...input.cycle_config,
                          hour: Number(e.target.value),
                        })
                      }
                    />
                  </Field>
                  <Field label='开始分钟'>
                    <Input
                      type='number'
                      min={0}
                      max={59}
                      value={Number(input.cycle_config.minute ?? 0)}
                      onChange={(e) =>
                        update('cycle_config', {
                          ...input.cycle_config,
                          minute: Number(e.target.value),
                        })
                      }
                    />
                  </Field>
                </>
              )}
              {input.cycle_type === 'rolling_days' && (
                <>
                  <Field label='循环起点'>
                    <Input
                      type='datetime-local'
                      value={localDateTime(input.cycle_config.anchor)}
                      onChange={(e) =>
                        update('cycle_config', {
                          ...input.cycle_config,
                          anchor: localTimestamp(e.target.value),
                        })
                      }
                    />
                  </Field>
                  <Field label='循环天数'>
                    <Input
                      type='number'
                      min={1}
                      value={Number(input.cycle_config.days ?? 30)}
                      onChange={(e) =>
                        update('cycle_config', {
                          ...input.cycle_config,
                          days: Number(e.target.value),
                        })
                      }
                    />
                  </Field>
                </>
              )}
              {input.cycle_type === 'fixed' && (
                <>
                  <Field label='开始时间'>
                    <Input
                      type='datetime-local'
                      value={localDateTime(input.cycle_config.start)}
                      onChange={(e) =>
                        update('cycle_config', {
                          ...input.cycle_config,
                          start: localTimestamp(e.target.value),
                        })
                      }
                    />
                  </Field>
                  <Field label='结束时间'>
                    <Input
                      type='datetime-local'
                      value={localDateTime(input.cycle_config.end)}
                      onChange={(e) =>
                        update('cycle_config', {
                          ...input.cycle_config,
                          end: localTimestamp(e.target.value),
                        })
                      }
                    />
                  </Field>
                </>
              )}
            </div>
          </FormSection>

          <FormSection title='预警档位'>
            <div className='space-y-3'>
              {input.thresholds.map((threshold, index) => (
                <div
                  key={threshold.id}
                  className='grid gap-3 rounded-md border p-3 sm:grid-cols-2 lg:grid-cols-7'
                >
                  <Field label='名称'>
                    <Input
                      value={threshold.name}
                      onChange={(e) =>
                        updateThreshold(index, { name: e.target.value })
                      }
                    />
                  </Field>
                  <Field label='级别'>
                    <NativeSelect
                      value={threshold.severity}
                      onChange={(e) =>
                        updateThreshold(index, { severity: e.target.value })
                      }
                    >
                      <NativeSelectOption value='info'>提示</NativeSelectOption>
                      <NativeSelectOption value='warning'>
                        警告
                      </NativeSelectOption>
                      <NativeSelectOption value='critical'>
                        严重
                      </NativeSelectOption>
                    </NativeSelect>
                  </Field>
                  <Field label='币种'>
                    <NativeSelect
                      value={threshold.currency}
                      onChange={(e) =>
                        updateThreshold(index, {
                          currency: e.target.value as 'USD' | 'CNY',
                        })
                      }
                    >
                      <NativeSelectOption value='USD'>美元</NativeSelectOption>
                      <NativeSelectOption value='CNY'>
                        人民币
                      </NativeSelectOption>
                    </NativeSelect>
                  </Field>
                  <Field label='阈值'>
                    <Input
                      type='number'
                      value={threshold.amount}
                      onChange={(e) =>
                        updateThreshold(index, { amount: e.target.value })
                      }
                    />
                  </Field>
                  <Field label='提醒方式' className='lg:col-span-2'>
                    <NativeSelect
                      value={threshold.reminder_mode}
                      onChange={(e) =>
                        updateThreshold(index, {
                          reminder_mode: e.target
                            .value as BillingThreshold['reminder_mode'],
                        })
                      }
                    >
                      <NativeSelectOption value='once_per_cycle'>
                        每账期一次
                      </NativeSelectOption>
                      <NativeSelectOption value='repeat_interval'>
                        按时间重复
                      </NativeSelectOption>
                      <NativeSelectOption value='repeat_increment'>
                        按金额增量
                      </NativeSelectOption>
                    </NativeSelect>
                  </Field>
                  <div className='flex items-end'>
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      aria-label='删除档位'
                      disabled={input.thresholds.length === 1}
                      onClick={() =>
                        update(
                          'thresholds',
                          input.thresholds.filter((_, i) => i !== index)
                        )
                      }
                    >
                      <Trash2 />
                    </Button>
                  </div>
                  {threshold.reminder_mode === 'repeat_interval' && (
                    <Field label='重复间隔（分钟）'>
                      <Input
                        type='number'
                        value={threshold.repeat_interval_seconds / 60}
                        onChange={(e) =>
                          updateThreshold(index, {
                            repeat_interval_seconds:
                              Number(e.target.value) * 60,
                          })
                        }
                      />
                    </Field>
                  )}
                  {threshold.reminder_mode === 'repeat_increment' && (
                    <Field label='金额增量'>
                      <Input
                        type='number'
                        value={threshold.repeat_increment}
                        onChange={(e) =>
                          updateThreshold(index, {
                            repeat_increment: e.target.value,
                          })
                        }
                      />
                    </Field>
                  )}
                </div>
              ))}
              <Button
                variant='outline'
                size='sm'
                onClick={() =>
                  update('thresholds', [
                    ...input.thresholds,
                    { ...EMPTY_THRESHOLD, id: -Date.now() },
                  ])
                }
              >
                <Plus />
                添加档位
              </Button>
            </div>
          </FormSection>

          <FormSection title='计划与通知'>
            <div className='grid gap-4 sm:grid-cols-2'>
              <Field label='执行计划'>
                <NativeSelect
                  value={input.schedule_type}
                  onChange={(e) => {
                    const type = e.target.value
                    setInput((current) => ({
                      ...current,
                      schedule_type: type,
                      schedule_config:
                        type === 'interval'
                          ? { seconds: 300 }
                          : { times: ['09:00'] },
                    }))
                  }}
                >
                  <NativeSelectOption value='interval'>
                    指定间隔
                  </NativeSelectOption>
                  <NativeSelectOption value='fixed_times'>
                    每日固定时间
                  </NativeSelectOption>
                </NativeSelect>
              </Field>
              {input.schedule_type === 'interval' ? (
                <Field label='统计间隔（分钟）'>
                  <Input
                    type='number'
                    min={1}
                    max={1440}
                    value={Number(input.schedule_config.seconds ?? 300) / 60}
                    onChange={(e) =>
                      update('schedule_config', {
                        seconds: Number(e.target.value) * 60,
                      })
                    }
                  />
                </Field>
              ) : (
                <Field label='每日时间（逗号分隔）'>
                  <Input
                    value={
                      Array.isArray(input.schedule_config.times)
                        ? input.schedule_config.times.join(', ')
                        : ''
                    }
                    onChange={(e) =>
                      update('schedule_config', {
                        times: e.target.value
                          .split(',')
                          .map((value) => value.trim())
                          .filter(Boolean),
                      })
                    }
                    placeholder='09:00, 17:30'
                  />
                </Field>
              )}
              <Field label='连续失败通知次数'>
                <Input
                  type='number'
                  value={input.failure_threshold}
                  onChange={(e) =>
                    update('failure_threshold', Number(e.target.value))
                  }
                />
              </Field>
              <Field label='收件邮箱（逗号分隔）' className='sm:col-span-2'>
                <Input
                  value={recipientText}
                  onChange={(e) => setRecipientText(e.target.value)}
                  placeholder='ops@example.com, finance@example.com'
                />
              </Field>
            </div>
          </FormSection>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button disabled={busy} onClick={() => void save()}>
            {impact ? '确认保存' : '预览影响'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function blankRule(templateId = 0): BillingRuleInput {
  return {
    name: '',
    description: '',
    template_id: templateId,
    enabled: true,
    timezone: 'Asia/Shanghai',
    cycle_type: 'natural_month',
    cycle_config: {},
    discount_rate: '1',
    exchange_mode: 'latest',
    manual_exchange_rate: '',
    exchange_override: false,
    schedule_type: 'interval',
    schedule_config: { seconds: 300 },
    recipients: [],
    failure_threshold: 3,
    instance_ids: [],
    thresholds: [{ ...EMPTY_THRESHOLD }],
  }
}

function Field({
  label,
  className,
  children,
}: {
  label: string
  className?: string
  children: ReactNode
}) {
  return (
    <div className={className}>
      <Label className='mb-1.5 block text-xs'>{label}</Label>
      {children}
    </div>
  )
}

function FormSection({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <section>
      <h3 className='mb-3 text-sm font-semibold'>{title}</h3>
      {children}
    </section>
  )
}

export function BillingAlerts() {
  const user = useAuthStore((state) => state.auth.user)
  const isRoot = user?.role === ROLE.SUPER_ADMIN
  const [templateOpen, setTemplateOpen] = useState(false)
  const [ruleOpen, setRuleOpen] = useState(false)
  const [editingTemplate, setEditingTemplate] =
    useState<BillingTemplate | null>(null)
  const [editingRule, setEditingRule] = useState<BillingRule | null>(null)
  const [deleteTemplate, setDeleteTemplate] = useState<BillingTemplate | null>(
    null
  )
  const [deleteRule, setDeleteRule] = useState<BillingRule | null>(null)

  const templatesQuery = useQuery({
    queryKey: ['billing-templates'],
    queryFn: listBillingTemplates,
  })
  const rulesQuery = useQuery({
    queryKey: ['billing-rules'],
    queryFn: listBillingRules,
    refetchInterval: 60_000,
  })
  const instancesQuery = useQuery({
    queryKey: ['billing-instances'],
    queryFn: () => getManagedInstances({ search: '', kind: '', status: '' }),
    staleTime: 60_000,
  })
  const templates = templatesQuery.data?.data ?? []
  const rules = rulesQuery.data?.data ?? []
  const instances = useMemo(
    () => instancesQuery.data?.data.items ?? [],
    [instancesQuery.data?.data.items]
  )
  const instanceMap = useMemo(
    () => new Map(instances.map((item) => [item.id, item])),
    [instances]
  )

  const removeTemplate = async () => {
    if (!deleteTemplate) return
    try {
      await deleteBillingTemplate(deleteTemplate.id)
      toast.success('筛选模板已删除')
      setDeleteTemplate(null)
      await templatesQuery.refetch()
    } catch {
      toast.error('模板正在被规则使用，无法删除')
    }
  }
  const removeRule = async () => {
    if (!deleteRule) return
    try {
      await deleteBillingRule(deleteRule.id)
      toast.success('预警规则已删除')
      setDeleteRule(null)
      await rulesQuery.refetch()
    } catch {
      toast.error('删除预警规则失败')
    }
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>账单预警</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='min-w-0'>
          <Tabs defaultValue='rules' className='gap-5'>
            <TabsList
              variant='line'
              className='max-w-full justify-start overflow-x-auto'
            >
              <TabsTrigger value='rules'>
                <BellRing />
                预警规则
              </TabsTrigger>
              <TabsTrigger value='templates'>
                <SlidersHorizontal />
                筛选模板
              </TabsTrigger>
              {isRoot && (
                <TabsTrigger value='exchange'>
                  <CircleDollarSign />
                  汇率设置
                </TabsTrigger>
              )}
              {isRoot && (
                <TabsTrigger value='smtp'>
                  <Mail />
                  邮件设置
                </TabsTrigger>
              )}
            </TabsList>
            <TabsContent value='rules'>
              <div className='mb-3 flex justify-end'>
                {isRoot && (
                  <Button
                    size='sm'
                    onClick={() => {
                      setEditingRule(null)
                      setRuleOpen(true)
                    }}
                  >
                    <Plus />
                    新建规则
                  </Button>
                )}
              </div>
              <div className='overflow-x-auto rounded-lg border'>
                <Table>
                  <TableHeader className='bg-muted/40'>
                    <TableRow>
                      <TableHead>状态</TableHead>
                      <TableHead>规则</TableHead>
                      <TableHead>账期 / 汇率</TableHead>
                      <TableHead>实例</TableHead>
                      <TableHead>档位</TableHead>
                      <TableHead>收件人</TableHead>
                      <TableHead className='text-right'>操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rules.map((rule) => (
                      <TableRow key={rule.id}>
                        <TableCell>
                          <Badge
                            variant={rule.enabled ? 'default' : 'secondary'}
                          >
                            {rule.enabled ? '运行中' : '已停用'}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <div className='font-medium'>{rule.name}</div>
                          <div className='text-muted-foreground text-xs'>
                            #{rule.id}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div>{rule.cycle_type}</div>
                          <div className='text-muted-foreground text-xs'>
                            {rule.exchange_override
                              ? rule.exchange_mode
                              : '跟随全局'}{' '}
                            · {rule.timezone}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className='flex max-w-72 flex-wrap gap-1'>
                            {rule.instance_ids.map((id) => (
                              <Badge key={id} variant='outline'>
                                {instanceMap.get(id)?.name ?? `#${id}`}
                              </Badge>
                            ))}
                          </div>
                        </TableCell>
                        <TableCell className='tabular-nums'>
                          {rule.thresholds
                            .map((item) => `${item.currency} ${item.amount}`)
                            .join(' / ')}
                        </TableCell>
                        <TableCell>{rule.recipients.length} 个</TableCell>
                        <TableCell>
                          <div className='flex justify-end gap-1'>
                            {rule.instance_ids[0] && (
                              <Button
                                variant='ghost'
                                size='icon-sm'
                                aria-label='立即统计'
                                onClick={async () => {
                                  try {
                                    await evaluateBillingRule(
                                      rule.id,
                                      rule.instance_ids[0]
                                    )
                                    toast.success('已加入统计队列')
                                  } catch {
                                    toast.error('提交统计失败')
                                  }
                                }}
                              >
                                <Play />
                              </Button>
                            )}
                            {isRoot && (
                              <>
                                <Button
                                  variant='ghost'
                                  size='icon-sm'
                                  aria-label='编辑规则'
                                  onClick={() => {
                                    setEditingRule(rule)
                                    setRuleOpen(true)
                                  }}
                                >
                                  <Pencil />
                                </Button>
                                <Button
                                  variant='ghost'
                                  size='icon-sm'
                                  aria-label='删除规则'
                                  onClick={() => setDeleteRule(rule)}
                                >
                                  <Trash2 />
                                </Button>
                              </>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                    {!rules.length && (
                      <TableRow>
                        <TableCell
                          colSpan={7}
                          className='text-muted-foreground h-32 text-center'
                        >
                          暂无预警规则
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>
            </TabsContent>
            <TabsContent value='templates'>
              <div className='mb-3 flex justify-end'>
                {isRoot && (
                  <Button
                    size='sm'
                    onClick={() => {
                      setEditingTemplate(null)
                      setTemplateOpen(true)
                    }}
                  >
                    <Plus />
                    新建模板
                  </Button>
                )}
              </div>
              <div className='overflow-x-auto rounded-lg border'>
                <Table>
                  <TableHeader className='bg-muted/40'>
                    <TableRow>
                      <TableHead>名称</TableHead>
                      <TableHead>版本</TableHead>
                      <TableHead>筛选条件</TableHead>
                      <TableHead>更新时间</TableHead>
                      <TableHead className='text-right'>操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {templates.map((template) => (
                      <TableRow key={template.id}>
                        <TableCell>
                          <div className='font-medium'>{template.name}</div>
                          <div className='text-muted-foreground text-xs'>
                            {template.description || '—'}
                          </div>
                        </TableCell>
                        <TableCell>v{template.current_version}</TableCell>
                        <TableCell>
                          <div className='flex flex-wrap gap-1'>
                            {Object.entries(template.filters)
                              .slice(0, 4)
                              .map(([key, values]) => (
                                <Badge key={key} variant='outline'>
                                  {key} · {values.length}
                                </Badge>
                              ))}
                          </div>
                        </TableCell>
                        <TableCell>
                          {dateTime.format(
                            new Date(template.updated_at * 1000)
                          )}
                        </TableCell>
                        <TableCell>
                          <div className='flex justify-end gap-1'>
                            {isRoot && (
                              <>
                                <Button
                                  variant='ghost'
                                  size='icon-sm'
                                  aria-label='编辑模板'
                                  onClick={() => {
                                    setEditingTemplate(template)
                                    setTemplateOpen(true)
                                  }}
                                >
                                  <Pencil />
                                </Button>
                                <Button
                                  variant='ghost'
                                  size='icon-sm'
                                  aria-label='删除模板'
                                  onClick={() => setDeleteTemplate(template)}
                                >
                                  <Trash2 />
                                </Button>
                              </>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                    {!templates.length && (
                      <TableRow>
                        <TableCell
                          colSpan={5}
                          className='text-muted-foreground h-32 text-center'
                        >
                          暂无筛选模板
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>
            </TabsContent>
            <TabsContent value='exchange'>
              <ExchangeSettings />
            </TabsContent>
            <TabsContent value='smtp'>
              <SMTPSettings />
            </TabsContent>
          </Tabs>
          <TemplateDialog
            open={templateOpen}
            template={editingTemplate}
            onOpenChange={setTemplateOpen}
            onSaved={() => templatesQuery.refetch()}
          />
          <RuleDialog
            open={ruleOpen}
            rule={editingRule}
            templates={templates}
            instances={instances}
            onOpenChange={setRuleOpen}
            onSaved={() => rulesQuery.refetch()}
          />
          <ConfirmDialog
            open={Boolean(deleteTemplate)}
            onOpenChange={(open) => !open && setDeleteTemplate(null)}
            title='删除筛选模板？'
            desc='仅未被规则使用的模板可以删除。'
            destructive
            confirmText='删除'
            handleConfirm={() => void removeTemplate()}
          />
          <ConfirmDialog
            open={Boolean(deleteRule)}
            onOpenChange={(open) => !open && setDeleteRule(null)}
            title='删除预警规则？'
            desc='规则配置将被删除，已经生成的预警记录仍会保留。此操作不可撤销。'
            destructive
            confirmText='删除'
            handleConfirm={() => void removeRule()}
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function ExchangeSettings() {
  const settingQuery = useQuery({
    queryKey: ['exchange-settings'],
    queryFn: getExchangeSettings,
  })
  const ratesQuery = useQuery({
    queryKey: ['exchange-rates'],
    queryFn: listExchangeRates,
  })
  const setting = settingQuery.data?.data
  const [mode, setMode] = useState('latest')
  const [automatic, setAutomatic] = useState(true)
  const [manualRate, setManualRate] = useState('')
  const [times, setTimes] = useState('17:30')
  useEffect(() => {
    if (setting) {
      setMode(setting.default_mode)
      setAutomatic(setting.automatic)
      setManualRate(setting.manual_rate)
      setTimes(parseJSON<string[]>(setting.update_times, ['17:30']).join(', '))
    }
  }, [setting])
  const save = async () => {
    try {
      await updateExchangeSettings({
        automatic,
        default_mode: mode,
        manual_rate: manualRate,
        update_times: times
          .split(',')
          .map((v) => v.trim())
          .filter(Boolean),
        timezone: 'Asia/Shanghai',
      })
      toast.success('汇率设置已保存')
      await settingQuery.refetch()
    } catch {
      toast.error('保存汇率设置失败')
    }
  }
  const refresh = async () => {
    try {
      await refreshExchangeRate()
      toast.success('参考汇率已更新')
      await Promise.all([settingQuery.refetch(), ratesQuery.refetch()])
    } catch {
      toast.error('公开汇率数据源暂时不可用')
    }
  }
  return (
    <div className='space-y-6'>
      <section className='grid gap-4 border-b pb-6 sm:grid-cols-2 lg:grid-cols-4'>
        <Field label='全局默认策略'>
          <NativeSelect value={mode} onChange={(e) => setMode(e.target.value)}>
            <NativeSelectOption value='latest'>最新汇率</NativeSelectOption>
            <NativeSelectOption value='record_date'>
              记录日期汇率
            </NativeSelectOption>
            <NativeSelectOption value='cycle_fixed'>
              账期固定汇率
            </NativeSelectOption>
            <NativeSelectOption value='manual'>手工汇率</NativeSelectOption>
          </NativeSelect>
        </Field>
        {mode === 'manual' && (
          <Field label='手工 USD/CNY'>
            <Input
              value={manualRate}
              onChange={(e) => setManualRate(e.target.value)}
            />
          </Field>
        )}
        <Field label='每日更新时间'>
          <Input
            value={times}
            onChange={(e) => setTimes(e.target.value)}
            placeholder='09:00, 17:30'
          />
        </Field>
        <label className='flex items-center gap-2 pt-6 text-sm'>
          <Switch checked={automatic} onCheckedChange={setAutomatic} />
          自动更新
        </label>
        <div className='flex gap-2 sm:col-span-2 lg:col-span-4'>
          <Button onClick={() => void save()}>保存设置</Button>
          <Button variant='outline' onClick={() => void refresh()}>
            <RefreshCw />
            立即获取
          </Button>
        </div>
      </section>
      <div className='overflow-x-auto rounded-lg border'>
        <Table>
          <TableHeader className='bg-muted/40'>
            <TableRow>
              <TableHead>观测日期</TableHead>
              <TableHead>USD/CNY</TableHead>
              <TableHead>来源</TableHead>
              <TableHead>获取时间</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(ratesQuery.data?.data ?? []).map((rate) => (
              <TableRow key={rate.id}>
                <TableCell>{rate.observed_date}</TableCell>
                <TableCell className='font-medium tabular-nums'>
                  {rate.rate}
                </TableCell>
                <TableCell>
                  <Badge variant={rate.fallback ? 'secondary' : 'outline'}>
                    {rate.source}
                    {rate.fallback ? ' · 备用' : ''}
                  </Badge>
                </TableCell>
                <TableCell>
                  {dateTime.format(new Date(rate.fetched_at * 1000))}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function SMTPSettings() {
  const query = useQuery({
    queryKey: ['smtp-settings'],
    queryFn: getSMTPSettings,
  })
  const [form, setForm] = useState<SMTPSetting & { password: string }>({
    id: 1,
    host: '',
    port: 587,
    security: 'starttls',
    username: '',
    password: '',
    password_stored: false,
    from_name: '',
    from_address: '',
    reply_to: '',
    enabled: false,
  })
  const [testRecipient, setTestRecipient] = useState('')
  useEffect(() => {
    if (query.data?.data) setForm({ ...query.data.data, password: '' })
  }, [query.data])
  const set = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) =>
    setForm((current) => ({ ...current, [key]: value }))
  const save = async () => {
    try {
      await updateSMTPSettings(form)
      toast.success('邮件设置已保存')
      set('password', '')
      await query.refetch()
    } catch {
      toast.error('保存邮件设置失败')
    }
  }
  const test = async () => {
    try {
      await testSMTPSettings(testRecipient)
      toast.success('测试邮件已发送')
    } catch {
      toast.error('测试邮件发送失败，请检查 SMTP 配置')
    }
  }
  return (
    <div className='grid gap-6 lg:grid-cols-[minmax(0,1fr)_320px]'>
      <section className='grid gap-4 sm:grid-cols-2'>
        <Field label='SMTP 主机'>
          <Input
            value={form.host}
            onChange={(e) => set('host', e.target.value)}
          />
        </Field>
        <Field label='端口'>
          <Input
            type='number'
            value={form.port}
            onChange={(e) => set('port', Number(e.target.value))}
          />
        </Field>
        <Field label='安全方式'>
          <NativeSelect
            value={form.security}
            onChange={(e) => set('security', e.target.value)}
          >
            <NativeSelectOption value='starttls'>STARTTLS</NativeSelectOption>
            <NativeSelectOption value='tls'>TLS / SSL</NativeSelectOption>
            <NativeSelectOption value='none'>无加密</NativeSelectOption>
          </NativeSelect>
        </Field>
        <Field label='用户名'>
          <Input
            value={form.username}
            onChange={(e) => set('username', e.target.value)}
          />
        </Field>
        <Field label={form.password_stored ? '密码（留空保持不变）' : '密码'}>
          <Input
            type='password'
            value={form.password}
            onChange={(e) => set('password', e.target.value)}
          />
        </Field>
        <Field label='发件人名称'>
          <Input
            value={form.from_name}
            onChange={(e) => set('from_name', e.target.value)}
          />
        </Field>
        <Field label='发件邮箱'>
          <Input
            value={form.from_address}
            onChange={(e) => set('from_address', e.target.value)}
          />
        </Field>
        <Field label='回复邮箱'>
          <Input
            value={form.reply_to}
            onChange={(e) => set('reply_to', e.target.value)}
          />
        </Field>
        <label className='flex items-center gap-2 text-sm'>
          <Switch
            checked={form.enabled}
            onCheckedChange={(value) => set('enabled', value)}
          />
          启用邮件发送
        </label>
        <div className='sm:col-span-2'>
          <Button onClick={() => void save()}>保存邮件设置</Button>
        </div>
      </section>
      <aside className='bg-muted/30 h-fit rounded-lg border p-4'>
        <h3 className='mb-1 font-medium'>发送测试邮件</h3>
        <p className='text-muted-foreground mb-4 text-xs'>
          先保存设置，再验证连接、认证与投递。
        </p>
        <div className='space-y-3'>
          <Input
            value={testRecipient}
            onChange={(e) => setTestRecipient(e.target.value)}
            placeholder='recipient@example.com'
          />
          <Button
            variant='outline'
            className='w-full'
            disabled={!testRecipient}
            onClick={() => void test()}
          >
            <Mail />
            发送测试
          </Button>
        </div>
      </aside>
    </div>
  )
}
