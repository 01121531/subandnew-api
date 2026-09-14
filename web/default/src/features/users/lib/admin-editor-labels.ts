import { useTranslation } from 'react-i18next'

// Feature-local fallbacks keep the bounded editor bilingual without changing shared locales.
const labels = {
  create: ['Create administrator', '创建管理员'],
  update: ['Update administrator', '编辑管理员'],
  basic: ['Basic information', '基本信息'],
  functions: ['Function permissions', '功能权限'],
  instances: ['Instance access', '实例权限'],
  fields: ['Data visibility', '数据可见性'],
  all: ['All instances', '全部实例'],
  selected: ['Selected instances', '指定实例'],
  search: ['Search instances', '搜索实例'],
  noInstances: ['No matching instances', '没有匹配的实例'],
  loadError: [
    'Could not load permissions or user details.',
    '无法加载权限或用户详情。',
  ],
  instanceError: ['Could not load instances.', '无法加载实例。'],
  retry: ['Retry', '重试'],
  missing: ['Policy not provided by server', '服务器未返回数据策略'],
  configure: ['Configure data policy', '配置数据策略'],
  conflict: [
    'Version conflict. Your draft has been preserved.',
    '版本冲突，您的编辑草稿已保留。',
  ],
  latest: ['Review latest version', '查看最新版本'],
  useRevision: ['Apply draft to this version', '将草稿应用于此版本'],
  revision: ['Version', '版本'],
  amount: ['Amount', '金额'],
  requests: ['Requests', '请求数'],
  tokens: ['Tokens', '令牌数'],
  rpm: ['RPM', '每分钟请求数'],
  concurrency: ['Concurrency', '并发数'],
  accounts: ['Accounts', '账号'],
  rates: ['Rates and utilization', '成功率与利用率'],
  email: ['Email', '邮箱'],
  vendor: ['Vendor', '供应商'],
  group: ['Group', '分组'],
  status: ['Status', '状态'],
  time: ['Time', '时间'],
} as const

export function useAdminEditorLabels() {
  const { t, i18n } = useTranslation()
  return (key: keyof typeof labels): string =>
    t(`users.adminEditor.${key}`, {
      defaultValue:
        labels[key][i18n.resolvedLanguage?.startsWith('zh') ? 1 : 0],
    })
}
