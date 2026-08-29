/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import {
  ChevronLeft,
  ChevronRight,
  Download,
  FileSpreadsheet,
  LoaderCircle,
  LockKeyhole,
  LogOut,
  RefreshCw,
  ShieldCheck,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'

import { MultiSelect } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { AccountFilterPanel } from '@/features/managed-accounts/account-filter-panel'
import {
  accountFilterSnapshot,
  type AccountAdvancedFilter,
  type AccountFilterField,
} from '@/features/managed-accounts/account-filtering'

import {
  exportPortal,
  getPortalSession,
  loginPortal,
  logoutPortal,
  PortalRequestError,
  queryPortal,
} from './api'
import type {
  PortalQuery,
  PortalResult,
  PortalSelection,
  PortalSession,
} from './types'

const fieldLabels: Record<string, string> = {
  instance_id: '实例 ID',
  account_id: '账号 ID',
  instance_name: '实例',
  platform: '平台',
  name: '账号名称',
  email: '邮箱',
  note: '备注',
  ownership: '账号归属',
  type: '账号类型',
  group: '分组',
  status: '状态',
  available: '可用性',
  source_name: '工作节点',
  created_at: '录入时间',
  last_activity_at: '最后活动',
  requests: '请求数',
  tokens: '总 Token',
  amount: '消费金额',
}

const sortableFields = new Set([
  'name',
  'created_at',
  'last_activity_at',
  'status',
  'requests',
  'tokens',
  'amount',
])

function emptyQuery(pageSize: number): PortalQuery {
  return {
    include_terms: [],
    exclude_terms: [],
    match_mode: 'all',
    rules: [],
    search: '',
    sort_by: '',
    sort_order: 'desc',
    page: 1,
    page_size: pageSize,
  }
}

function selectionKey(item: PortalSelection) {
  return `${item.instance_id}\u0000${item.account_id}`
}

function formatValue(field: string, value: unknown) {
  if (value == null || value === '') return '--'
  if (field === 'available') return value ? '可用' : '不可用'
  if (field === 'created_at' || field === 'last_activity_at') {
    const timestamp = Number(value)
    return timestamp > 0
      ? new Intl.DateTimeFormat('zh-CN', {
          timeZone: 'Asia/Shanghai',
          dateStyle: 'medium',
          timeStyle: 'short',
        }).format(timestamp * 1000)
      : '--'
  }
  if (field === 'amount') return `US$${Number(value).toFixed(8)}`
  if (field === 'requests' || field === 'tokens') {
    return new Intl.NumberFormat('zh-CN').format(Number(value))
  }
  return String(value)
}

function observedLabel(value: string | number) {
  if (!value) return '--'
  const date = new Date(typeof value === 'number' ? value * 1000 : value)
  return Number.isNaN(date.getTime())
    ? '--'
    : new Intl.DateTimeFormat('zh-CN', {
        timeZone: 'Asia/Shanghai',
        dateStyle: 'medium',
        timeStyle: 'medium',
      }).format(date)
}

export function AccountDataPortal({ slug }: { slug: string }) {
  const [session, setSession] = useState<PortalSession | null>(null)
  const [checking, setChecking] = useState(true)
  const [password, setPassword] = useState('')
  const [loginPending, setLoginPending] = useState(false)
  const [query, setQuery] = useState<PortalQuery>(emptyQuery(50))
  const [filter, setFilter] = useState<AccountAdvancedFilter>({
    match_mode: 'all',
    rules: [],
  })
  const [result, setResult] = useState<PortalResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<Map<string, PortalSelection>>(
    new Map()
  )
  const [exporting, setExporting] = useState(false)

  useEffect(() => {
    void getPortalSession(slug)
      .then((value) => {
        if (!value.authenticated) {
          setSession(null)
          return
        }
        setSession(value)
        setQuery(emptyQuery(Math.min(value.page_size || 50, 50)))
      })
      .catch(() => setSession(null))
      .finally(() => setChecking(false))
  }, [slug])

  const requestQuery = useMemo(
    () => ({ ...query, ...accountFilterSnapshot(filter) }),
    [filter, query]
  )
  const load = useCallback(async () => {
    if (!session) return
    setLoading(true)
    try {
      setResult(await queryPortal(slug, session.csrf_token, requestQuery))
    } catch (error) {
      if (error instanceof PortalRequestError && error.status === 401) {
        setSession(null)
      } else {
        toast.error(error instanceof Error ? error.message : '数据加载失败')
      }
    } finally {
      setLoading(false)
    }
  }, [requestQuery, session, slug])

  useEffect(() => {
    if (session) void load()
  }, [load, session])
  const filterSignature = JSON.stringify([
    query.include_terms,
    query.exclude_terms,
    filter,
  ])
  useEffect(() => {
    setSelected(new Map())
  }, [filterSignature])

  const login = async () => {
    setLoginPending(true)
    try {
      const value = await loginPortal(slug, password)
      setSession(value)
      setQuery(emptyQuery(Math.min(value.page_size || 50, 50)))
      setPassword('')
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '登录失败')
    } finally {
      setLoginPending(false)
    }
  }
  if (checking) {
    return <PortalLoading />
  }
  if (!session) {
    return (
      <PortalLogin
        password={password}
        pending={loginPending}
        onChange={setPassword}
        onSubmit={() => void login()}
      />
    )
  }

  const columns = [...new Set(['instance_id', 'account_id', ...session.fields])]
  const allowedFields = session.filter_fields.filter(
    (field): field is AccountFilterField =>
      [
        'name',
        'email',
        'account_id',
        'note',
        'ownership',
        'instance',
        'platform',
        'type',
        'group',
        'status',
        'source',
        'available',
      ].includes(field)
  )
  const options = Object.fromEntries(allowedFields.map((field) => [field, []]))
  for (const item of result?.items ?? []) {
    for (const field of allowedFields) {
      let sourceField: string = field
      if (field === 'instance') sourceField = 'instance_name'
      if (field === 'source') sourceField = 'source_name'
      const value = item[sourceField]
      if (value == null || value === '') continue
      const list = options[field] as Array<{ value: string; label: string }>
      if (!list.some((option) => option.value === String(value))) {
        list.push({ value: String(value), label: String(value) })
      }
    }
  }
  const pageSelections = (result?.items ?? []).map((item) => ({
    instance_id: Number(item.instance_id),
    account_id: String(item.account_id),
  }))
  const allPageSelected =
    pageSelections.length > 0 &&
    pageSelections.every((item) => selected.has(selectionKey(item)))

  const runExport = async (mode: 'filtered' | 'selected') => {
    setExporting(true)
    try {
      const file = await exportPortal(
        slug,
        session.csrf_token,
        requestQuery,
        mode,
        mode === 'selected' ? [...selected.values()] : []
      )
      const url = URL.createObjectURL(file.blob)
      const link = document.createElement('a')
      link.href = url
      link.download = file.fileName
      link.click()
      URL.revokeObjectURL(url)
      toast.success('Excel 已生成')
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '导出失败')
    } finally {
      setExporting(false)
    }
  }

  return (
    <main className='bg-background min-h-dvh overflow-x-hidden'>
      <header className='bg-background/95 sticky top-0 z-20 border-b backdrop-blur'>
        <div className='mx-auto flex min-h-16 max-w-[1600px] items-center gap-3 px-4 sm:px-6'>
          <div className='bg-primary/10 text-primary flex size-10 items-center justify-center rounded-md'>
            <ShieldCheck />
          </div>
          <div className='min-w-0 flex-1'>
            <h1 className='truncate font-semibold'>{session.name}</h1>
            <p className='text-muted-foreground truncate text-xs'>
              {session.description || '账号数据授权门户'}
            </p>
          </div>
          <Button
            variant='ghost'
            size='icon'
            aria-label='退出登录'
            onClick={() =>
              void logoutPortal(slug, session.csrf_token).finally(() =>
                setSession(null)
              )
            }
          >
            <LogOut />
          </Button>
        </div>
      </header>
      <div className='mx-auto grid max-w-[1600px] gap-4 p-3 sm:p-6'>
        {(result?.stale || result?.partial) && (
          <div className='border-warning/40 bg-warning/5 text-warning rounded-md border px-4 py-3 text-sm'>
            {result.partial
              ? '部分实例暂无快照，当前展示已有数据。'
              : '当前展示最后一次成功采集的数据。'}
          </div>
        )}
        <section className='grid grid-cols-2 gap-2 md:grid-cols-4'>
          <Metric title='匹配账号' value={result?.pagination.total ?? '--'} />
          {session.fields.includes('available') && (
            <>
              <Metric
                title='可用账号'
                value={result?.summary.available ?? '--'}
              />
              <Metric
                title='不可用账号'
                value={result?.summary.unavailable ?? '--'}
              />
            </>
          )}
          <Metric
            title='采集时间'
            value={observedLabel(
              result?.observed_at ?? session.last_observed_at
            )}
            compact
          />
        </section>
        <section className='grid gap-3 rounded-md border p-3 sm:p-4'>
          <div className='grid gap-2 md:grid-cols-2'>
            <MultiSelect
              selected={query.include_terms}
              onChange={(values) =>
                setQuery((old) => ({ ...old, include_terms: values, page: 1 }))
              }
              options={[]}
              maxValues={50}
              onLimitExceeded={() => toast.error('最多输入 50 个值')}
              placeholder='包含任一值，支持换行或逗号'
            />
            <MultiSelect
              selected={query.exclude_terms}
              onChange={(values) =>
                setQuery((old) => ({ ...old, exclude_terms: values, page: 1 }))
              }
              options={[]}
              maxValues={50}
              onLimitExceeded={() => toast.error('最多输入 50 个值')}
              placeholder='排除任一值，支持换行或逗号'
            />
          </div>
          <AccountFilterPanel
            value={filter}
            onChange={(value) => {
              setFilter(value)
              setQuery((old) => ({ ...old, page: 1 }))
            }}
            options={options}
            templatesEnabled={false}
            allowedFields={allowedFields}
          />
          <div className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_11rem_9rem_auto]'>
            <Input
              value={query.search}
              onChange={(event) =>
                setQuery((old) => ({
                  ...old,
                  search: event.target.value,
                  page: 1,
                }))
              }
              placeholder='在开放字段内继续搜索'
            />
            <NativeSelect
              value={query.sort_by}
              onChange={(event) =>
                setQuery((old) => ({
                  ...old,
                  sort_by: event.target.value,
                  page: 1,
                }))
              }
            >
              <NativeSelectOption value=''>默认排序</NativeSelectOption>
              {columns
                .filter((field) => sortableFields.has(field))
                .map((field) => (
                  <NativeSelectOption key={field} value={field}>
                    {fieldLabels[field] ?? field}
                  </NativeSelectOption>
                ))}
            </NativeSelect>
            <NativeSelect
              value={query.sort_order}
              onChange={(event) =>
                setQuery((old) => ({
                  ...old,
                  sort_order: event.target.value as 'asc' | 'desc',
                  page: 1,
                }))
              }
            >
              <NativeSelectOption value='desc'>降序</NativeSelectOption>
              <NativeSelectOption value='asc'>升序</NativeSelectOption>
            </NativeSelect>
            <Button
              variant='outline'
              disabled={loading}
              onClick={() => void load()}
            >
              <RefreshCw className={loading ? 'animate-spin' : ''} />
              刷新
            </Button>
          </div>
        </section>
        <section className='overflow-hidden rounded-md border'>
          <div className='flex flex-col gap-2 border-b p-3 sm:flex-row sm:items-center'>
            <span className='text-muted-foreground flex-1 text-sm'>
              已选择 {selected.size} / 当前筛选 {result?.pagination.total ?? 0}
            </span>
            <Button
              variant='outline'
              disabled={exporting}
              onClick={() => void runExport('filtered')}
            >
              <Download />
              导出筛选结果
            </Button>
            <Button
              disabled={exporting || selected.size === 0}
              onClick={() => void runExport('selected')}
            >
              <FileSpreadsheet />
              导出已选账号
            </Button>
          </div>
          {loading && !result && (
            <div className='flex min-h-64 items-center justify-center'>
              <LoaderCircle className='animate-spin' />
            </div>
          )}
          {(!loading || result != null) &&
            (result?.items.length ?? 0) === 0 && (
              <div className='text-muted-foreground py-20 text-center'>
                没有符合条件的账号
              </div>
            )}
          {(!loading || result != null) && (result?.items.length ?? 0) > 0 && (
            <>
              <div className='hidden overflow-x-auto md:block'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className='w-12'>
                        <Checkbox
                          checked={allPageSelected}
                          onCheckedChange={(checked) =>
                            setSelected((old) => {
                              const next = new Map(old)
                              pageSelections.forEach((item) =>
                                checked
                                  ? next.set(selectionKey(item), item)
                                  : next.delete(selectionKey(item))
                              )
                              return next
                            })
                          }
                        />
                      </TableHead>
                      {columns.map((field) => (
                        <TableHead key={field}>
                          {fieldLabels[field] ?? field}
                        </TableHead>
                      ))}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {result?.items.map((item) => {
                      const identity = {
                        instance_id: Number(item.instance_id),
                        account_id: String(item.account_id),
                      }
                      const key = selectionKey(identity)
                      return (
                        <TableRow key={key}>
                          <TableCell>
                            <Checkbox
                              checked={selected.has(key)}
                              onCheckedChange={(checked) =>
                                setSelected((old) => {
                                  const next = new Map(old)
                                  if (checked) next.set(key, identity)
                                  else next.delete(key)
                                  return next
                                })
                              }
                            />
                          </TableCell>
                          {columns.map((field) => (
                            <TableCell
                              key={field}
                              className='max-w-72 break-words'
                            >
                              {formatValue(field, item[field])}
                            </TableCell>
                          ))}
                        </TableRow>
                      )
                    })}
                  </TableBody>
                </Table>
              </div>
              <div className='grid gap-2 p-2 md:hidden'>
                {result?.items.map((item) => {
                  const identity = {
                    instance_id: Number(item.instance_id),
                    account_id: String(item.account_id),
                  }
                  const key = selectionKey(identity)
                  return (
                    <article key={key} className='rounded-md border p-3'>
                      <div className='flex items-start gap-3'>
                        <Checkbox
                          checked={selected.has(key)}
                          onCheckedChange={(checked) =>
                            setSelected((old) => {
                              const next = new Map(old)
                              if (checked) next.set(key, identity)
                              else next.delete(key)
                              return next
                            })
                          }
                        />
                        <div className='min-w-0 flex-1'>
                          <div className='font-medium break-words'>
                            {formatValue(
                              'name',
                              item.name ?? item.email ?? item.account_id
                            )}
                          </div>
                          <div className='text-muted-foreground mt-1 text-xs break-all'>
                            {String(item.account_id)}
                          </div>
                        </div>
                        {item.available != null && (
                          <Badge variant='outline'>
                            {item.available ? '可用' : '不可用'}
                          </Badge>
                        )}
                      </div>
                      <dl className='mt-3 grid grid-cols-2 gap-3 border-t pt-3'>
                        {columns
                          .filter(
                            (field) =>
                              ![
                                'name',
                                'email',
                                'account_id',
                                'available',
                              ].includes(field)
                          )
                          .map((field) => (
                            <div key={field} className='min-w-0'>
                              <dt className='text-muted-foreground text-xs'>
                                {fieldLabels[field] ?? field}
                              </dt>
                              <dd className='mt-1 text-sm break-words'>
                                {formatValue(field, item[field])}
                              </dd>
                            </div>
                          ))}
                      </dl>
                    </article>
                  )
                })}
              </div>
            </>
          )}
          <div className='flex flex-col gap-2 border-t p-3 sm:flex-row sm:items-center sm:justify-between'>
            <div className='flex items-center gap-2'>
              <Label>每页</Label>
              <NativeSelect
                className='w-24'
                value={String(query.page_size)}
                onChange={(event) =>
                  setQuery((old) => ({
                    ...old,
                    page_size: Number(event.target.value),
                    page: 1,
                  }))
                }
              >
                {[10, 20, 30, 40, 50, 100]
                  .filter((size) => size <= session.page_size)
                  .map((size) => (
                    <NativeSelectOption key={size} value={String(size)}>
                      {size}
                    </NativeSelectOption>
                  ))}
              </NativeSelect>
            </div>
            <div className='flex items-center justify-end gap-2'>
              <Button
                variant='outline'
                size='icon'
                disabled={query.page <= 1 || loading}
                onClick={() =>
                  setQuery((old) => ({ ...old, page: old.page - 1 }))
                }
              >
                <ChevronLeft />
              </Button>
              <span className='text-sm tabular-nums'>
                {query.page} /{' '}
                {Math.max(
                  1,
                  Math.ceil((result?.pagination.total ?? 0) / query.page_size)
                )}
              </span>
              <Button
                variant='outline'
                size='icon'
                disabled={!result?.pagination.has_more || loading}
                onClick={() =>
                  setQuery((old) => ({ ...old, page: old.page + 1 }))
                }
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </section>
      </div>
    </main>
  )
}

function PortalLogin(props: {
  password: string
  pending: boolean
  onChange: (value: string) => void
  onSubmit: () => void
}) {
  return (
    <main className='bg-muted/30 flex min-h-dvh items-center justify-center p-4'>
      <form
        className='bg-background grid w-full max-w-sm gap-5 rounded-md border p-6 shadow-sm'
        onSubmit={(event) => {
          event.preventDefault()
          props.onSubmit()
        }}
      >
        <div className='bg-primary/10 text-primary flex size-12 items-center justify-center rounded-md'>
          <LockKeyhole />
        </div>
        <div>
          <h1 className='text-xl font-semibold'>账号数据门户</h1>
          <p className='text-muted-foreground mt-1 text-sm'>
            请输入授权方提供的访问密码。
          </p>
        </div>
        <div className='grid gap-2'>
          <Label htmlFor='portal-password'>访问密码</Label>
          <Input
            id='portal-password'
            type='password'
            autoComplete='current-password'
            autoFocus
            value={props.password}
            onChange={(event) => props.onChange(event.target.value)}
          />
        </div>
        <Button
          type='submit'
          className='min-h-11'
          disabled={props.pending || !props.password}
        >
          {props.pending && <LoaderCircle className='animate-spin' />}登录
        </Button>
      </form>
    </main>
  )
}

function PortalLoading() {
  return (
    <main className='flex min-h-dvh items-center justify-center'>
      <LoaderCircle className='text-muted-foreground animate-spin' />
    </main>
  )
}
function Metric(props: {
  title: string
  value: string | number
  compact?: boolean
}) {
  return (
    <div className='min-w-0 rounded-md border p-3 sm:p-4'>
      <div className='text-muted-foreground text-xs sm:text-sm'>
        {props.title}
      </div>
      <div
        className={
          props.compact
            ? 'mt-2 text-sm font-semibold break-words'
            : 'mt-2 text-2xl font-semibold break-words tabular-nums'
        }
      >
        {props.value}
      </div>
    </div>
  )
}
