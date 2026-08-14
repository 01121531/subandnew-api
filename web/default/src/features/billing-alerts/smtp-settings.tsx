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
import { MailCheck, Save, Send, ShieldAlert } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'

import {
  type SMTPSetting,
  getSMTPSettings,
  testSMTPSettings,
  updateSMTPSettings,
} from './api'

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <Label className='mb-1.5 block text-xs'>{label}</Label>
      {children}
    </div>
  )
}

export function SMTPSettings() {
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
    alert_recipients: '',
    instance_alert_failure_threshold: 3,
    enabled: false,
  })
  const [testRecipient, setTestRecipient] = useState('')
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)

  useEffect(() => {
    if (query.data?.data) setForm({ ...query.data.data, password: '' })
  }, [query.data])

  const set = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) =>
    setForm((current) => ({ ...current, [key]: value }))

  const save = async () => {
    setSaving(true)
    try {
      await updateSMTPSettings(form)
      toast.success('巡检通知设置已保存')
      set('password', '')
      await query.refetch()
    } catch {
      toast.error('保存巡检通知设置失败')
    } finally {
      setSaving(false)
    }
  }

  const test = async () => {
    setTesting(true)
    try {
      await testSMTPSettings(testRecipient)
      toast.success('测试邮件已发送')
    } catch {
      toast.error('测试邮件发送失败，请检查 SMTP 配置')
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className='overflow-hidden rounded-lg border'>
      <section className='border-b p-4 sm:p-5'>
        <div className='mb-5 flex items-start gap-3'>
          <div className='bg-info/10 text-info flex size-9 shrink-0 items-center justify-center rounded-md'>
            <MailCheck className='size-4' />
          </div>
          <div>
            <h3 className='text-sm font-semibold'>SMTP 发信服务</h3>
            <p className='text-muted-foreground mt-1 text-xs'>
              账单预警和实例巡检共用此发信账号。
            </p>
          </div>
        </div>
        <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
          <Field label='SMTP 主机'>
            <Input
              value={form.host}
              onChange={(e) => set('host', e.target.value)}
              placeholder='smtp.example.com'
            />
          </Field>
          <Field label='端口'>
            <Input
              type='number'
              min={1}
              max={65535}
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
              autoComplete='new-password'
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
              type='email'
              value={form.from_address}
              onChange={(e) => set('from_address', e.target.value)}
            />
          </Field>
          <Field label='回复邮箱'>
            <Input
              type='email'
              value={form.reply_to}
              onChange={(e) => set('reply_to', e.target.value)}
            />
          </Field>
        </div>
      </section>

      <section className='border-b p-4 sm:p-5'>
        <div className='mb-5 flex items-start gap-3'>
          <div className='bg-warning/10 text-warning flex size-9 shrink-0 items-center justify-center rounded-md'>
            <ShieldAlert className='size-4' />
          </div>
          <div>
            <h3 className='text-sm font-semibold'>实例巡检通知</h3>
            <p className='text-muted-foreground mt-1 text-xs'>
              普通连接错误达到阈值后通知；认证、凭据和权限错误首次失败立即通知。
            </p>
          </div>
        </div>
        <div className='grid gap-4 sm:grid-cols-2'>
          <Field label='通知收件人'>
            <Input
              value={form.alert_recipients}
              onChange={(e) => set('alert_recipients', e.target.value)}
              placeholder='ops@example.com, admin@example.com'
            />
            <p className='text-muted-foreground mt-1.5 text-xs'>
              多个邮箱使用英文逗号分隔。
            </p>
          </Field>
          <Field label='连续失败通知阈值'>
            <Input
              type='number'
              min={1}
              max={100}
              value={form.instance_alert_failure_threshold}
              onChange={(e) =>
                set('instance_alert_failure_threshold', Number(e.target.value))
              }
            />
            <p className='text-muted-foreground mt-1.5 text-xs'>
              单个实例还可以在实例设置中覆盖此全局阈值。
            </p>
          </Field>
          <label className='bg-muted/30 flex min-h-11 items-center justify-between gap-4 rounded-md border px-3 py-2 sm:col-span-2'>
            <span>
              <span className='block text-sm font-medium'>启用邮件发送</span>
              <span className='text-muted-foreground block text-xs'>
                关闭后巡检仍会运行，但不会发送故障或恢复邮件。
              </span>
            </span>
            <Switch
              checked={form.enabled}
              onCheckedChange={(value) => set('enabled', value)}
            />
          </label>
        </div>
      </section>

      <section className='flex flex-col gap-4 p-4 sm:p-5 lg:flex-row lg:items-end lg:justify-between'>
        <div className='min-w-0 flex-1'>
          <div className='mb-2 flex items-center gap-2 text-sm font-semibold'>
            <Send className='size-4' />
            发送测试邮件
          </div>
          <div className='flex max-w-xl flex-col gap-2 sm:flex-row'>
            <Input
              type='email'
              value={testRecipient}
              onChange={(e) => setTestRecipient(e.target.value)}
              placeholder='recipient@example.com'
            />
            <Button
              variant='outline'
              disabled={!testRecipient || testing}
              onClick={() => void test()}
            >
              <Send />
              {testing ? '发送中' : '发送测试'}
            </Button>
          </div>
          <p className='text-muted-foreground mt-1.5 text-xs'>
            请先保存设置，再验证连接、认证与投递。
          </p>
        </div>
        <Button
          disabled={saving || query.isLoading}
          onClick={() => void save()}
        >
          <Save />
          {saving ? '保存中' : '保存设置'}
        </Button>
      </section>
    </div>
  )
}
