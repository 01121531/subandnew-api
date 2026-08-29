/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  Braces,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Eye,
  ExternalLink,
  KeyRound,
  LockKeyhole,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { SectionPageLayout } from '@/components/layout'
import { MultiSelect } from '@/components/multi-select'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { AccountFilterPanel } from '@/features/managed-accounts/account-filter-panel'
import {
  isAccountFilterRuleComplete,
  type AccountAdvancedFilter,
} from '@/features/managed-accounts/account-filtering'
import { accountAmountSummaries } from '@/lib/account-amounts'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  createAccountDataAPI,
  createAccountDataAPIKey,
  deleteAccountDataAPI,
  listAccountDataAPIAccessLogs,
  listAccountDataAPIInstances,
  listAccountDataAPIs,
  previewAccountDataAPI,
  revokeAccountDataAPIKey,
  updateAccountDataAPI,
} from './api'
import type {
  AccountDataAPI,
  AccountDataAPIAccessLog,
  AccountDataAPIInput,
  AccountDataAPIPreview,
} from './types'

const QUERY_KEY = ['account-data-apis'] as const
const HANDOFF_KEY = 'managed-account-api-draft'
const DEFAULT_FIELDS = [
  'instance_name',
  'platform',
  'name',
  'type',
  'status',
  'available',
]

const FIELD_OPTIONS = [
  ['instance_name', '实例'],
  ['platform', '平台'],
  ['name', '账号名称'],
  ['type', '账号类型'],
  ['status', '状态'],
  ['available', '可用性'],
  ['email', '邮箱'],
  ['note', '备注'],
  ['ownership', '账号归属'],
  ['group', '分组'],
  ['source_name', '工作节点'],
  ['created_at', '录入时间'],
  ['last_activity_at', '最后活动'],
  ['requests', '请求数'],
  ['tokens', '总 Token'],
  ['amount', '消费金额'],
  ['rpm', 'RPM'],
  ['active_sessions', '活跃会话'],
  ['utilization_5h', '5 小时利用率'],
  ['utilization_7d', '7 天利用率'],
] as const

const SORT_OPTIONS = [
  ['name', '账号名称'],
  ['created_at', '录入时间'],
  ['last_activity_at', '最后活动'],
  ['status', '状态'],
  ['requests', '请求数'],
  ['tokens', '总 Token'],
  ['amount', '消费金额'],
] as const

type DraftHandoff = Partial<AccountDataAPIInput>

function defaultInput(): AccountDataAPIInput {
  return {
    name: '',
    description: '',
    status: 'enabled',
    dataset: 'inventory',
    preset_days: 7,
    instance_ids: [],
    include_terms: [],
    exclude_terms: [],
    match_mode: 'all',
    rules: [],
    fields: [...DEFAULT_FIELDS],
    sort_by: 'created_at',
    sort_order: 'desc',
    page_size: 50,
    rate_limit_per_minute: 60,
    allowed_cidrs: [],
    portal_enabled: false,
    portal_password: '',
    reset_portal_slug: false,
  }
}

function normalizeInputCollections(
  input: AccountDataAPIInput
): AccountDataAPIInput {
  return {
    ...input,
    instance_ids: input.instance_ids ?? [],
    include_terms: input.include_terms ?? [],
    exclude_terms: input.exclude_terms ?? [],
    rules: (input.rules ?? []).map((rule) => ({
      ...rule,
      values: rule.values ?? [],
    })),
    fields: input.fields ?? [],
    allowed_cidrs: input.allowed_cidrs ?? [],
  }
}

function toInput(item: AccountDataAPI): AccountDataAPIInput {
  return normalizeInputCollections({
    name: item.name,
    description: item.description,
    status: item.status,
    dataset: item.dataset,
    preset_days: item.preset_days,
    instance_ids: item.instance_ids,
    include_terms: item.include_terms,
    exclude_terms: item.exclude_terms,
    match_mode: item.match_mode,
    rules: item.rules,
    fields: item.fields,
    sort_by: item.sort_by,
    sort_order: item.sort_order,
    page_size: item.page_size,
    rate_limit_per_minute: item.rate_limit_per_minute,
    allowed_cidrs: item.allowed_cidrs,
    portal_enabled: item.portal_enabled,
    portal_password: '',
    reset_portal_slug: false,
  })
}

function previewInputSignature(input: AccountDataAPIInput) {
  const {
    portal_enabled: _portalEnabled,
    portal_password: _portalPassword,
    reset_portal_slug: _resetPortalSlug,
    ...dataInput
  } = input
  return JSON.stringify(dataInput)
}

function generatedPortalPassword() {
  const alphabet =
    'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%'
  const bytes = new Uint8Array(18)
  globalThis.crypto.getRandomValues(bytes)
  return Array.from(bytes, (value) =>
    alphabet.charAt(value % alphabet.length)
  ).join('')
}

function errorMessage(error: unknown) {
  return (
    (error as { response?: { data?: { message?: string } } }).response?.data
      ?.message ?? '请求失败，请检查配置后重试'
  )
}

function formatTime(timestamp: number) {
  if (!timestamp) return '--'
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(timestamp * 1000)
}

function absolutePortalURL(value: string) {
  if (!value || typeof window === 'undefined') return value
  return new URL(value, window.location.origin).toString()
}

function activeKeyLabel(item: AccountDataAPI) {
  const active = item.keys
    .filter((key) => !key.revoked_at && key.expires_at > Date.now() / 1000)
    .sort((left, right) => left.expires_at - right.expires_at)
  if (active.length === 0) return '无活动密钥'
  return `${active.length} 个 · 最近到期 ${formatTime(active[0].expires_at)}`
}

export function AccountDataAPIs() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const queryClient = useQueryClient()
  const canManage = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_ACCOUNT_API,
    ADMIN_PERMISSION_ACTIONS.MANAGE
  )
  const canAudit = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_ACCOUNT_API,
    ADMIN_PERMISSION_ACTIONS.AUDIT
  )
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<AccountDataAPI | null | undefined>()
  const [deleteTarget, setDeleteTarget] = useState<AccountDataAPI | null>(null)
  const [secret, setSecret] = useState<{
    name: string
    value: string
    portalPassword?: string
    portalURL?: string
  } | null>(null)
  const [logsTarget, setLogsTarget] = useState<AccountDataAPI | null>(null)
  const [prefill, setPrefill] = useState<DraftHandoff | null>(null)

  useEffect(() => {
    try {
      const raw = sessionStorage.getItem(HANDOFF_KEY)
      if (!raw) return
      sessionStorage.removeItem(HANDOFF_KEY)
      setPrefill(JSON.parse(raw) as DraftHandoff)
      setEditing(null)
    } catch {
      sessionStorage.removeItem(HANDOFF_KEY)
    }
  }, [])

  const query = useQuery({
    queryKey: [...QUERY_KEY, search, status, page],
    queryFn: () => listAccountDataAPIs({ search, status, page, pageSize: 20 }),
  })
  const removeMutation = useMutation({
    mutationFn: deleteAccountDataAPI,
    onSuccess: async () => {
      setDeleteTarget(null)
      await queryClient.invalidateQueries({ queryKey: QUERY_KEY })
      toast.success(t('授权已删除，所有密钥已撤销'))
    },
    onError: (error) => toast.error(errorMessage(error)),
  })
  const rotateMutation = useMutation({
    mutationFn: (item: AccountDataAPI) =>
      createAccountDataAPIKey(item.id, {
        name: `轮换 ${formatTime(Date.now() / 1000)}`,
      }),
    onSuccess: async (response, item) => {
      setSecret({ name: item.name, value: response.data.secret })
      await queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })
  const revokeMutation = useMutation({
    mutationFn: ({ apiId, keyId }: { apiId: number; keyId: number }) =>
      revokeAccountDataAPIKey(apiId, keyId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: QUERY_KEY })
      toast.success(t('密钥已撤销'))
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const items = query.data?.data.items ?? []
  const total = query.data?.data.total ?? 0
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('接口管理')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {canManage && (
          <Button onClick={() => setEditing(null)}>
            <Plus />
            {t('创建接口授权')}
          </Button>
        )}
        <Button
          variant='outline'
          size='icon-sm'
          aria-label={t('Refresh')}
          onClick={() => void query.refetch()}
        >
          <RefreshCw className={query.isFetching ? 'animate-spin' : ''} />
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <>
          <div className='grid gap-4 pb-6'>
            <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
              <div className='relative min-w-0 flex-1'>
                <Search className='text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2' />
                <Input
                  className='h-11 pl-9 sm:h-9'
                  value={search}
                  onChange={(event) => {
                    setSearch(event.target.value)
                    setPage(1)
                  }}
                  placeholder={t('搜索授权名称')}
                />
              </div>
              <NativeSelect
                className='w-full sm:w-44'
                value={status}
                onChange={(event) => {
                  setStatus(event.target.value)
                  setPage(1)
                }}
              >
                <NativeSelectOption value=''>
                  {t('全部状态')}
                </NativeSelectOption>
                <NativeSelectOption value='enabled'>
                  {t('已启用')}
                </NativeSelectOption>
                <NativeSelectOption value='disabled'>
                  {t('已停用')}
                </NativeSelectOption>
              </NativeSelect>
            </div>

            {query.isLoading && (
              <div className='text-muted-foreground py-16 text-center'>
                {t('Loading...')}
              </div>
            )}
            {query.isError && (
              <div className='border-destructive/30 bg-destructive/5 rounded-lg border p-6 text-center'>
                <p>{t('接口授权加载失败')}</p>
                <Button
                  className='mt-3'
                  variant='outline'
                  onClick={() => void query.refetch()}
                >
                  {t('Retry')}
                </Button>
              </div>
            )}
            {!query.isLoading && !query.isError && items.length === 0 && (
              <div className='border-border/70 rounded-lg border py-16 text-center'>
                <Braces className='text-muted-foreground mx-auto mb-3 size-8' />
                <p className='font-medium'>{t('暂无接口授权')}</p>
                <p className='text-muted-foreground mt-1 text-sm'>
                  {t('创建后，乙方可使用独立密钥读取授权账号快照。')}
                </p>
              </div>
            )}
            {!query.isLoading && !query.isError && items.length > 0 && (
              <>
                <div className='hidden overflow-hidden rounded-lg border md:block'>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('授权')}</TableHead>
                        <TableHead>{t('数据范围')}</TableHead>
                        <TableHead>{t('命中账号')}</TableHead>
                        <TableHead>{t('最近访问')}</TableHead>
                        <TableHead className='text-right'>
                          {t('操作')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {items.map((item) => (
                        <TableRow key={item.id}>
                          <TableCell>
                            <div className='flex items-center gap-2 font-medium'>
                              {item.name}
                              <StatusBadge item={item} />
                            </div>
                            <div className='text-muted-foreground mt-1 text-xs'>
                              {item.description || item.endpoint}
                            </div>
                          </TableCell>
                          <TableCell>
                            {item.dataset === 'inventory'
                              ? t('账号明细')
                              : t('{{days}} 天账号产出', {
                                  days: item.preset_days,
                                })}
                            <div className='text-muted-foreground text-xs'>
                              {t('{{count}} 个实例 · {{fields}} 个字段', {
                                count: item.instance_ids.length,
                                fields: item.fields.length + 2,
                              })}
                            </div>
                            <div className='text-muted-foreground text-xs'>
                              {activeKeyLabel(item)}
                            </div>
                            {item.portal_enabled && (
                              <div className='text-success mt-1 flex items-center gap-1 text-xs'>
                                <LockKeyhole className='size-3' />
                                {t('可视化门户已启用')}
                              </div>
                            )}
                          </TableCell>
                          <TableCell className='tabular-nums'>
                            {item.matched_count}
                          </TableCell>
                          <TableCell>
                            {formatTime(item.last_accessed_at)}
                            <div className='text-muted-foreground text-xs'>
                              {t('{{count}} 次请求', {
                                count: item.request_count,
                              })}
                            </div>
                          </TableCell>
                          <TableCell>
                            <RowActions
                              item={item}
                              canManage={canManage}
                              canAudit={canAudit}
                              onEdit={() => setEditing(item)}
                              onLogs={() => setLogsTarget(item)}
                              onRotate={() => rotateMutation.mutate(item)}
                              onRevoke={(keyId) =>
                                revokeMutation.mutate({ apiId: item.id, keyId })
                              }
                              onDelete={() => setDeleteTarget(item)}
                            />
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>

                <Accordion className='rounded-lg border px-3 md:hidden'>
                  {items.map((item) => (
                    <AccordionItem key={item.id} value={String(item.id)}>
                      <AccordionTrigger className='min-h-14'>
                        <span className='min-w-0'>
                          <span className='flex items-center gap-2'>
                            <span className='truncate font-medium'>
                              {item.name}
                            </span>
                            <StatusBadge item={item} />
                          </span>
                          <span className='text-muted-foreground mt-1 block text-xs'>
                            {item.matched_count} {t('个账号')} ·{' '}
                            {item.instance_ids.length} {t('个实例')}
                          </span>
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        <div className='grid grid-cols-2 gap-3 border-t pt-3 text-sm'>
                          <Detail
                            label={t('数据集')}
                            value={
                              item.dataset === 'inventory'
                                ? t('账号明细')
                                : t('新增账号产出')
                            }
                          />
                          <Detail
                            label={t('最近访问')}
                            value={formatTime(item.last_accessed_at)}
                          />
                          <Detail
                            label={t('采集时间')}
                            value={formatTime(item.last_observed_at)}
                          />
                          <Detail
                            label={t('限流')}
                            value={`${item.rate_limit_per_minute}/min`}
                          />
                          <Detail
                            label={t('活动密钥')}
                            value={activeKeyLabel(item)}
                          />
                          <Detail
                            label={t('可视化门户')}
                            value={
                              item.portal_enabled ? t('已启用') : t('未启用')
                            }
                          />
                        </div>
                        <RowActions
                          item={item}
                          canManage={canManage}
                          canAudit={canAudit}
                          mobile
                          onEdit={() => setEditing(item)}
                          onLogs={() => setLogsTarget(item)}
                          onRotate={() => rotateMutation.mutate(item)}
                          onRevoke={(keyId) =>
                            revokeMutation.mutate({ apiId: item.id, keyId })
                          }
                          onDelete={() => setDeleteTarget(item)}
                        />
                      </AccordionContent>
                    </AccordionItem>
                  ))}
                </Accordion>
              </>
            )}

            {total > 20 && (
              <div className='flex items-center justify-end gap-2'>
                <Button
                  variant='outline'
                  size='icon-sm'
                  disabled={page <= 1}
                  onClick={() => setPage((value) => value - 1)}
                >
                  <ChevronLeft />
                </Button>
                <span className='text-muted-foreground text-sm tabular-nums'>
                  {page} / {Math.ceil(total / 20)}
                </span>
                <Button
                  variant='outline'
                  size='icon-sm'
                  disabled={page * 20 >= total}
                  onClick={() => setPage((value) => value + 1)}
                >
                  <ChevronRight />
                </Button>
              </div>
            )}
          </div>
          <AuthorizationEditor
            open={editing !== undefined}
            item={editing ?? null}
            prefill={prefill}
            onOpenChange={(open) => {
              if (!open) {
                setEditing(undefined)
                setPrefill(null)
              }
            }}
            onCreated={(name, value, portalPassword, portalURL) =>
              setSecret({ name, value, portalPassword, portalURL })
            }
          />
          <SecretDialog
            secret={secret}
            onOpenChange={(open) => !open && setSecret(null)}
          />
          <AccessLogDialog
            item={logsTarget}
            onOpenChange={(open) => !open && setLogsTarget(null)}
          />
          <ConfirmDialog
            open={deleteTarget != null}
            onOpenChange={(open) => !open && setDeleteTarget(null)}
            title={t('删除接口授权')}
            desc={t('删除后全部密钥会立即失效，访问日志仍会保留。')}
            destructive
            isLoading={removeMutation.isPending}
            handleConfirm={() =>
              deleteTarget && removeMutation.mutate(deleteTarget.id)
            }
          />
        </>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function StatusBadge({ item }: { item: AccountDataAPI }) {
  if (item.status === 'disabled') {
    return <Badge variant='secondary'>已停用</Badge>
  }
  if (item.stale) {
    return (
      <Badge variant='outline' className='text-warning'>
        旧数据
      </Badge>
    )
  }
  return (
    <Badge variant='outline' className='text-success'>
      已启用
    </Badge>
  )
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{label}</div>
      <div className='mt-1 break-words'>{value}</div>
    </div>
  )
}

function RowActions(props: {
  item: AccountDataAPI
  canManage: boolean
  canAudit: boolean
  mobile?: boolean
  onEdit: () => void
  onLogs: () => void
  onRotate: () => void
  onRevoke: (keyId: number) => void
  onDelete: () => void
}) {
  const activeKeys = props.item.keys.filter(
    (key) => !key.revoked_at && key.expires_at > Date.now() / 1000
  )
  return (
    <div
      className={cn(
        'flex flex-wrap justify-end gap-2',
        props.mobile && 'mt-4 justify-start'
      )}
    >
      {props.canAudit && (
        <Button variant='outline' size='sm' onClick={props.onLogs}>
          <Activity />
          访问日志
        </Button>
      )}
      {props.item.portal_enabled && props.item.portal_url && (
        <Button
          variant='outline'
          size='sm'
          onClick={() =>
            window.open(
              absolutePortalURL(props.item.portal_url),
              '_blank',
              'noopener'
            )
          }
        >
          <ExternalLink />
          门户
        </Button>
      )}
      {props.canManage && (
        <Button variant='outline' size='sm' onClick={props.onEdit}>
          <Pencil />
          编辑
        </Button>
      )}
      {props.canManage && activeKeys.length < 2 && (
        <Button variant='outline' size='sm' onClick={props.onRotate}>
          <KeyRound />
          新密钥
        </Button>
      )}
      {props.canManage && activeKeys.length > 1 && (
        <Button
          variant='outline'
          size='sm'
          onClick={() => props.onRevoke(activeKeys.at(-1)?.id ?? 0)}
        >
          <ShieldCheck />
          撤销旧密钥
        </Button>
      )}
      {props.canManage && (
        <Button
          variant='ghost'
          size='icon-sm'
          className='text-destructive'
          aria-label='删除授权'
          onClick={props.onDelete}
        >
          <Trash2 />
        </Button>
      )}
    </div>
  )
}

function AuthorizationEditor(props: {
  open: boolean
  item: AccountDataAPI | null
  prefill: DraftHandoff | null
  onOpenChange: (open: boolean) => void
  onCreated: (
    name: string,
    secret: string,
    portalPassword?: string,
    portalURL?: string
  ) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [step, setStep] = useState(0)
  const [input, setInput] = useState<AccountDataAPIInput>(defaultInput)
  const [preview, setPreview] = useState<AccountDataAPIPreview | null>(null)
  const [previewSignature, setPreviewSignature] = useState('')
  const instancesQuery = useQuery({
    queryKey: ['account-data-api-instances'],
    queryFn: listAccountDataAPIInstances,
    enabled: props.open,
  })
  const instances = useMemo(
    () => instancesQuery.data?.data ?? [],
    [instancesQuery.data?.data]
  )

  useEffect(() => {
    if (!props.open) return
    const base = props.item ? toInput(props.item) : defaultInput()
    setInput(
      normalizeInputCollections({
        ...base,
        ...props.prefill,
        name: props.prefill?.name ?? base.name,
      })
    )
    setStep(0)
    setPreview(null)
    setPreviewSignature('')
  }, [props.item, props.open, props.prefill])

  const filter: AccountAdvancedFilter = useMemo(
    () => ({
      match_mode: input.match_mode,
      rules: input.rules.map((rule, index) => ({
        ...rule,
        id: `account-data-api-rule-${index}`,
      })),
    }),
    [input.match_mode, input.rules]
  )
  const selectedInstances = input.instance_ids.map(String)
  const instanceOptions = useMemo(
    () =>
      instances.map((instance) => ({
        value: String(instance.id),
        label: `${instance.name} · ${instance.kind}`,
      })),
    [instances]
  )
  const filterOptions = useMemo(
    () => ({
      instance: instanceOptions,
      platform: [...new Set(instances.map((item) => item.kind))].map(
        (value) => ({ value, label: value })
      ),
    }),
    [instanceOptions, instances]
  )
  const previewMutation = useMutation({
    mutationFn: previewAccountDataAPI,
    onSuccess: (response) => {
      setPreview(response.data)
      setPreviewSignature(previewInputSignature(input))
    },
    onError: (error) => toast.error(errorMessage(error)),
  })
  const saveMutation = useMutation({
    mutationFn: async () => {
      if (props.item) {
        const response = await updateAccountDataAPI(props.item.id, input)
        return { api: response.data, secret: '' }
      }
      const response = await createAccountDataAPI(input)
      return { api: response.data.api, secret: response.data.secret }
    },
    onSuccess: async (response) => {
      await queryClient.invalidateQueries({ queryKey: QUERY_KEY })
      props.onOpenChange(false)
      if (response.secret || input.portal_password) {
        props.onCreated(
          input.name,
          response.secret,
          input.portal_password || undefined,
          response.api.portal_url || undefined
        )
      }
      toast.success(props.item ? t('接口授权已更新') : t('接口授权已创建'))
    },
    onError: (error) => toast.error(errorMessage(error)),
  })
  const validationIssues: Array<{ step: number; label: string }> = []
  if (!input.name.trim()) {
    validationIssues.push({ step: 0, label: t('授权名称') })
  }
  if (input.instance_ids.length === 0) {
    validationIssues.push({ step: 1, label: t('至少选择一个实例') })
  }
  if (!filter.rules.every(isAccountFilterRuleComplete)) {
    validationIssues.push({ step: 1, label: t('完成高级筛选条件') })
  }
  if (input.fields.length === 0) {
    validationIssues.push({ step: 2, label: t('至少开放一个字段') })
  }
  if (input.page_size < 1 || input.page_size > 100) {
    validationIssues.push({ step: 3, label: t('单页上限应为 1 至 100') })
  }
  if (input.rate_limit_per_minute < 1 || input.rate_limit_per_minute > 6000) {
    validationIssues.push({
      step: 3,
      label: t('每分钟请求上限应为 1 至 6000'),
    })
  }
  if (
    input.portal_enabled &&
    input.portal_password.length < 8 &&
    !props.item?.portal_configured
  ) {
    validationIssues.push({ step: 3, label: t('至少 8 位的门户密码') })
  }
  const valid = validationIssues.length === 0
  const runPreview = () => {
    const issue = validationIssues[0]
    if (issue) {
      toast.error(t('请先完成：{{item}}', { item: issue.label }))
      setStep(issue.step)
      return
    }
    previewMutation.mutate(input)
  }
  const endpoint =
    typeof window === 'undefined'
      ? '/open-api/v1/accounts'
      : `${window.location.origin}/open-api/v1/accounts`
  let previewStatus = t('完整')
  if (preview?.partial) previewStatus = t('部分可用')
  else if (preview?.stale) previewStatus = t('旧数据')
  const previewAmounts = preview
    ? accountAmountSummaries(preview.summary.amounts)
    : []

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex max-h-[calc(100dvh-1rem)] w-[min(960px,calc(100%-1rem))] max-w-none min-w-0 flex-col overflow-hidden p-0'>
        <DialogHeader className='border-b px-4 pt-4 pb-3 sm:px-6'>
          <DialogTitle>
            {props.item ? t('编辑接口授权') : t('创建接口授权')}
          </DialogTitle>
          <DialogDescription>
            {t('授权固定读取后台账号快照，不会直接请求目标平台。')}
          </DialogDescription>
          <div
            className='grid grid-cols-2 gap-1 pt-2 sm:grid-cols-4'
            aria-label={t('创建步骤')}
          >
            {['基本信息', '数据范围', '开放字段', '安全与预览'].map(
              (label, index) => (
                <button
                  type='button'
                  key={label}
                  className={cn(
                    'min-h-9 rounded-md px-2 text-xs',
                    index === step
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-muted text-muted-foreground'
                  )}
                  onClick={() => setStep(index)}
                >
                  {index + 1}. {t(label)}
                </button>
              )
            )}
          </div>
        </DialogHeader>
        <div className='min-h-0 min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-4 py-4 sm:px-6'>
          {step === 0 && (
            <div className='grid gap-4'>
              <Field label={t('授权名称')}>
                <Input
                  value={input.name}
                  maxLength={96}
                  onChange={(event) =>
                    setInput({ ...input, name: event.target.value })
                  }
                />
              </Field>
              <Field label={t('描述')}>
                <Textarea
                  value={input.description}
                  maxLength={500}
                  onChange={(event) =>
                    setInput({ ...input, description: event.target.value })
                  }
                />
              </Field>
              <div className='grid gap-4 sm:grid-cols-2'>
                <Field label={t('数据集')}>
                  <NativeSelect
                    className='w-full'
                    value={input.dataset}
                    onChange={(event) =>
                      setInput({
                        ...input,
                        dataset: event.target
                          .value as AccountDataAPIInput['dataset'],
                      })
                    }
                  >
                    <NativeSelectOption value='inventory'>
                      {t('账号明细')}
                    </NativeSelectOption>
                    <NativeSelectOption value='account_output'>
                      {t('新增账号产出')}
                    </NativeSelectOption>
                  </NativeSelect>
                </Field>
                <Field label={t('状态')}>
                  <NativeSelect
                    className='w-full'
                    value={input.status}
                    onChange={(event) =>
                      setInput({
                        ...input,
                        status: event.target
                          .value as AccountDataAPIInput['status'],
                      })
                    }
                  >
                    <NativeSelectOption value='enabled'>
                      {t('已启用')}
                    </NativeSelectOption>
                    <NativeSelectOption value='disabled'>
                      {t('已停用')}
                    </NativeSelectOption>
                  </NativeSelect>
                </Field>
              </div>
              {input.dataset === 'account_output' && (
                <Field label={t('产出时间范围')}>
                  <NativeSelect
                    className='w-full'
                    value={String(input.preset_days)}
                    onChange={(event) =>
                      setInput({
                        ...input,
                        preset_days: Number(event.target.value),
                      })
                    }
                  >
                    {[1, 7, 14, 30].map((days) => (
                      <NativeSelectOption key={days} value={String(days)}>
                        {days} {t('天')}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </Field>
              )}
            </div>
          )}
          {step === 1 && (
            <div className='grid gap-4'>
              <Field label={t('授权实例（最多 100 个）')}>
                <MultiSelect
                  options={instanceOptions}
                  selected={selectedInstances}
                  onChange={(values) =>
                    setInput({
                      ...input,
                      instance_ids: values.map(Number),
                    })
                  }
                  maxValues={100}
                  onLimitExceeded={() =>
                    toast.error(t('每个授权最多选择 100 个实例'))
                  }
                  placeholder={t('选择实例')}
                  className='min-h-11'
                />
              </Field>
              <Field label={t('包含任一值')}>
                <MultiSelect
                  options={[]}
                  selected={input.include_terms}
                  onChange={(values) =>
                    setInput({ ...input, include_terms: values })
                  }
                  allowCreate
                  maxValues={50}
                  onLimitExceeded={() => toast.error(t('最多输入 50 个筛选值'))}
                  placeholder={t('输入后按回车，可添加多个值')}
                  className='min-h-11'
                />
              </Field>
              <Field label={t('排除任一值')}>
                <MultiSelect
                  options={[]}
                  selected={input.exclude_terms}
                  onChange={(values) =>
                    setInput({ ...input, exclude_terms: values })
                  }
                  allowCreate
                  maxValues={50}
                  onLimitExceeded={() => toast.error(t('最多输入 50 个筛选值'))}
                  placeholder={t('命中任一值即排除')}
                  className='min-h-11'
                />
              </Field>
              <AccountFilterPanel
                value={filter}
                onChange={(value) => {
                  setInput({
                    ...input,
                    match_mode: value.match_mode,
                    rules: value.rules.map(({ id: _id, ...rule }) => ({
                      ...rule,
                      values: [...rule.values],
                    })),
                  })
                }}
                options={filterOptions}
                templatesEnabled={false}
              />
            </div>
          )}
          {step === 2 && (
            <div>
              <p className='text-muted-foreground mb-3 text-sm'>
                {t(
                  '实例 ID 和字符串账号 ID 始终返回。邮箱、备注、金额及用量需要主动授权。'
                )}
              </p>
              <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-3'>
                {FIELD_OPTIONS.map(([value, label]) => (
                  <label
                    key={value}
                    className='hover:bg-muted/40 flex min-h-11 cursor-pointer items-center gap-3 rounded-md border px-3 py-2'
                  >
                    <Checkbox
                      checked={input.fields.includes(value)}
                      onCheckedChange={(checked) =>
                        setInput({
                          ...input,
                          fields: checked
                            ? [...input.fields, value]
                            : input.fields.filter((field) => field !== value),
                        })
                      }
                    />
                    <span>{t(label)}</span>
                  </label>
                ))}
              </div>
            </div>
          )}
          {step === 3 && (
            <div className='grid gap-4'>
              <div className='grid gap-4 sm:grid-cols-2'>
                <Field label={t('默认排序')}>
                  <NativeSelect
                    className='w-full'
                    value={input.sort_by}
                    onChange={(event) =>
                      setInput({ ...input, sort_by: event.target.value })
                    }
                  >
                    {SORT_OPTIONS.map(([value, label]) => (
                      <NativeSelectOption key={value} value={value}>
                        {t(label)}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </Field>
                <Field label={t('排序方向')}>
                  <NativeSelect
                    className='w-full'
                    value={input.sort_order}
                    onChange={(event) =>
                      setInput({
                        ...input,
                        sort_order: event.target.value as 'asc' | 'desc',
                      })
                    }
                  >
                    <NativeSelectOption value='desc'>
                      {t('降序')}
                    </NativeSelectOption>
                    <NativeSelectOption value='asc'>
                      {t('升序')}
                    </NativeSelectOption>
                  </NativeSelect>
                </Field>
                <Field label={t('单页上限')}>
                  <Input
                    type='number'
                    min={1}
                    max={100}
                    value={input.page_size}
                    onChange={(event) =>
                      setInput({
                        ...input,
                        page_size: Number(event.target.value),
                      })
                    }
                  />
                </Field>
                <Field label={t('每密钥每分钟请求上限')}>
                  <Input
                    type='number'
                    min={1}
                    max={6000}
                    value={input.rate_limit_per_minute}
                    onChange={(event) =>
                      setInput({
                        ...input,
                        rate_limit_per_minute: Number(event.target.value),
                      })
                    }
                  />
                </Field>
              </div>
              <Field label={t('IP/CIDR 白名单（留空允许全部）')}>
                <Textarea
                  value={input.allowed_cidrs.join('\n')}
                  onChange={(event) =>
                    setInput({
                      ...input,
                      allowed_cidrs: event.target.value
                        .split(/[\n,，]+/)
                        .map((value) => value.trim())
                        .filter(Boolean),
                    })
                  }
                  placeholder='203.0.113.10&#10;203.0.113.0/24'
                />
              </Field>
              <div className='border-border/70 rounded-lg border p-3'>
                <label className='flex min-h-11 cursor-pointer items-center justify-between gap-3'>
                  <span>
                    <span className='flex items-center gap-2 font-medium'>
                      <LockKeyhole className='size-4' />
                      {t('启用可视化门户')}
                    </span>
                    <span className='text-muted-foreground mt-1 block text-xs'>
                      {t('乙方使用独立密码登录，只能查看此授权内的数据。')}
                    </span>
                  </span>
                  <Checkbox
                    checked={input.portal_enabled}
                    onCheckedChange={(checked) =>
                      setInput({ ...input, portal_enabled: checked === true })
                    }
                  />
                </label>
                {input.portal_enabled && (
                  <div className='mt-3 grid gap-3 border-t pt-3'>
                    <Field
                      label={
                        props.item?.portal_configured
                          ? t('重置门户密码（留空保持不变）')
                          : t('门户登录密码')
                      }
                    >
                      <div className='flex flex-col gap-2 sm:flex-row'>
                        <Input
                          type='text'
                          minLength={8}
                          maxLength={128}
                          autoComplete='new-password'
                          value={input.portal_password}
                          onChange={(event) =>
                            setInput({
                              ...input,
                              portal_password: event.target.value,
                            })
                          }
                          placeholder={t('至少 8 位')}
                        />
                        <Button
                          type='button'
                          variant='outline'
                          onClick={() =>
                            setInput({
                              ...input,
                              portal_password: generatedPortalPassword(),
                            })
                          }
                        >
                          <RefreshCw />
                          {t('生成强密码')}
                        </Button>
                        {input.portal_password && (
                          <CopyButton
                            value={input.portal_password}
                            tooltip={t('复制门户密码')}
                          />
                        )}
                      </div>
                    </Field>
                    {props.item?.portal_url && (
                      <div className='flex min-w-0 items-center gap-2 rounded-md border px-3 py-2'>
                        <code className='min-w-0 flex-1 truncate text-xs'>
                          {absolutePortalURL(props.item.portal_url)}
                        </code>
                        <CopyButton
                          value={absolutePortalURL(props.item.portal_url)}
                          tooltip={t('复制门户地址')}
                        />
                      </div>
                    )}
                    {props.item && (
                      <label className='flex min-h-11 cursor-pointer items-center gap-3 rounded-md border px-3 py-2'>
                        <Checkbox
                          checked={input.reset_portal_slug}
                          onCheckedChange={(checked) =>
                            setInput({
                              ...input,
                              reset_portal_slug: checked === true,
                            })
                          }
                        />
                        <span>
                          <span className='block text-sm font-medium'>
                            {t('重置随机访问地址')}
                          </span>
                          <span className='text-muted-foreground block text-xs'>
                            {t('旧地址和现有门户会话将立即失效。')}
                          </span>
                        </span>
                      </label>
                    )}
                  </div>
                )}
              </div>
              <div className='border-border/70 bg-muted/20 rounded-lg border p-3'>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <div>
                    <div className='font-medium'>{t('数据预览')}</div>
                    <div className='text-muted-foreground text-xs'>
                      {t('保存前必须成功预览一次。')}
                    </div>
                  </div>
                  <Button
                    type='button'
                    variant='outline'
                    disabled={previewMutation.isPending}
                    onClick={runPreview}
                  >
                    <Eye />
                    {previewMutation.isPending ? t('正在预览') : t('预览数据')}
                  </Button>
                </div>
                {!valid && (
                  <div className='text-destructive mt-2 text-xs leading-relaxed'>
                    {t('待完成：{{items}}', {
                      items: validationIssues
                        .map((issue) => issue.label)
                        .join('、'),
                    })}
                  </div>
                )}
                {preview && (
                  <div className='mt-3 border-t pt-3'>
                    <div className='grid grid-cols-1 gap-3 min-[420px]:grid-cols-2 sm:grid-cols-4'>
                      <Detail
                        label={t('命中账号')}
                        value={String(preview.total)}
                      />
                      <Detail
                        label={t('可用账号')}
                        value={String(preview.summary.available)}
                      />
                      <Detail
                        label={t('采集时间')}
                        value={formatTime(preview.observed_at)}
                      />
                      <Detail label={t('数据状态')} value={previewStatus} />
                      {previewAmounts.length > 0 ? (
                        previewAmounts.map((amount) => (
                          <Detail
                            key={amount.key}
                            label={t(amount.label)}
                            value={amount.value}
                          />
                        ))
                      ) : (
                        <Detail label={t('总金额')} value={t('未提供')} />
                      )}
                    </div>
                    {preview.sample.length > 0 && (
                      <div className='mt-3 grid gap-1.5'>
                        <div className='text-muted-foreground text-xs'>
                          {t('样例账号（最多 5 条）')}
                        </div>
                        {preview.sample.map((sample) => (
                          <div
                            key={`${String(sample.instance_id)}:${String(sample.account_id)}`}
                            className='grid gap-1 rounded-md border px-3 py-2 text-xs sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]'
                          >
                            <span className='truncate font-medium'>
                              {String(sample.name ?? sample.email ?? '--')}
                            </span>
                            <span className='text-muted-foreground truncate font-mono'>
                              {String(sample.account_id ?? '--')}
                            </span>
                            <span>{String(sample.status ?? '--')}</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
              <div className='flex min-w-0 items-center gap-2 rounded-md border px-3 py-2'>
                <code className='min-w-0 flex-1 truncate text-xs'>
                  {endpoint}
                </code>
                <CopyButton value={endpoint} tooltip={t('复制接口地址')} />
              </div>
            </div>
          )}
        </div>
        <DialogFooter className='flex-row flex-wrap justify-end border-t px-4 py-3 sm:px-6'>
          <Button
            variant='outline'
            onClick={() =>
              step === 0
                ? props.onOpenChange(false)
                : setStep((value) => value - 1)
            }
          >
            {step === 0 ? t('Cancel') : t('上一步')}
          </Button>
          {step < 3 ? (
            <Button
              disabled={
                step === 1 && !filter.rules.every(isAccountFilterRuleComplete)
              }
              onClick={() => setStep((value) => value + 1)}
            >
              {t('下一步')}
              <ChevronRight />
            </Button>
          ) : (
            <Button
              disabled={
                (input.status !== 'disabled' &&
                  (!preview ||
                    previewSignature !== previewInputSignature(input))) ||
                saveMutation.isPending
              }
              onClick={() => saveMutation.mutate()}
            >
              <CheckCircle2 />
              {saveMutation.isPending ? t('保存中') : t('保存授权')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Field(props: { label: string; children: ReactNode }) {
  return (
    <div className='grid gap-1.5'>
      <Label>{props.label}</Label>
      {props.children}
    </div>
  )
}

function SecretDialog(props: {
  secret: {
    name: string
    value: string
    portalPassword?: string
    portalURL?: string
  } | null
  onOpenChange: (open: boolean) => void
}) {
  const endpoint =
    typeof window === 'undefined'
      ? '/open-api/v1/accounts'
      : `${window.location.origin}/open-api/v1/accounts`
  const curl = props.secret
    ? `curl --url '${endpoint}?page=1&page_size=50' \\\n+  -H 'Authorization: Bearer ${props.secret.value}' \\\n+  -H 'Accept: application/json'`
    : ''
  return (
    <Dialog open={props.secret != null} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-w-xl'>
        <DialogHeader>
          <DialogTitle>保存 API Key</DialogTitle>
          <DialogDescription>
            密钥只显示这一次，关闭后无法再次查看。
          </DialogDescription>
        </DialogHeader>
        {props.secret && (
          <div className='grid gap-3'>
            {props.secret.value && (
              <>
                <div className='flex items-center gap-2 rounded-md border p-3'>
                  <code className='min-w-0 flex-1 text-xs break-all'>
                    {props.secret.value}
                  </code>
                  <CopyButton value={props.secret.value} tooltip='复制密钥' />
                </div>
                <div>
                  <Label>cURL 示例</Label>
                  <div className='bg-muted mt-1 flex items-start gap-2 rounded-md p-3'>
                    <pre className='min-w-0 flex-1 overflow-x-auto text-xs break-all whitespace-pre-wrap'>
                      {curl}
                    </pre>
                    <CopyButton value={curl} tooltip='复制 cURL' />
                  </div>
                </div>
              </>
            )}
            {props.secret.portalPassword && (
              <div className='grid gap-2 rounded-md border p-3'>
                <Label>可视化门户密码（仅显示这一次）</Label>
                <div className='flex items-center gap-2'>
                  <code className='min-w-0 flex-1 text-sm break-all'>
                    {props.secret.portalPassword}
                  </code>
                  <CopyButton
                    value={props.secret.portalPassword}
                    tooltip='复制门户密码'
                  />
                </div>
                {props.secret.portalURL && (
                  <div className='flex items-center gap-2 border-t pt-2'>
                    <code className='min-w-0 flex-1 truncate text-xs'>
                      {absolutePortalURL(props.secret.portalURL)}
                    </code>
                    <CopyButton
                      value={absolutePortalURL(props.secret.portalURL)}
                      tooltip='复制门户地址'
                    />
                  </div>
                )}
              </div>
            )}
          </div>
        )}
        <DialogFooter>
          <Button onClick={() => props.onOpenChange(false)}>我已保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function AccessLogDialog(props: {
  item: AccountDataAPI | null
  onOpenChange: (open: boolean) => void
}) {
  const query = useQuery({
    queryKey: ['account-data-api-logs', props.item?.id],
    queryFn: () => listAccountDataAPIAccessLogs(props.item?.id ?? 0),
    enabled: props.item != null,
  })
  const logs = query.data?.data.items ?? []
  let content: ReactNode
  if (query.isLoading) {
    content = <div className='py-10 text-center'>加载中...</div>
  } else if (logs.length === 0) {
    content = (
      <div className='text-muted-foreground py-10 text-center'>
        暂无访问记录
      </div>
    )
  } else {
    content = (
      <div className='grid gap-2'>
        {logs.map((log) => (
          <AccessLogRow key={log.id} log={log} />
        ))}
      </div>
    )
  }
  return (
    <Dialog open={props.item != null} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[calc(100dvh-2rem)] max-w-3xl overflow-y-auto'>
        <DialogHeader>
          <DialogTitle>访问日志 · {props.item?.name}</DialogTitle>
          <DialogDescription>
            仅记录访问元数据，不保存响应内容或搜索条件，保留 90 天。
          </DialogDescription>
        </DialogHeader>
        {content}
      </DialogContent>
    </Dialog>
  )
}

function AccessLogRow({ log }: { log: AccountDataAPIAccessLog }) {
  return (
    <div className='grid gap-2 rounded-md border p-3 text-sm sm:grid-cols-4'>
      <Detail label='时间' value={formatTime(log.created_at)} />
      <Detail label='IP' value={log.ip_address} />
      <Detail label='状态' value={String(log.status_code)} />
      <Detail label='返回条数' value={String(log.result_count)} />
      <Detail label='耗时' value={`${log.duration_ms} ms`} />
      <Detail
        label='访问方式'
        value={log.auth_type === 'portal' ? '可视化门户' : 'API Key'}
      />
      <Detail
        label='操作'
        value={
          ({ login: '登录', query: '查询', export: '导出', logout: '退出' }[
            log.action
          ] ??
            log.action) ||
          '查询'
        }
      />
      {log.error_code && (
        <div className='sm:col-span-4'>
          <Detail label='错误码' value={log.error_code} />
        </div>
      )}
    </div>
  )
}
