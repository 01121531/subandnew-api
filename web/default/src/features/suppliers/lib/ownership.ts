import type { i18n } from 'i18next'

import { ROLE } from '@/lib/roles'
import type { AuthUser } from '@/stores/auth-store'

export function canTakeSupplierOwnership(
  user: Pick<AuthUser, 'id' | 'role' | 'status'> | null | undefined,
  ownerId: number | undefined
) {
  return (
    !!user &&
    user.role === ROLE.SUPER_ADMIN &&
    (user.status === undefined || user.status === 1) &&
    user.id !== ownerId
  )
}

const en = {
  action: 'Take ownership',
  title: 'Take ownership of {{name}}?',
  confirm:
    'You will become the responsible administrator. All existing supplier sessions and pending OAuth flows will be revoked, and this action will be audited. Bindings and configuration stay unchanged; unsaved edits will be discarded. The supplier must sign in again.',
  success: 'Ownership transferred. The supplier must sign in again.',
  failed:
    'Ownership could not be transferred. Refresh the supplier and try again.',
  refreshFailed:
    'Ownership transferred, but the details could not be refreshed. Reopen the supplier.',
  owner: 'Responsible administrator: #{{id}}',
  legacy: 'Responsible administrator: legacy root fallback',
}
const zh = {
  action: '接管',
  title: '接管“{{name}}”？',
  confirm:
    '你将成为负责管理员，全部现有供应商会话和待完成的 OAuth 流程将被撤销，操作会写入审计。绑定和配置保持不变，未保存的编辑将被丢弃。供应商需要重新登录。',
  success: '已接管，供应商需要重新登录。',
  failed: '接管失败，请刷新供应商后重试。',
  refreshFailed: '已接管，但详情刷新失败，请重新打开供应商。',
  owner: '负责管理员：#{{id}}',
  legacy: '负责管理员：历史 root 回退',
}

export function supplierOwnershipText(i18n: i18n) {
  const namespace = 'supplierOwnership'
  for (const language of ['en', 'zhCN', 'zhTW', 'fr', 'ru', 'ja', 'vi']) {
    if (!i18n.hasResourceBundle(language, namespace)) {
      i18n.addResourceBundle(
        language,
        namespace,
        language.startsWith('zh') ? zh : en
      )
    }
  }
  return i18n.getFixedT(null, namespace)
}
