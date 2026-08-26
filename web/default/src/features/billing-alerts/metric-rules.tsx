/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  CircleAlert,
  Pencil,
  Play,
  Plus,
  Trash2,
  X,
} from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
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
import { DropdownMenuItem } from '@/components/ui/dropdown-menu'
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
import { Textarea } from '@/components/ui/textarea'
import type { ManagedInstance } from '@/features/managed-instances/types'
import { cn } from '@/lib/utils'

import {
  type MetricAlertCondition,
  type MetricAlertRule,
  type MetricAlertRuleInput,
  createMetricAlertRule,
  deleteMetricAlertRule,
  evaluateMetricAlertRule,
  listMetricAlertCapabilities,
  listMetricAlertRules,
  updateMetricAlertRule,
} from './api'

const EMPTY_CONDITION: MetricAlertCondition = {
  id: -1,
  metric: '',
  operator: 'lt',
  threshold: '',
  recovery_threshold: '',
}

const OPERATOR_LABELS: Record<MetricAlertCondition['operator'], string> = {
  gt: '大于',
  gte: '大于等于',
  lt: '小于',
  lte: '小于等于',
  eq: '等于',
  ne: '不等于',
}

const KIND_LABELS: Record<string, string> = {
  new_api: 'New API',
  huichuan: '汇川',
  sub2api: 'Sub2API',
  conductor: 'Conductor',
  claude_gateway: 'Claude Gateway',
  generic: '通用',
}

const METRIC_LABELS: Record<string, string> = {
  rpm: 'RPM',
  rpm_capacity: 'RPM 最大容量',
  rpm_utilization: 'RPM 容量使用率',
  accounts_available: '可用账号',
  accounts_total: '全部账号',
  accounts_availability: '账号可用率',
  concurrency_used: '当前并发',
  concurrency_max: '最大并发',
  concurrency_utilization: '并发使用率',
  success_rate: '成功率',
  active_sessions: '活跃会话',
  today_cost: '今日费用',
  instance_connected: '实例连接状态',
  unhealthy_instances: '异常实例数',
}

function emptyInput(): MetricAlertRuleInput {
  return {
    name: '',
    description: '',
    enabled: true,
    scope_mode: 'per_instance',
    match_mode: 'all',
    evaluation_interval_seconds: 60,
    trigger_count: 3,
    recovery_count: 2,
    failure_threshold: 3,
    reminder_mode: 'once',
    repeat_interval_seconds: 3600,
    recipients: [],
    instance_ids: [],
    conditions: [{ ...EMPTY_CONDITION }],
  }
}

function toInput(rule: MetricAlertRule): MetricAlertRuleInput {
  return {
    name: rule.name,
    description: rule.description,
    enabled: rule.enabled,
    scope_mode: rule.scope_mode,
    match_mode: rule.match_mode,
    evaluation_interval_seconds: rule.evaluation_interval_seconds,
    trigger_count: rule.trigger_count,
    recovery_count: rule.recovery_count,
    failure_threshold: rule.failure_threshold,
    reminder_mode: rule.reminder_mode,
    repeat_interval_seconds: rule.repeat_interval_seconds,
    recipients: [...rule.recipients],
    instance_ids: [...rule.instance_ids],
    conditions: rule.conditions.map((condition) => ({
      id: condition.id,
      metric: condition.metric,
      operator: condition.operator,
      threshold: condition.threshold,
      recovery_threshold: condition.recovery_threshold,
    })),
  }
}

function formatTime(timestamp: number) {
  if (!timestamp) return '尚未执行'
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'short',
    timeStyle: 'medium',
  }).format(new Date(timestamp * 1000))
}

function ruleStatus(rule: MetricAlertRule) {
  if (!rule.enabled) {
    return {
      label: '已停用',
      className: 'border-muted-foreground/25 bg-muted text-muted-foreground',
    }
  }
  if (rule.states.some((state) => state.active)) {
    return {
      label: '预警中',
      className:
        'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/50 dark:text-red-300',
    }
  }
  if (rule.states.some((state) => state.consecutive_failures > 0)) {
    return {
      label: '数据异常',
      className:
        'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900 dark:bg-amber-950/50 dark:text-amber-300',
    }
  }
  return {
    label: '监控中',
    className:
      'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-300',
  }
}

function MetricRuleDialog({
  open,
  rule,
  instances,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  rule: MetricAlertRule | null
  instances: ManagedInstance[]
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const [form, setForm] = useState<MetricAlertRuleInput>(emptyInput)
  const [recipientText, setRecipientText] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    const next = rule ? toInput(rule) : emptyInput()
    setForm(next)
    setRecipientText(next.recipients.join(', '))
  }, [open, rule])

  const capabilitiesQuery = useQuery({
    queryKey: ['metric-alert-capabilities', form.instance_ids, form.scope_mode],
    queryFn: () =>
      listMetricAlertCapabilities(form.instance_ids, form.scope_mode),
    enabled: open && form.instance_ids.length > 0,
  })
  const capabilities = useMemo(
    () => capabilitiesQuery.data?.data ?? [],
    [capabilitiesQuery.data?.data]
  )
  const capabilityMap = useMemo(
    () => new Map(capabilities.map((item) => [item.key, item])),
    [capabilities]
  )
  const metricOptions = useMemo(() => {
    const result = [...capabilities]
    for (const condition of form.conditions) {
      if (
        condition.metric &&
        !result.some((item) => item.key === condition.metric)
      ) {
        result.push({
          key: condition.metric,
          label: condition.metric,
          unit: '',
          kinds: [],
          aggregatable: true,
        })
      }
    }
    return result
  }, [capabilities, form.conditions])

  const updateCondition = (
    index: number,
    patch: Partial<MetricAlertCondition>
  ) => {
    setForm((current) => ({
      ...current,
      conditions: current.conditions.map((condition, conditionIndex) =>
        conditionIndex === index ? { ...condition, ...patch } : condition
      ),
    }))
  }

  const save = async () => {
    const recipients = recipientText
      .split(/[;,\n]/)
      .map((value) => value.trim())
      .filter(Boolean)
    const input = { ...form, recipients }
    if (!input.name.trim()) return toast.error('请输入规则名称')
    if (!input.instance_ids.length) return toast.error('请选择至少一个实例')
    if (
      !input.conditions.length ||
      input.conditions.some((item) => !item.metric || item.threshold === '')
    ) {
      return toast.error('请完整填写预警条件')
    }
    if (!recipients.length) return toast.error('请填写至少一个收件邮箱')
    setSaving(true)
    try {
      if (rule) await updateMetricAlertRule(rule.id, input)
      else await createMetricAlertRule(input)
      toast.success(rule ? '指标预警已更新' : '指标预警已创建')
      onOpenChange(false)
      onSaved()
    } catch {
      toast.error('保存指标预警失败，请检查实例和条件配置')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] overflow-y-auto sm:max-h-[92vh] sm:max-w-4xl'>
        <DialogHeader>
          <DialogTitle>{rule ? '编辑指标预警' : '新建指标预警'}</DialogTitle>
          <DialogDescription>
            基于服务端缓存指标持续检查，数据缺失不会被当作零值。
          </DialogDescription>
        </DialogHeader>

        <div className='grid gap-5 py-1'>
          <section className='grid gap-3'>
            <div className='flex items-center justify-between'>
              <h3 className='text-sm font-semibold'>基本信息</h3>
              <div className='flex items-center gap-2'>
                <Label htmlFor='metric-enabled'>启用规则</Label>
                <Switch
                  id='metric-enabled'
                  checked={form.enabled}
                  onCheckedChange={(enabled) => setForm({ ...form, enabled })}
                />
              </div>
            </div>
            <div className='grid gap-3 md:grid-cols-2'>
              <div className='grid gap-1.5'>
                <Label htmlFor='metric-name'>名称</Label>
                <Input
                  id='metric-name'
                  value={form.name}
                  onChange={(event) =>
                    setForm({ ...form, name: event.target.value })
                  }
                  placeholder='例如：可用账号不足'
                />
              </div>
              <div className='grid gap-1.5'>
                <Label htmlFor='metric-recipients'>收件邮箱</Label>
                <Input
                  id='metric-recipients'
                  value={recipientText}
                  onChange={(event) => setRecipientText(event.target.value)}
                  placeholder='ops@example.com, owner@example.com'
                />
              </div>
            </div>
            <div className='grid gap-1.5'>
              <Label htmlFor='metric-description'>说明</Label>
              <Textarea
                id='metric-description'
                value={form.description}
                onChange={(event) =>
                  setForm({ ...form, description: event.target.value })
                }
                placeholder='记录这条规则的用途和处理方式'
              />
            </div>
          </section>

          <section className='grid gap-3 border-t pt-5'>
            <h3 className='text-sm font-semibold'>监控范围</h3>
            <div className='grid gap-3 md:grid-cols-2'>
              <div className='grid gap-1.5'>
                <Label>统计方式</Label>
                <NativeSelect
                  className='w-full'
                  value={form.scope_mode}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      scope_mode: event.target
                        .value as MetricAlertRuleInput['scope_mode'],
                    })
                  }
                >
                  <NativeSelectOption value='per_instance'>
                    每个实例独立预警
                  </NativeSelectOption>
                  <NativeSelectOption value='aggregate'>
                    所选实例汇总预警
                  </NativeSelectOption>
                </NativeSelect>
              </div>
              <div className='grid gap-1.5'>
                <Label>条件关系</Label>
                <NativeSelect
                  className='w-full'
                  value={form.match_mode}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      match_mode: event.target
                        .value as MetricAlertRuleInput['match_mode'],
                    })
                  }
                >
                  <NativeSelectOption value='all'>
                    全部满足（AND）
                  </NativeSelectOption>
                  <NativeSelectOption value='any'>
                    任一满足（OR）
                  </NativeSelectOption>
                </NativeSelect>
              </div>
            </div>
            <div className='rounded-lg border'>
              <div className='border-b px-3 py-2 text-sm font-medium'>
                选择实例
              </div>
              <div className='grid max-h-48 gap-1 overflow-y-auto p-2 sm:grid-cols-2'>
                {instances.map((instance) => {
                  const checked = form.instance_ids.includes(instance.id)
                  return (
                    <label
                      key={instance.id}
                      className='hover:bg-muted flex cursor-pointer items-center gap-2 rounded-md px-2 py-2 text-sm'
                    >
                      <Checkbox
                        checked={checked}
                        onCheckedChange={(next) =>
                          setForm((current) => ({
                            ...current,
                            instance_ids: next
                              ? [...current.instance_ids, instance.id]
                              : current.instance_ids.filter(
                                  (id) => id !== instance.id
                                ),
                          }))
                        }
                      />
                      <span className='min-w-0 flex-1 truncate'>
                        {instance.name}
                      </span>
                      <Badge variant='outline' className='font-normal'>
                        {KIND_LABELS[instance.kind] ?? instance.kind}
                      </Badge>
                    </label>
                  )
                })}
              </div>
            </div>
          </section>

          <section className='grid gap-3 border-t pt-5'>
            <div className='flex items-center justify-between gap-3'>
              <div>
                <h3 className='text-sm font-semibold'>触发条件</h3>
                <p className='text-muted-foreground mt-1 text-xs'>
                  指标选项会根据所选实例的实际能力自动变化。
                </p>
              </div>
              <Button
                type='button'
                size='sm'
                variant='outline'
                disabled={!capabilities.length}
                onClick={() =>
                  setForm((current) => ({
                    ...current,
                    conditions: [
                      ...current.conditions,
                      { ...EMPTY_CONDITION, id: -Date.now() },
                    ],
                  }))
                }
              >
                <Plus />
                添加条件
              </Button>
            </div>
            {!form.instance_ids.length && (
              <div className='flex items-center gap-2 rounded-lg border border-blue-200 bg-blue-50 px-3 py-2 text-sm text-blue-700 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300'>
                <CircleAlert className='size-4' />
                选择实例后即可选择该平台支持的指标。
              </div>
            )}
            {form.conditions.map((condition, index) => {
              const capability = capabilityMap.get(condition.metric)
              return (
                <div
                  key={condition.id}
                  className='grid items-end gap-2 rounded-lg border p-3 md:grid-cols-[1.4fr_1fr_1fr_1fr_auto]'
                >
                  <div className='grid gap-1.5'>
                    <Label>指标</Label>
                    <NativeSelect
                      className='w-full'
                      value={condition.metric}
                      disabled={
                        !form.instance_ids.length || capabilitiesQuery.isLoading
                      }
                      onChange={(event) =>
                        updateCondition(index, { metric: event.target.value })
                      }
                    >
                      <NativeSelectOption value=''>
                        请选择指标
                      </NativeSelectOption>
                      {metricOptions.map((option) => (
                        <NativeSelectOption key={option.key} value={option.key}>
                          {option.label}
                          {option.unit ? `（${option.unit}）` : ''}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </div>
                  <div className='grid gap-1.5'>
                    <Label>比较</Label>
                    <NativeSelect
                      className='w-full'
                      value={condition.operator}
                      onChange={(event) =>
                        updateCondition(index, {
                          operator: event.target
                            .value as MetricAlertCondition['operator'],
                        })
                      }
                    >
                      {Object.entries(OPERATOR_LABELS).map(([value, label]) => (
                        <NativeSelectOption key={value} value={value}>
                          {label}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </div>
                  <div className='grid gap-1.5'>
                    <Label>触发阈值 {capability?.unit}</Label>
                    <Input
                      inputMode='decimal'
                      value={condition.threshold}
                      onChange={(event) =>
                        updateCondition(index, {
                          threshold: event.target.value,
                        })
                      }
                      placeholder='0'
                    />
                  </div>
                  <div className='grid gap-1.5'>
                    <Label>恢复阈值 {capability?.unit}</Label>
                    <Input
                      inputMode='decimal'
                      value={condition.recovery_threshold}
                      onChange={(event) =>
                        updateCondition(index, {
                          recovery_threshold: event.target.value,
                        })
                      }
                      placeholder='留空则使用触发阈值'
                    />
                  </div>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    aria-label='删除条件'
                    disabled={form.conditions.length === 1}
                    onClick={() =>
                      setForm((current) => ({
                        ...current,
                        conditions: current.conditions.filter(
                          (_, itemIndex) => itemIndex !== index
                        ),
                      }))
                    }
                  >
                    <X />
                  </Button>
                </div>
              )
            })}
          </section>

          <section className='grid gap-3 border-t pt-5'>
            <h3 className='text-sm font-semibold'>检查与通知</h3>
            <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
              <div className='grid gap-1.5'>
                <Label>检查间隔</Label>
                <NativeSelect
                  className='w-full'
                  value={String(form.evaluation_interval_seconds)}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      evaluation_interval_seconds: Number(
                        event.target.value
                      ) as MetricAlertRuleInput['evaluation_interval_seconds'],
                    })
                  }
                >
                  <NativeSelectOption value='10'>10 秒</NativeSelectOption>
                  <NativeSelectOption value='30'>30 秒</NativeSelectOption>
                  <NativeSelectOption value='60'>60 秒</NativeSelectOption>
                  <NativeSelectOption value='300'>5 分钟</NativeSelectOption>
                </NativeSelect>
              </div>
              <div className='grid gap-1.5'>
                <Label>连续触发次数</Label>
                <Input
                  type='number'
                  min={1}
                  max={100}
                  value={form.trigger_count}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      trigger_count: Number(event.target.value),
                    })
                  }
                />
              </div>
              <div className='grid gap-1.5'>
                <Label>连续恢复次数</Label>
                <Input
                  type='number'
                  min={1}
                  max={100}
                  value={form.recovery_count}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      recovery_count: Number(event.target.value),
                    })
                  }
                />
              </div>
              <div className='grid gap-1.5'>
                <Label>数据失败通知次数</Label>
                <Input
                  type='number'
                  min={1}
                  max={100}
                  value={form.failure_threshold}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      failure_threshold: Number(event.target.value),
                    })
                  }
                />
              </div>
            </div>
            <div className='grid gap-3 md:grid-cols-2'>
              <div className='grid gap-1.5'>
                <Label>重复提醒</Label>
                <NativeSelect
                  className='w-full'
                  value={form.reminder_mode}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      reminder_mode: event.target
                        .value as MetricAlertRuleInput['reminder_mode'],
                    })
                  }
                >
                  <NativeSelectOption value='once'>
                    同一故障仅提醒一次
                  </NativeSelectOption>
                  <NativeSelectOption value='repeat_interval'>
                    按固定间隔重复提醒
                  </NativeSelectOption>
                </NativeSelect>
              </div>
              <div className='grid gap-1.5'>
                <Label>重复间隔（分钟）</Label>
                <Input
                  type='number'
                  min={1}
                  disabled={form.reminder_mode !== 'repeat_interval'}
                  value={Math.max(
                    1,
                    Math.round(form.repeat_interval_seconds / 60)
                  )}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      repeat_interval_seconds: Number(event.target.value) * 60,
                    })
                  }
                />
              </div>
            </div>
          </section>
        </div>

        <DialogFooter className='bg-background sticky bottom-0 -mx-6 -mb-6 border-t px-6 py-4'>
          <Button
            variant='outline'
            onClick={() => onOpenChange(false)}
            disabled={saving}
          >
            取消
          </Button>
          <Button onClick={() => void save()} disabled={saving}>
            {saving ? '保存中…' : '保存规则'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function MetricAlertRules({
  instances,
  isRoot,
}: {
  instances: ManagedInstance[]
  isRoot: boolean
}) {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<MetricAlertRule | null>(null)
  const [deleting, setDeleting] = useState<MetricAlertRule | null>(null)
  const query = useQuery({
    queryKey: ['metric-alert-rules'],
    queryFn: listMetricAlertRules,
    refetchInterval: 30_000,
  })
  const rules = query.data?.data ?? []
  const instanceMap = useMemo(
    () => new Map(instances.map((instance) => [instance.id, instance])),
    [instances]
  )

  const remove = async () => {
    if (!deleting) return
    try {
      await deleteMetricAlertRule(deleting.id)
      toast.success('指标预警已删除')
      setDeleting(null)
      await query.refetch()
    } catch {
      toast.error('删除指标预警失败')
    }
  }

  return (
    <div className='grid gap-3'>
      <div className='flex items-center justify-between gap-3'>
        <div className='text-muted-foreground text-sm'>
          基于服务端实时缓存独立或汇总监控多个实例。
        </div>
        {isRoot && (
          <Button
            size='sm'
            onClick={() => {
              setEditing(null)
              setOpen(true)
            }}
          >
            <Plus />
            新建指标预警
          </Button>
        )}
      </div>
      <Accordion className='divide-border divide-y overflow-hidden rounded-lg border md:hidden'>
        {rules.map((rule) => {
          const status = ruleStatus(rule)
          const activeCount = rule.states.filter((state) => state.active).length
          return (
            <AccordionItem
              key={rule.id}
              value={String(rule.id)}
              className={cn(
                'border-l-2',
                activeCount ? 'border-l-red-500' : 'border-l-border'
              )}
            >
              <div className='flex min-w-0 items-stretch'>
                <AccordionTrigger className='min-h-24 min-w-0 flex-1 gap-3 rounded-none px-3 py-3 hover:no-underline'>
                  <div className='min-w-0 flex-1'>
                    <div className='flex min-w-0 flex-wrap items-center gap-2'>
                      <span className='min-w-0 font-medium break-words'>
                        {rule.name}
                      </span>
                      <Badge variant='outline' className={status.className}>
                        {status.label}
                      </Badge>
                    </div>
                    <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs'>
                      <span>#{rule.id}</span>
                      <span>{rule.instance_ids.length} 个实例</span>
                      <span>{rule.conditions.length} 个条件</span>
                      {activeCount > 0 && (
                        <span className='text-red-600 dark:text-red-400'>
                          {activeCount} 个范围正在预警
                        </span>
                      )}
                    </div>
                  </div>
                </AccordionTrigger>
                {isRoot && (
                  <div className='flex shrink-0 items-center gap-1 pe-2'>
                    <Button
                      variant='ghost'
                      size='icon'
                      className='size-11'
                      aria-label='编辑规则'
                      onClick={() => {
                        setEditing(rule)
                        setOpen(true)
                      }}
                    >
                      <Pencil />
                    </Button>
                    <DataTableRowActionMenu
                      ariaLabel='更多规则操作'
                      triggerClassName='size-11'
                    >
                      <DropdownMenuItem
                        onClick={async () => {
                          try {
                            await evaluateMetricAlertRule(rule.id)
                            toast.success('已加入检查队列')
                          } catch {
                            toast.error('提交检查失败')
                          }
                        }}
                      >
                        <Play />
                        立即检查
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        variant='destructive'
                        onClick={() => setDeleting(rule)}
                      >
                        <Trash2 />
                        删除规则
                      </DropdownMenuItem>
                    </DataTableRowActionMenu>
                  </div>
                )}
              </div>
              <AccordionContent className='px-3 pb-4'>
                <div className='bg-muted/35 grid gap-3 rounded-md p-3 min-[420px]:grid-cols-2'>
                  <MobileRuleDetail label='监控范围'>
                    {rule.scope_mode === 'aggregate' ? '汇总监控' : '独立监控'}
                    <div className='mt-1 flex flex-wrap gap-1'>
                      {rule.instance_ids.map((id) => (
                        <Badge
                          key={id}
                          variant='outline'
                          className='font-normal'
                        >
                          {instanceMap.get(id)?.name ?? `#${id}`}
                        </Badge>
                      ))}
                    </div>
                  </MobileRuleDetail>
                  <MobileRuleDetail label='触发条件'>
                    <div className='grid gap-1'>
                      {rule.conditions.map((condition, index) => (
                        <div
                          key={condition.id ?? index}
                          className='break-words'
                        >
                          {METRIC_LABELS[condition.metric] ?? condition.metric}{' '}
                          {OPERATOR_LABELS[condition.operator]}{' '}
                          {condition.threshold}
                        </div>
                      ))}
                    </div>
                    <span className='text-muted-foreground mt-1 block text-xs'>
                      {rule.match_mode === 'all' ? '全部满足' : '任一满足'}
                    </span>
                  </MobileRuleDetail>
                  <MobileRuleDetail label='检查策略'>
                    每 {rule.evaluation_interval_seconds} 秒
                    <span className='text-muted-foreground mt-1 block text-xs'>
                      触发 {rule.trigger_count} 次 · 恢复 {rule.recovery_count}{' '}
                      次
                    </span>
                  </MobileRuleDetail>
                  <MobileRuleDetail label='最近检查'>
                    {formatTime(rule.last_evaluated_at)}
                  </MobileRuleDetail>
                  <MobileRuleDetail label='收件人'>
                    <div className='break-words'>
                      {rule.recipients.join(', ') || '—'}
                    </div>
                  </MobileRuleDetail>
                </div>
              </AccordionContent>
            </AccordionItem>
          )
        })}
        {!rules.length && (
          <div className='text-muted-foreground grid min-h-36 content-center justify-items-center gap-2 text-sm'>
            <Activity className='size-6' />
            暂无指标预警规则
          </div>
        )}
      </Accordion>
      <div className='hidden overflow-x-auto rounded-lg border md:block'>
        <Table>
          <TableHeader className='bg-muted/40'>
            <TableRow>
              <TableHead>状态</TableHead>
              <TableHead>规则</TableHead>
              <TableHead>范围</TableHead>
              <TableHead>条件</TableHead>
              <TableHead>检查策略</TableHead>
              <TableHead>最近检查</TableHead>
              <TableHead className='text-right'>操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rules.map((rule) => {
              const status = ruleStatus(rule)
              const activeCount = rule.states.filter(
                (state) => state.active
              ).length
              return (
                <TableRow
                  key={rule.id}
                  className={
                    activeCount ? 'bg-red-50/30 dark:bg-red-950/10' : undefined
                  }
                >
                  <TableCell>
                    <Badge variant='outline' className={status.className}>
                      {status.label}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <div className='font-medium'>{rule.name}</div>
                    <div className='text-muted-foreground text-xs'>
                      #{rule.id} · {rule.recipients.length} 个收件人
                    </div>
                  </TableCell>
                  <TableCell>
                    <div>
                      {rule.scope_mode === 'aggregate'
                        ? '汇总监控'
                        : '独立监控'}
                    </div>
                    <div className='text-muted-foreground mt-1 flex max-w-64 flex-wrap gap-1 text-xs'>
                      {rule.instance_ids.map((id) => (
                        <Badge
                          key={id}
                          variant='outline'
                          className='font-normal'
                        >
                          {instanceMap.get(id)?.name ?? `#${id}`}
                        </Badge>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className='grid gap-1 text-sm'>
                      {rule.conditions.map((condition, index) => (
                        <div
                          key={condition.id ?? index}
                          className='whitespace-nowrap'
                        >
                          {METRIC_LABELS[condition.metric] ?? condition.metric}{' '}
                          {OPERATOR_LABELS[condition.operator]}{' '}
                          {condition.threshold}
                        </div>
                      ))}
                    </div>
                    <div className='text-muted-foreground mt-1 text-xs'>
                      {rule.match_mode === 'all' ? '全部满足' : '任一满足'}
                    </div>
                  </TableCell>
                  <TableCell className='tabular-nums'>
                    <div>每 {rule.evaluation_interval_seconds} 秒</div>
                    <div className='text-muted-foreground text-xs'>
                      触发 {rule.trigger_count} 次 · 恢复 {rule.recovery_count}{' '}
                      次
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className='whitespace-nowrap'>
                      {formatTime(rule.last_evaluated_at)}
                    </div>
                    {activeCount > 0 && (
                      <div className='mt-1 text-xs text-red-600 dark:text-red-400'>
                        {activeCount} 个范围正在预警
                      </div>
                    )}
                  </TableCell>
                  <TableCell>
                    <div className='flex justify-end gap-1'>
                      {isRoot && (
                        <>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label='立即检查'
                            title='立即检查'
                            onClick={async () => {
                              try {
                                await evaluateMetricAlertRule(rule.id)
                                toast.success('已加入检查队列')
                              } catch {
                                toast.error('提交检查失败')
                              }
                            }}
                          >
                            <Play />
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label='编辑规则'
                            title='编辑规则'
                            onClick={() => {
                              setEditing(rule)
                              setOpen(true)
                            }}
                          >
                            <Pencil />
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label='删除规则'
                            title='删除规则'
                            onClick={() => setDeleting(rule)}
                          >
                            <Trash2 />
                          </Button>
                        </>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              )
            })}
            {!rules.length && (
              <TableRow>
                <TableCell
                  colSpan={7}
                  className='text-muted-foreground h-36 text-center'
                >
                  <div className='grid justify-items-center gap-2'>
                    <Activity className='size-6' />
                    暂无指标预警规则
                  </div>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      <MetricRuleDialog
        open={open}
        rule={editing}
        instances={instances}
        onOpenChange={setOpen}
        onSaved={() => void query.refetch()}
      />
      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(next) => !next && setDeleting(null)}
        title='删除指标预警？'
        desc='规则和当前状态将被删除，已经产生的预警记录仍会保留。'
        destructive
        confirmText='删除'
        handleConfirm={() => void remove()}
      />
    </div>
  )
}

function MobileRuleDetail(props: { label: string; children: ReactNode }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 text-sm break-words tabular-nums'>
        {props.children}
      </div>
    </div>
  )
}
