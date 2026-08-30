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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { MultiSelect } from '@/components/multi-select'
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
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  type InstanceAlertRule,
  type InstanceAlertRuleInput,
  createInstanceAlertRule,
  deleteInstanceAlertRule,
  listInstanceAlertRules,
  updateInstanceAlertRule,
} from './api'

type InstanceOption = {
  id: number
  name: string
  kind?: string
  status?: string
}

const emptyRule = (): InstanceAlertRuleInput => ({
  name: '',
  description: '',
  enabled: true,
  alert_types: ['availability', 'credential'],
  check_interval_seconds: 60,
  failure_threshold: 0,
  recipients: [],
  instance_ids: [],
})

export function InstanceAlerts({ instances }: { instances: InstanceOption[] }) {
  const client = useQueryClient()
  const [editing, setEditing] = useState<InstanceAlertRule | null>(null)
  const [draft, setDraft] = useState<InstanceAlertRuleInput | null>(null)
  const [saving, setSaving] = useState(false)
  const query = useQuery({
    queryKey: ['instance-alert-rules'],
    queryFn: listInstanceAlertRules,
    refetchInterval: 60_000,
  })
  const instanceMap = useMemo(
    () => new Map(instances.map((instance) => [instance.id, instance])),
    [instances]
  )
  const options = instances.map((instance) => ({
    value: String(instance.id),
    label: `${instance.name} · ${instance.kind ?? 'unknown'}`,
  }))

  const openEditor = (rule?: InstanceAlertRule) => {
    setEditing(rule ?? null)
    setDraft(
      rule
        ? {
            name: rule.name,
            description: rule.description,
            enabled: rule.enabled,
            alert_types: [...rule.alert_types],
            check_interval_seconds: rule.check_interval_seconds,
            failure_threshold: rule.failure_threshold,
            recipients: [...rule.recipients],
            instance_ids: [...rule.instance_ids],
          }
        : emptyRule()
    )
  }

  const save = async () => {
    if (
      !draft?.name.trim() ||
      !draft.instance_ids.length ||
      !draft.alert_types.length
    ) {
      toast.error('请填写名称，并至少选择一个实例和一种故障类型')
      return
    }
    setSaving(true)
    try {
      if (editing) await updateInstanceAlertRule(editing.id, draft)
      else await createInstanceAlertRule(draft)
      toast.success(editing ? '实例预警规则已更新' : '实例预警规则已创建')
      setDraft(null)
      await client.invalidateQueries({ queryKey: ['instance-alert-rules'] })
    } catch (error: unknown) {
      const conflicts = (
        error as {
          response?: { data?: { data?: { instance_ids?: number[] } } }
        }
      ).response?.data?.data?.instance_ids
      if (conflicts?.length) {
        toast.error(
          `以下实例已属于其他启用规则：${conflicts.map((id) => instanceMap.get(id)?.name ?? `#${id}`).join('、')}`
        )
      } else toast.error('保存实例预警规则失败')
    } finally {
      setSaving(false)
    }
  }

  const toggle = async (rule: InstanceAlertRule) => {
    try {
      await updateInstanceAlertRule(rule.id, {
        ...rule,
        enabled: !rule.enabled,
      })
      await client.invalidateQueries({ queryKey: ['instance-alert-rules'] })
    } catch {
      toast.error('切换规则状态失败，实例可能已被其他规则占用')
    }
  }

  const remove = async (rule: InstanceAlertRule) => {
    if (!window.confirm(`确认删除“${rule.name}”？`)) return
    try {
      await deleteInstanceAlertRule(rule.id)
      toast.success('实例预警规则已删除')
      await client.invalidateQueries({ queryKey: ['instance-alert-rules'] })
    } catch {
      toast.error('删除实例预警规则失败')
    }
  }

  return (
    <div className='min-w-0 space-y-4'>
      <div className='flex flex-wrap justify-end gap-2'>
        <Button
          variant='outline'
          size='icon'
          aria-label='刷新实例预警规则'
          onClick={() => void query.refetch()}
        >
          <RefreshCw className={query.isFetching ? 'animate-spin' : ''} />
        </Button>
        <Button onClick={() => openEditor()}>
          <Plus /> 创建规则
        </Button>
      </div>

      <div className='hidden overflow-x-auto rounded-lg border md:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>规则</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>实例</TableHead>
              <TableHead>故障类型</TableHead>
              <TableHead>周期 / 阈值</TableHead>
              <TableHead>收件人</TableHead>
              <TableHead className='text-right'>操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(query.data?.data ?? []).map((rule) => (
              <TableRow key={rule.id}>
                <TableCell className='max-w-64 whitespace-normal'>
                  <div className='font-medium'>{rule.name}</div>
                  <div className='text-muted-foreground text-sm'>
                    {rule.description || '无说明'}
                  </div>
                </TableCell>
                <TableCell>
                  <Switch
                    checked={rule.enabled}
                    onCheckedChange={() => void toggle(rule)}
                  />
                </TableCell>
                <TableCell>{rule.instance_ids.length} 个</TableCell>
                <TableCell>{alertTypeText(rule.alert_types)}</TableCell>
                <TableCell>
                  {rule.check_interval_seconds} 秒 /{' '}
                  {rule.failure_threshold ||
                    `继承 ${rule.effective_failure_threshold}`}
                </TableCell>
                <TableCell className='max-w-64 whitespace-normal'>
                  {rule.recipients.length
                    ? rule.recipients.join('、')
                    : `继承全局（${rule.effective_recipients.length}）`}
                </TableCell>
                <TableCell>
                  <div className='flex justify-end gap-1'>
                    <Button
                      variant='ghost'
                      size='icon'
                      aria-label='编辑规则'
                      onClick={() => openEditor(rule)}
                    >
                      <Pencil />
                    </Button>
                    <Button
                      variant='ghost'
                      size='icon'
                      aria-label='删除规则'
                      onClick={() => void remove(rule)}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className='grid gap-3 md:hidden'>
        {(query.data?.data ?? []).map((rule) => (
          <div key={rule.id} className='rounded-lg border p-4'>
            <div className='flex items-start justify-between gap-3'>
              <div className='min-w-0'>
                <div className='font-medium break-words'>{rule.name}</div>
                <div className='text-muted-foreground mt-1 text-sm'>
                  {rule.instance_ids.length} 个实例 ·{' '}
                  {rule.check_interval_seconds} 秒
                </div>
              </div>
              <Badge variant={rule.enabled ? 'default' : 'secondary'}>
                {rule.enabled ? '启用' : '停用'}
              </Badge>
            </div>
            <div className='mt-3 text-sm'>
              {alertTypeText(rule.alert_types)} · 连续失败{' '}
              {rule.failure_threshold || rule.effective_failure_threshold} 次
            </div>
            <div className='mt-4 flex gap-2'>
              <Button
                className='min-h-11 flex-1'
                variant='outline'
                onClick={() => openEditor(rule)}
              >
                <Pencil />
                编辑
              </Button>
              <Button
                className='min-h-11'
                variant='outline'
                onClick={() => void toggle(rule)}
              >
                {rule.enabled ? '停用' : '启用'}
              </Button>
              <Button
                className='min-h-11'
                variant='ghost'
                size='icon'
                aria-label='删除规则'
                onClick={() => void remove(rule)}
              >
                <Trash2 />
              </Button>
            </div>
          </div>
        ))}
      </div>
      {!query.isLoading && !query.data?.data?.length && (
        <div className='text-muted-foreground rounded-lg border p-8 text-center'>
          暂无实例预警规则
        </div>
      )}

      <Dialog
        open={draft !== null}
        onOpenChange={(open) => !open && setDraft(null)}
      >
        <DialogContent className='max-h-[92dvh] max-w-2xl overflow-y-auto'>
          <DialogHeader>
            <DialogTitle>
              {editing ? '编辑实例预警规则' : '创建实例预警规则'}
            </DialogTitle>
            <DialogDescription>
              一条启用规则可管理多个实例，同一实例不能同时属于两条启用规则。
            </DialogDescription>
          </DialogHeader>
          {draft && (
            <div className='grid gap-4 sm:grid-cols-2'>
              <Field label='规则名称'>
                <Input
                  value={draft.name}
                  maxLength={128}
                  onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                />
              </Field>
              <Field label='巡检周期（秒）'>
                <Input
                  type='number'
                  min={10}
                  max={86400}
                  value={draft.check_interval_seconds}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      check_interval_seconds: Number(e.target.value),
                    })
                  }
                />
              </Field>
              <Field label='说明' className='sm:col-span-2'>
                <Input
                  value={draft.description}
                  onChange={(e) =>
                    setDraft({ ...draft, description: e.target.value })
                  }
                />
              </Field>
              <Field label='实例范围' className='sm:col-span-2'>
                <MultiSelect
                  options={options}
                  selected={draft.instance_ids.map(String)}
                  onChange={(values) =>
                    setDraft({ ...draft, instance_ids: values.map(Number) })
                  }
                  maxValues={100}
                  allowCreate={false}
                  placeholder='选择最多 100 个实例'
                />
              </Field>
              <Field label='故障类型' className='sm:col-span-2'>
                <div className='flex flex-wrap gap-5 rounded-md border p-3'>
                  <TypeCheckbox
                    label='实例不可用'
                    value='availability'
                    draft={draft}
                    setDraft={setDraft}
                  />
                  <TypeCheckbox
                    label='凭据异常'
                    value='credential'
                    draft={draft}
                    setDraft={setDraft}
                  />
                </div>
              </Field>
              <Field label='连续失败阈值（0 为继承全局）'>
                <Input
                  type='number'
                  min={0}
                  max={100}
                  value={draft.failure_threshold}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      failure_threshold: Number(e.target.value),
                    })
                  }
                />
              </Field>
              <Field label='收件人（留空继承全局）'>
                <MultiSelect
                  options={[]}
                  selected={draft.recipients}
                  onChange={(recipients) => setDraft({ ...draft, recipients })}
                  placeholder='输入邮箱后回车'
                />
              </Field>
              <div className='flex items-center justify-between rounded-md border p-3 sm:col-span-2'>
                <Label>启用规则</Label>
                <Switch
                  checked={draft.enabled}
                  onCheckedChange={(enabled) => setDraft({ ...draft, enabled })}
                />
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant='outline' onClick={() => setDraft(null)}>
              取消
            </Button>
            <Button disabled={saving} onClick={() => void save()}>
              {saving ? '保存中…' : '保存规则'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function Field({
  label,
  className = '',
  children,
}: {
  label: string
  className?: string
  children: ReactNode
}) {
  return (
    <div className={`min-w-0 space-y-2 ${className}`}>
      <Label>{label}</Label>
      {children}
    </div>
  )
}

function TypeCheckbox({
  label,
  value,
  draft,
  setDraft,
}: {
  label: string
  value: 'availability' | 'credential'
  draft: InstanceAlertRuleInput
  setDraft: (value: InstanceAlertRuleInput) => void
}) {
  const checked = draft.alert_types.includes(value)
  return (
    <label className='flex min-h-11 items-center gap-2'>
      <Checkbox
        checked={checked}
        onCheckedChange={(next) =>
          setDraft({
            ...draft,
            alert_types: next
              ? [...draft.alert_types, value]
              : draft.alert_types.filter((item) => item !== value),
          })
        }
      />
      {label}
    </label>
  )
}

function alertTypeText(values: string[]) {
  return values
    .map((value) => (value === 'credential' ? '凭据异常' : '实例不可用'))
    .join('、')
}
