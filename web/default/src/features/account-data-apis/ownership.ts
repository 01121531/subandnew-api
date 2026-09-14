import type { i18n } from 'i18next'

import { ROLE } from '@/lib/roles'
import type { AuthUser } from '@/stores/auth-store'

export function canTakeOwnership(
  user: Pick<AuthUser, 'id' | 'role' | 'status'> | null | undefined,
  ownerId: number
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
    'You will become the responsible administrator. All existing API keys and portal sessions will be revoked, and this action will be audited. Configuration stays unchanged; unsaved edits will be discarded. Create a new key before restoring API access.',
  success:
    'Ownership transferred. Existing keys and portal sessions were revoked.',
  failed:
    'Ownership could not be transferred. Refresh the authorization and try again.',
  owner: 'Responsible administrator: #{{id}}',
  legacy: 'Responsible administrator: legacy root fallback',
}
const zh = {
  action: '接管',
  title: '接管“{{name}}”？',
  confirm:
    '你将成为负责管理员，全部现有 API 密钥和门户会话将被撤销，操作会写入审计。配置保持不变，未保存的编辑将被丢弃。恢复 API 访问前需要创建新密钥。',
  success: '已接管，旧密钥和门户会话已撤销。',
  failed: '接管失败，请刷新授权后重试。',
  owner: '负责管理员：#{{id}}',
  legacy: '负责管理员：历史 root 回退',
}

export function ownershipText(i18n: i18n) {
  const namespace = 'accountDataOwnership'
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
