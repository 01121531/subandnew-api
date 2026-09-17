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
import { useTranslation } from 'react-i18next'

type Text = (zh: string, en: string) => string
export function useScheduleText(): Text {
  const { i18n } = useTranslation()
  return (zh, en) => (i18n.language.toLowerCase().startsWith('zh') ? zh : en)
}
export function scheduleMessage(error: unknown, text: Text) {
  const code =
    (error as { response?: { data?: { message?: string } } })?.response?.data
      ?.message ?? ''
  return (
    statusText(code, text) ||
    text('操作失败，请刷新后重试。', 'Operation failed. Refresh and retry.')
  )
}
export function statusText(code: string, text: Text): string {
  const labels: Record<string, [string, string]> = {
    pending: ['待发送', 'Pending'],
    retry: ['等待重试', 'Retry scheduled'],
    sending: ['发送中', 'Sending'],
    sent: ['已发送', 'Sent'],
    uncertain: ['待核对（可能已发送）', 'Verify delivery (may have been sent)'],
    failed: ['失败', 'Failed'],
    cancelled: ['已取消', 'Cancelled'],
    succeeded: ['已完成', 'Completed'],
    exporting: ['导出中', 'Exporting'],
    delivering: ['邮件投递中', 'Delivering'],
    skipped: ['本轮跳过', 'Skipped'],
    no_data: ['无匹配数据，不发送邮件', 'No matching data; no email sent'],
    attachment_too_large: [
      '附件超过 15MB，仅供下载',
      'Attachment exceeds 15MB; download only',
    ],
    delivery_failed: ['邮件需要处理', 'Delivery needs attention'],
    running: ['生成中', 'Generating'],
    expired: ['文件已过期', 'Expired'],
    permission_revoked: [
      '负责人权限已变更，计划暂停',
      'Owner permissions changed; paused',
    ],
    smtp_not_configured: [
      'SMTP 未配置或未启用',
      'SMTP not configured or enabled',
    ],
    version_conflict: [
      '配置已变化，请刷新后重试',
      'Configuration changed. Refresh and retry.',
    ],
    invalid_schedule: [
      '请检查名称、范围、日期和收件邮箱',
      'Check name, scope, dates and recipients',
    ],
    snapshot_unavailable: [
      '本地账号快照不可用',
      'Local account snapshot unavailable',
    ],
    snapshot_changed: [
      '账号快照变化，请重新执行',
      'Snapshot changed; run again',
    ],
    account_limit: ['账号超过 10,000 条', 'Account limit exceeded (10,000)'],
    overlapping_run: ['上一轮未结束，本轮跳过', 'Previous run still active'],
    file_expired: ['文件已过期', 'File expired'],
    file_unavailable: ['文件不可用', 'File unavailable'],
    export_failed: ['导出失败', 'Export failed'],
    smtp_uncertain: [
      '投递结果不确定，请先核对',
      'Uncertain delivery; verify first',
    ],
    smtp_temporary: ['临时拒收', 'Temporary rejection'],
    smtp_permanent: ['永久拒收', 'Permanent rejection'],
    delivery_interrupted: [
      '发送中断，请先核对',
      'Sending interrupted; verify first',
    ],
  }
  return labels[code] ? text(...labels[code]) : code
}
