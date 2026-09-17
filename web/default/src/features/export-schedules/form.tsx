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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { AccountFilterPanel } from '@/features/managed-accounts/account-filter-panel'
import {
  ACCOUNT_FILTER_FIELDS,
  accountFilterSnapshot,
} from '@/features/managed-accounts/account-filtering'
import { getManagedInstances } from '@/features/managed-instances/api'
import { useAdminDataAccess } from '@/hooks/use-admin-data-access'

import {
  listSchedules,
  saveSchedule,
  shanghaiInput,
  parseShanghaiInput,
  splitRecipients,
  type ExportSchedule,
  type ScheduleConfig,
} from './api'
import { scheduleMessage, useScheduleText } from './messages'

export function ScheduleForm({
  initial,
  id,
  onClose,
  onSaved,
}: {
  initial: Pick<ExportSchedule, 'name' | 'enabled' | 'version' | 'config'>
  id?: number
  onClose: () => void
  onSaved: () => void
}) {
  const text = useScheduleText()
  const { i18n } = useTranslation()
  const access = useAdminDataAccess()
  const [draft, setDraft] = useState(() => structuredClone(initial))
  const [recipients, setRecipients] = useState(
    initial.config.recipients.join('\n')
  )
  const [filter, setFilter] = useState(() => ({
    match_mode: initial.config.query.match_mode,
    rules: initial.config.query.rules.map((r, i) => ({
      ...r,
      id: `schedule-${i}`,
    })),
  }))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const smtp = useQuery({
    queryKey: ['export-schedule-smtp'],
    queryFn: () => listSchedules(1),
    retry: false,
  })
  const instances = useQuery({
    queryKey: ['usage-export-instances'],
    queryFn: () => getManagedInstances({ search: '', kind: '', status: '' }),
    staleTime: 60000,
  })
  const setConfig = (patch: Partial<ScheduleConfig>) =>
    setDraft((d) => ({ ...d, config: { ...d.config, ...patch } }))
  const setQuery = (patch: Partial<ScheduleConfig['query']>) =>
    setConfig({ query: { ...draft.config.query, ...patch } })
  const setSchedule = (patch: Partial<ScheduleConfig['schedule']>) =>
    setConfig({ schedule: { ...draft.config.schedule, ...patch } })
  const close = () => {
    if (busy) return
    const dirty =
      JSON.stringify(draft) !== JSON.stringify(initial) ||
      recipients !== initial.config.recipients.join('\n') ||
      JSON.stringify(accountFilterSnapshot(filter)) !==
        JSON.stringify({
          match_mode: initial.config.query.match_mode,
          rules: initial.config.query.rules,
        })
    if (
      !dirty ||
      window.confirm(text('放弃尚未保存的修改？', 'Discard unsaved changes?'))
    ) {
      onClose()
    }
  }
  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (busy) return
    setError('')
    setBusy(true)
    try {
      await saveSchedule(
        {
          ...draft,
          config: {
            ...draft.config,
            locale: i18n.language,
            recipients: splitRecipients(recipients),
            query: { ...draft.config.query, ...accountFilterSnapshot(filter) },
          },
        },
        id
      )
      toast.success(text('定时导出已保存', 'Schedule saved'))
      onSaved()
    } catch (e) {
      setError(scheduleMessage(e, text))
    } finally {
      setBusy(false)
    }
  }
  const s = draft.config.schedule
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close()
      }}
    >
      <DialogContent className='flex max-h-[90dvh] flex-col overflow-hidden sm:max-w-2xl'>
        <DialogHeader className='shrink-0'>
          <DialogTitle>{text('定时导出', 'Scheduled export')}</DialogTitle>
          <DialogDescription>
            {text(
              '北京时间。附件发送后无法撤回。',
              'Asia/Shanghai time. Sent attachments cannot be recalled.'
            )}
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(event) => void submit(event)}
          className='flex min-h-0 flex-1 flex-col gap-4'
        >
          <div className='min-h-0 flex-1 overflow-y-auto'>
            <fieldset disabled={busy} className='grid min-w-0 gap-4 px-1 pb-2'>
              <label className='grid gap-1 text-sm'>
                {text('任务名称', 'Name')}
                <Input
                  required
                  maxLength={128}
                  value={draft.name}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, name: e.target.value }))
                  }
                />
              </label>
              <div className='grid gap-3 sm:grid-cols-2'>
                <label className='grid gap-1 text-sm'>
                  {text('账号范围', 'Scope')}
                  <NativeSelect
                    value={draft.config.scope}
                    onChange={(e) =>
                      setConfig({
                        scope: e.target.value as ScheduleConfig['scope'],
                      })
                    }
                  >
                    <NativeSelectOption value='dynamic'>
                      {text('动态筛选', 'Dynamic filters')}
                    </NativeSelectOption>
                    <NativeSelectOption
                      value='fixed'
                      disabled={!draft.config.accounts.length}
                    >
                      {text('固定勾选账号', 'Fixed selection')} (
                      {draft.config.accounts.length})
                    </NativeSelectOption>
                  </NativeSelect>
                </label>
                <label className='grid gap-1 text-sm'>
                  {text('报表周期', 'Report period')}
                  <NativeSelect
                    value={draft.config.period.kind}
                    onChange={(e) =>
                      setConfig({
                        period: {
                          ...draft.config.period,
                          kind: e.target
                            .value as ScheduleConfig['period']['kind'],
                        },
                      })
                    }
                  >
                    {[
                      ['today', text('今天', 'Today')],
                      ['yesterday', text('昨天', 'Yesterday')],
                      ['last7', text('近 7 天', 'Last 7 days')],
                      ['last30', text('近 30 天', 'Last 30 days')],
                      ['fixed', text('固定日期', 'Fixed dates')],
                    ].map(([key, label]) => (
                      <NativeSelectOption key={key} value={key}>
                        {label}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </label>
              </div>
              {draft.config.period.kind === 'fixed' && (
                <div className='grid grid-cols-2 gap-3'>
                  {(['start_date', 'end_date'] as const).map((key) => (
                    <label key={key} className='grid gap-1 text-sm'>
                      {key === 'start_date'
                        ? text('开始日期', 'Start date')
                        : text('结束日期', 'End date')}
                      <Input
                        required
                        type='date'
                        value={draft.config.period[key] ?? ''}
                        onChange={(e) =>
                          setConfig({
                            period: {
                              ...draft.config.period,
                              [key]: e.target.value,
                            },
                          })
                        }
                      />
                    </label>
                  ))}
                </div>
              )}
              <fieldset className='grid gap-2'>
                <legend className='mb-2 text-sm font-medium'>
                  {text('实例范围', 'Instances')}
                </legend>
                <div className='grid max-h-36 gap-2 overflow-auto rounded-md border p-3 sm:grid-cols-2'>
                  {instances.data?.data.items
                    .filter((i) => access.hasInstance(i.id))
                    .map((i) => (
                      <label
                        key={i.id}
                        className='flex min-w-0 items-center gap-2 text-sm'
                      >
                        <Checkbox
                          disabled={draft.config.scope === 'fixed'}
                          checked={draft.config.query.instance_ids.includes(
                            i.id
                          )}
                          onCheckedChange={(checked) =>
                            setQuery({
                              instance_ids: checked
                                ? [...draft.config.query.instance_ids, i.id]
                                : draft.config.query.instance_ids.filter(
                                    (id) => id !== i.id
                                  ),
                            })
                          }
                        />
                        <span className='break-all'>{i.name}</span>
                      </label>
                    ))}
                </div>
              </fieldset>
              {draft.config.scope === 'dynamic' && (
                <>
                  <div className='grid gap-3 sm:grid-cols-2'>
                    <label className='grid gap-1 text-sm'>
                      {text('包含关键词', 'Include keywords')}
                      <Input
                        value={(draft.config.query.include_terms ?? []).join(
                          ', '
                        )}
                        onChange={(e) =>
                          setQuery({
                            include_terms: e.target.value
                              .split(/[,，\n]/)
                              .map((v) => v.trim())
                              .filter(Boolean),
                          })
                        }
                      />
                    </label>
                    <label className='grid gap-1 text-sm'>
                      {text('排除关键词', 'Exclude keywords')}
                      <Input
                        value={(draft.config.query.exclude_terms ?? []).join(
                          ', '
                        )}
                        onChange={(e) =>
                          setQuery({
                            exclude_terms: e.target.value
                              .split(/[,，\n]/)
                              .map((v) => v.trim())
                              .filter(Boolean),
                          })
                        }
                      />
                    </label>
                  </div>
                  <AccountFilterPanel
                    value={filter}
                    onChange={setFilter}
                    options={{}}
                    templatesEnabled={false}
                    allowedFields={ACCOUNT_FILTER_FIELDS.filter((f) =>
                      access.allows(f)
                    )}
                  />
                </>
              )}
              <div className='grid gap-3 sm:grid-cols-2'>
                <label className='grid gap-1 text-sm'>
                  {text('执行频率', 'Frequency')}
                  <NativeSelect
                    value={s.kind}
                    onChange={(e) =>
                      setSchedule({ kind: e.target.value as typeof s.kind })
                    }
                  >
                    {[
                      ['daily', text('每天', 'Daily')],
                      ['weekly', text('每周', 'Weekly')],
                      ['interval', text('每隔若干小时', 'Hourly interval')],
                    ].map(([key, label]) => (
                      <NativeSelectOption key={key} value={key}>
                        {label}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </label>
                {s.kind !== 'interval' ? (
                  <label className='grid gap-1 text-sm'>
                    {text('时间（北京时间）', 'Time (Asia/Shanghai)')}
                    <Input
                      type='time'
                      required
                      value={`${String(s.hour).padStart(2, '0')}:${String(s.minute).padStart(2, '0')}`}
                      onChange={(e) => {
                        const [hour, minute] = e.target.value
                          .split(':')
                          .map(Number)
                        setSchedule({ hour, minute })
                      }}
                    />
                  </label>
                ) : (
                  <label className='grid gap-1 text-sm'>
                    {text('间隔小时', 'Interval hours')}
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      required
                      value={s.interval_hours}
                      onChange={(e) =>
                        setSchedule({ interval_hours: Number(e.target.value) })
                      }
                    />
                  </label>
                )}
              </div>
              {s.kind === 'weekly' && (
                <label className='grid gap-1 text-sm'>
                  {text('星期', 'Weekday')}
                  <NativeSelect
                    value={s.weekday}
                    onChange={(e) =>
                      setSchedule({ weekday: Number(e.target.value) })
                    }
                  >
                    {[
                      text('周日', 'Sunday'),
                      text('周一', 'Monday'),
                      text('周二', 'Tuesday'),
                      text('周三', 'Wednesday'),
                      text('周四', 'Thursday'),
                      text('周五', 'Friday'),
                      text('周六', 'Saturday'),
                    ].map((day, index) => (
                      <NativeSelectOption key={day} value={index}>
                        {day}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </label>
              )}
              {s.kind === 'interval' && (
                <label className='grid gap-1 text-sm'>
                  {text(
                    '首次执行时间（北京时间）',
                    'First run (Asia/Shanghai)'
                  )}
                  <Input
                    type='datetime-local'
                    required
                    value={s.first_at ? shanghaiInput(s.first_at) : ''}
                    onChange={(e) =>
                      setSchedule({
                        first_at: parseShanghaiInput(e.target.value),
                      })
                    }
                  />
                </label>
              )}
              <label className='grid gap-1 text-sm'>
                {text(
                  '收件邮箱（逗号或换行分隔）',
                  'Recipients (comma or newline separated)'
                )}
                <Textarea
                  required
                  rows={3}
                  value={recipients}
                  onChange={(e) => setRecipients(e.target.value)}
                />
              </label>
              <Label className='flex items-center gap-2'>
                <Checkbox
                  checked={draft.enabled}
                  disabled={!smtp.data?.smtp_ready && !draft.enabled}
                  onCheckedChange={(value) =>
                    setDraft((d) => ({ ...d, enabled: value === true }))
                  }
                />
                {text('启用计划', 'Enable schedule')}
              </Label>
              {!smtp.data?.smtp_ready && (
                <p className='text-muted-foreground text-sm'>
                  {smtp.isError
                    ? text(
                        'SMTP 状态读取失败，仅可保存暂停计划。',
                        'SMTP status unavailable. Save paused only.'
                      )
                    : text(
                        'SMTP 未启用或未配置，仅可保存暂停计划。',
                        'SMTP is not enabled or configured. Save paused only.'
                      )}
                </p>
              )}
              {error && (
                <p role='alert' className='text-destructive text-sm'>
                  {error}
                </p>
              )}
            </fieldset>
          </div>
          <DialogFooter className='bg-popover shrink-0 border-t pt-3'>
            <Button
              type='button'
              variant='outline'
              disabled={busy}
              onClick={close}
            >
              {text('取消', 'Cancel')}
            </Button>
            <Button
              type='submit'
              disabled={busy || draft.config.query.instance_ids.length === 0}
            >
              {busy ? text('保存中…', 'Saving…') : text('保存', 'Save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
