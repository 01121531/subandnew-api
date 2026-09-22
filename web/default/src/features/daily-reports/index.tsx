import { useQuery } from '@tanstack/react-query'
import { Download, RefreshCw } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getManagedInstances } from '@/features/managed-instances/api'
import type { ManagedInstance } from '@/features/managed-instances/types'

import {
  exportDailyReportAccounts,
  exportDailyReportUploads,
  getDailyReportUploads,
  getDailyReports,
  listDailyReportSupplierOptions,
  listDailyReportRules,
  type DailyReportOverview,
  type DailyReportRule,
  type UploadDay,
  type UploadRecord,
  type UploadsOverview,
} from './api'

type ReportTab = 'suppliers' | 'accounts' | 'uploads'
type ReportQuery<T> = {
  data?: { data: T }
  isError: boolean
  error: unknown
}

const formatNumber = (value: number) =>
  value.toLocaleString(undefined, { maximumFractionDigits: 2 })
const formatCost = (value: number, currency: string) =>
  currency ? `${formatNumber(value)} ${currency}` : '未提供'
const today = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Shanghai',
}).format(new Date())

function shiftDate(date: string, days: number) {
  const value = new Date(`${date}T00:00:00+08:00`)
  value.setUTCDate(value.getUTCDate() + days)
  return value.toISOString().slice(0, 10)
}

function instanceIDs(instances: ManagedInstance[], selected: string) {
  return selected ? [Number(selected)] : instances.map((item) => item.id)
}

function queryErrorMessage(error: unknown) {
  if (error instanceof Error && error.message) return error.message
  return '日报数据加载失败'
}

function MetricCard({ label, value }: { label: string; value: ReactNode }) {
  return (
    <Card size='sm'>
      <CardHeader>
        <CardTitle className='text-muted-foreground text-xs'>{label}</CardTitle>
      </CardHeader>
      <CardContent className='text-xl font-semibold'>{value}</CardContent>
    </Card>
  )
}

export function DailyReports() {
  const [tab, setTab] = useState<ReportTab>(() => {
    if (typeof window === 'undefined') return 'suppliers'
    const value = new URLSearchParams(window.location.search).get('tab')
    return value === 'accounts' || value === 'uploads' ? value : 'suppliers'
  })
  const [supplierDate, setSupplierDate] = useState(today)
  const [supplierInstance, setSupplierInstance] = useState('')
  const [supplier, setSupplier] = useState('')
  const [supplierRule, setSupplierRule] = useState('')
  const [supplierMode, setSupplierMode] = useState<'full' | 'filtered'>('full')
  const [accountDate, setAccountDate] = useState(today)
  const [accountInstance, setAccountInstance] = useState('')
  const [uploadStart, setUploadStart] = useState(shiftDate(today, -6))
  const [uploadEnd, setUploadEnd] = useState(today)
  const [uploadInstance, setUploadInstance] = useState('')
  const [uploadDetail, setUploadDetail] = useState<UploadRecord | null>(null)

  const instancesQuery = useQuery({
    queryKey: ['daily-report-instances'],
    queryFn: () => getManagedInstances({ search: '', kind: '', status: '' }),
    staleTime: 60_000,
  })
  const rulesQuery = useQuery({
    queryKey: ['daily-report-rules'],
    queryFn: listDailyReportRules,
    staleTime: 60_000,
  })
  const instanceItems = instancesQuery.data?.data?.items
  const instances = useMemo(
    () => (instanceItems ?? []) as ManagedInstance[],
    [instanceItems]
  )
  const claudeInstances = useMemo(
    () => instances.filter((item) => item.kind === 'claude_gateway'),
    [instances]
  )
  const mercerInstances = useMemo(
    () => instances.filter((item) => item.kind === 'mercer_router'),
    [instances]
  )
  const nevermoreInstances = useMemo(
    () => instances.filter((item) => item.kind === 'nevermore'),
    [instances]
  )
  const ruleItems = rulesQuery.data?.data?.items
  const rules = useMemo(() => ruleItems ?? [], [ruleItems])
  const supplierRules = useMemo(
    () =>
      rules.filter(
        (rule) =>
          rule.enabled &&
          claudeInstances.some((item) => item.id === rule.instance_id) &&
          (!supplierInstance ||
            rule.instance_id === Number(supplierInstance)) &&
          (!supplier || rule.supplier_code === supplier)
      ),
    [claudeInstances, supplier, supplierInstance, rules]
  )
  const supplierNames = useMemo(
    () => [
      ...new Map(
        supplierRules.map((rule) => [rule.supplier_code, rule.supplier_name])
      ),
    ],
    [supplierRules]
  )

  const supplierOptionsQuery = useQuery({
    queryKey: ['daily-report-supplier-options', supplierInstance],
    queryFn: () => listDailyReportSupplierOptions(Number(supplierInstance)),
    enabled: tab === 'suppliers' && Boolean(supplierInstance),
    staleTime: 60_000,
  })
  const supplierOptions = useMemo(() => {
    const options = new Map(supplierNames)
    for (const item of supplierOptionsQuery.data?.data?.items ?? []) {
      options.set(item.code, item.name)
    }
    return [...options]
  }, [supplierNames, supplierOptionsQuery.data])

  const supplierQuery = useQuery({
    queryKey: [
      'daily-report-suppliers',
      supplierDate,
      supplierInstance,
      supplier,
      supplierRule,
    ],
    queryFn: () =>
      getDailyReports({
        date: supplierDate,
        source: 'managed_accounts',
        instance_ids: instanceIDs(claudeInstances, supplierInstance),
        instance_kind: 'claude_gateway',
        supplier_code: supplier || undefined,
        rule_id: supplierRule ? Number(supplierRule) : undefined,
      }),
    enabled: tab === 'suppliers' && claudeInstances.length > 0,
    staleTime: 30_000,
  })
  const accountQuery = useQuery({
    queryKey: ['daily-report-accounts', accountDate, accountInstance],
    queryFn: () =>
      getDailyReports({
        date: accountDate,
        source: 'managed_accounts',
        instance_ids: instanceIDs(mercerInstances, accountInstance),
        instance_kind: 'mercer_router',
      }),
    enabled: tab === 'accounts' && mercerInstances.length > 0,
    staleTime: 30_000,
  })
  const uploadQuery = useQuery({
    queryKey: ['daily-report-uploads', uploadStart, uploadEnd, uploadInstance],
    queryFn: () =>
      getDailyReportUploads({
        start_date: uploadStart,
        end_date: uploadEnd,
        instance_ids: instanceIDs(nevermoreInstances, uploadInstance),
      }),
    enabled: tab === 'uploads' && nevermoreInstances.length > 0,
    staleTime: 30_000,
  })

  const exportCurrent = async () => {
    try {
      if (tab === 'suppliers') {
        await exportDailyReportAccounts({
          date: supplierDate,
          instance_ids: instanceIDs(claudeInstances, supplierInstance),
          instance_kind: 'claude_gateway',
          supplier_code: supplier || undefined,
          rule_id: supplierRule ? Number(supplierRule) : undefined,
          mode: supplierMode,
        })
      } else if (tab === 'accounts') {
        await exportDailyReportAccounts({
          date: accountDate,
          instance_ids: instanceIDs(mercerInstances, accountInstance),
          instance_kind: 'mercer_router',
        })
      } else {
        await exportDailyReportUploads({
          start_date: uploadStart,
          end_date: uploadEnd,
          instance_ids: instanceIDs(nevermoreInstances, uploadInstance),
        })
      }
      toast.success('日报已导出')
    } catch {
      toast.error('日报导出失败')
    }
  }

  const refreshCurrent = () => {
    if (tab === 'suppliers') void supplierQuery.refetch()
    if (tab === 'accounts') void accountQuery.refetch()
    if (tab === 'uploads') void uploadQuery.refetch()
  }

  const changeTab = (value: string) => {
    const next = value as ReportTab
    setTab(next)
    if (typeof window !== 'undefined') {
      const url = new URL(window.location.href)
      url.searchParams.set('tab', next)
      window.history.replaceState({}, '', url)
    }
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>日报</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button variant='outline' onClick={refreshCurrent}>
          <RefreshCw />
          刷新
        </Button>
        <Button onClick={() => void exportCurrent()}>
          <Download />
          导出 Excel
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content className='space-y-4'>
        <Tabs value={tab} onValueChange={changeTab}>
          <TabsList className='w-full justify-start overflow-x-auto sm:w-fit'>
            <TabsTrigger value='suppliers'>供货商数据</TabsTrigger>
            <TabsTrigger value='accounts'>每日账户数据</TabsTrigger>
            <TabsTrigger value='uploads'>上传账户数量</TabsTrigger>
          </TabsList>
        </Tabs>
        {tab === 'suppliers' && (
          <SupplierReport
            date={supplierDate}
            setDate={setSupplierDate}
            instance={supplierInstance}
            setInstance={(value) => {
              setSupplierInstance(value)
              setSupplier('')
              setSupplierRule('')
            }}
            supplier={supplier}
            setSupplier={(value) => {
              setSupplier(value)
              setSupplierRule('')
            }}
            rule={supplierRule}
            setRule={setSupplierRule}
            mode={supplierMode}
            setMode={setSupplierMode}
            instances={claudeInstances}
            rules={supplierRules}
            supplierNames={supplierOptions}
            query={supplierQuery}
          />
        )}
        {tab === 'accounts' && (
          <AccountReport
            title='MercerRouter 每日账户数据'
            date={accountDate}
            setDate={setAccountDate}
            instance={accountInstance}
            setInstance={setAccountInstance}
            instances={mercerInstances}
            query={accountQuery}
          />
        )}
        {tab === 'uploads' && (
          <UploadReport
            startDate={uploadStart}
            endDate={uploadEnd}
            setStartDate={setUploadStart}
            setEndDate={setUploadEnd}
            instance={uploadInstance}
            setInstance={setUploadInstance}
            instances={nevermoreInstances}
            query={uploadQuery}
            detail={uploadDetail}
            setDetail={setUploadDetail}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function DateRangeFields(props: {
  startDate: string
  endDate: string
  setStartDate: (value: string) => void
  setEndDate: (value: string) => void
}) {
  return (
    <div className='grid gap-2 sm:grid-cols-2'>
      <Input
        type='date'
        value={props.startDate}
        onChange={(event) => props.setStartDate(event.target.value)}
        aria-label='开始日期'
      />
      <Input
        type='date'
        value={props.endDate}
        onChange={(event) => props.setEndDate(event.target.value)}
        aria-label='结束日期'
      />
    </div>
  )
}

function SupplierReport(props: {
  date: string
  setDate: (value: string) => void
  instance: string
  setInstance: (value: string) => void
  supplier: string
  setSupplier: (value: string) => void
  rule: string
  setRule: (value: string) => void
  mode: 'full' | 'filtered'
  setMode: (value: 'full' | 'filtered') => void
  instances: ManagedInstance[]
  rules: DailyReportRule[]
  supplierNames: [string, string][]
  query: ReportQuery<DailyReportOverview>
}) {
  return (
    <AccountReport title='Claude Gateway 供货商数据' {...props} showFilters />
  )
}

function AccountReport(props: {
  title: string
  date: string
  setDate: (value: string) => void
  instance: string
  setInstance: (value: string) => void
  supplier?: string
  setSupplier?: (value: string) => void
  rule?: string
  setRule?: (value: string) => void
  mode?: 'full' | 'filtered'
  setMode?: (value: 'full' | 'filtered') => void
  instances: ManagedInstance[]
  rules?: DailyReportRule[]
  supplierNames?: [string, string][]
  showFilters?: boolean
  query: ReportQuery<DailyReportOverview>
}) {
  const showFilters = props.showFilters === true
  const mode = props.mode ?? 'full'
  const data = props.query.data?.data as DailyReportOverview | undefined
  const rows = data?.items ?? []
  const totals = rows.reduce(
    (result, row) => {
      const metric = mode === 'filtered' ? row.filtered : row.full
      if (mode === 'full' && result.instances.has(row.snapshot.instance_id)) {
        result.accounts = Math.max(result.accounts, row.snapshot.account_count)
        return result
      }
      result.instances.add(row.snapshot.instance_id)
      result.requests += metric.requests
      result.tokens += metric.total_tokens
      result.cost += metric.cost
      result.costAvailable = result.costAvailable && Boolean(metric.currency)
      result.accounts += row.snapshot.account_count
      return result
    },
    {
      requests: 0,
      tokens: 0,
      cost: 0,
      costAvailable: true,
      accounts: 0,
      instances: new Set<number>(),
    }
  )
  return (
    <>
      <Card>
        <CardContent
          className={
            showFilters
              ? 'grid gap-3 p-4 md:grid-cols-[180px_220px_220px_220px_1fr]'
              : 'grid gap-3 p-4 md:grid-cols-[180px_280px_1fr]'
          }
        >
          <Input
            type='date'
            value={props.date}
            onChange={(event) => props.setDate(event.target.value)}
            aria-label='日报日期'
          />
          <NativeSelect
            value={props.instance}
            onChange={(event) => {
              props.setInstance(event.target.value)
              props.setSupplier?.('')
              props.setRule?.('')
            }}
          >
            <NativeSelectOption value=''>
              {showFilters ? '全部 Claude Gateway' : '全部 MercerRouter'}
            </NativeSelectOption>
            {props.instances.map((item) => (
              <NativeSelectOption key={item.id} value={String(item.id)}>
                {item.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          {showFilters && (
            <>
              <NativeSelect
                value={props.supplier ?? ''}
                onChange={(event) => {
                  props.setSupplier?.(event.target.value)
                  props.setRule?.('')
                }}
              >
                <NativeSelectOption value=''>全部供应商</NativeSelectOption>
                {(props.supplierNames ?? []).map(([code, name]) => (
                  <NativeSelectOption key={code} value={code}>
                    {name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
              <NativeSelect
                value={props.rule ?? ''}
                onChange={(event) => props.setRule?.(event.target.value)}
              >
                <NativeSelectOption value=''>全部已启用规则</NativeSelectOption>
                {(props.rules ?? []).map((rule) => (
                  <NativeSelectOption key={rule.id} value={String(rule.id)}>
                    {rule.supplier_name} ·{' '}
                    {rule.source_template_name || '手动规则'}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
              <div className='bg-muted flex items-center gap-1 rounded-md p-1'>
                <Button
                  size='sm'
                  variant={mode === 'full' ? 'secondary' : 'ghost'}
                  onClick={() => props.setMode?.('full')}
                >
                  全部消耗
                </Button>
                <Button
                  size='sm'
                  variant={mode === 'filtered' ? 'secondary' : 'ghost'}
                  onClick={() => props.setMode?.('filtered')}
                >
                  筛选消耗
                </Button>
              </div>
            </>
          )}
          {!showFilters && (
            <div className='text-muted-foreground self-center text-sm'>
              北京时间自然日 · MercerRouter 聚合用量
            </div>
          )}
        </CardContent>
      </Card>
      {props.query.isError && (
        <ErrorNotice message={queryErrorMessage(props.query.error)} />
      )}
      <div className='grid gap-3 sm:grid-cols-4'>
        <MetricCard label='请求数' value={formatNumber(totals.requests)} />
        <MetricCard label='总 Token' value={formatNumber(totals.tokens)} />
        <MetricCard
          label='费用'
          value={totals.costAvailable ? formatNumber(totals.cost) : '未提供'}
        />
        <MetricCard label='账号数' value={formatNumber(totals.accounts)} />
      </div>
      <Card>
        <CardHeader>
          <CardTitle>{props.title}</CardTitle>
        </CardHeader>
        <CardContent className='p-0'>
          <div className='overflow-x-auto'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>实例</TableHead>
                  <TableHead>供应商</TableHead>
                  <TableHead>请求数</TableHead>
                  <TableHead>总 Token</TableHead>
                  <TableHead>费用</TableHead>
                  <TableHead>账号数</TableHead>
                  <TableHead>活跃账号</TableHead>
                  <TableHead>状态</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.length === 0 ? (
                  <EmptyRow colSpan={8} />
                ) : (
                  rows.map((row) => {
                    const metric = mode === 'filtered' ? row.filtered : row.full
                    return (
                      <TableRow
                        key={`${row.snapshot.instance_id}-${row.snapshot.supplier_code}`}
                      >
                        <TableCell>
                          {props.instances.find(
                            (item) => item.id === row.snapshot.instance_id
                          )?.name ?? row.snapshot.instance_id}
                        </TableCell>
                        <TableCell>
                          {row.snapshot.supplier_name ||
                            row.snapshot.supplier_code ||
                            '全量'}
                        </TableCell>
                        <TableCell>{formatNumber(metric.requests)}</TableCell>
                        <TableCell>
                          {formatNumber(metric.total_tokens)}
                        </TableCell>
                        <TableCell>
                          {formatCost(metric.cost, metric.currency)}
                        </TableCell>
                        <TableCell>{row.snapshot.account_count}</TableCell>
                        <TableCell>{row.snapshot.active_count}</TableCell>
                        <TableCell>
                          <Badge
                            variant={
                              row.snapshot.status === 'succeeded'
                                ? 'secondary'
                                : 'destructive'
                            }
                          >
                            {row.snapshot.stale
                              ? '旧快照'
                              : row.snapshot.status}
                          </Badge>
                        </TableCell>
                      </TableRow>
                    )
                  })
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>
    </>
  )
}

function UploadReport(props: {
  startDate: string
  endDate: string
  setStartDate: (value: string) => void
  setEndDate: (value: string) => void
  instance: string
  setInstance: (value: string) => void
  instances: ManagedInstance[]
  query: ReportQuery<UploadsOverview>
  detail: UploadRecord | null
  setDetail: (value: UploadRecord | null) => void
}) {
  const data = props.query.data?.data as
    | Awaited<ReturnType<typeof getDailyReportUploads>>['data']
    | undefined
  const days = data?.days ?? []
  const total = days.reduce((sum, day) => sum + day.upload_count, 0)
  const submitted = days.reduce((sum, day) => sum + day.submitted, 0)
  const needsFix = days.reduce((sum, day) => sum + day.needs_fix, 0)
  const items = data?.items ?? []
  return (
    <>
      <Card>
        <CardContent className='grid gap-3 p-4 md:grid-cols-[1fr_1fr_240px_1fr]'>
          <DateRangeFields {...props} />
          <NativeSelect
            value={props.instance}
            onChange={(event) => props.setInstance(event.target.value)}
          >
            <NativeSelectOption value=''>
              全部 Nevermore 实例
            </NativeSelectOption>
            {props.instances.map((item) => (
              <NativeSelectOption key={item.id} value={String(item.id)}>
                {item.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <div className='text-muted-foreground self-center text-sm'>
            上传日期按北京时间计算
          </div>
        </CardContent>
      </Card>
      {props.query.isError && (
        <ErrorNotice message={queryErrorMessage(props.query.error)} />
      )}
      <div className='grid gap-3 sm:grid-cols-3'>
        <MetricCard label='上传账号数' value={formatNumber(total)} />
        <MetricCard label='已提交' value={formatNumber(submitted)} />
        <MetricCard label='NEEDS_FIX' value={formatNumber(needsFix)} />
      </div>
      <Card>
        <CardHeader>
          <CardTitle>每日上传趋势</CardTitle>
        </CardHeader>
        <CardContent className='grid gap-2 sm:grid-cols-2 lg:grid-cols-4'>
          {days.length === 0 ? (
            <p className='text-muted-foreground text-sm'>暂无上传记录</p>
          ) : (
            days.map((day) => <UploadDayCard key={day.date} day={day} />)
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>上传账号明细</CardTitle>
        </CardHeader>
        <CardContent className='p-0'>
          <div className='overflow-x-auto'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>日期</TableHead>
                  <TableHead>供应商</TableHead>
                  <TableHead>批次</TableHead>
                  <TableHead>账号标识</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>问题类型</TableHead>
                  <TableHead>已提交</TableHead>
                  <TableHead>详情</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <EmptyRow colSpan={8} />
                ) : (
                  items.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell>{item.beijing_date}</TableCell>
                      <TableCell>{item.vendor || item.vendor_id}</TableCell>
                      <TableCell>{item.batch}</TableCell>
                      <TableCell>{item.account_identifier}</TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            item.state === 'NEEDS_FIX'
                              ? 'destructive'
                              : 'secondary'
                          }
                        >
                          {item.state}
                        </Badge>
                      </TableCell>
                      <TableCell>{item.issue_type || '-'}</TableCell>
                      <TableCell>{item.submitted ? '是' : '否'}</TableCell>
                      <TableCell>
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={() => props.setDetail(item)}
                        >
                          详情
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>
      <Dialog
        open={props.detail != null}
        onOpenChange={(open) => !open && props.setDetail(null)}
      >
        <DialogContent className='sm:max-w-xl'>
          <DialogHeader>
            <DialogTitle>上传记录详情</DialogTitle>
            <DialogDescription>Nevermore 本地采集快照</DialogDescription>
          </DialogHeader>
          {props.detail && (
            <DetailGrid
              values={{
                DeliveryID: props.detail.id,
                供应商: props.detail.vendor,
                批次: props.detail.batch,
                账号标识: props.detail.account_identifier,
                状态: props.detail.state,
                问题类型: props.detail.issue_type || '-',
                已提交: props.detail.submitted ? '是' : '否',
                创建时间: props.detail.created_at,
                更新时间: props.detail.updated_at,
                北京时间日期: props.detail.beijing_date,
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}

function UploadDayCard({ day }: { day: UploadDay }) {
  return (
    <Card size='sm'>
      <CardContent className='space-y-1 p-3'>
        <div className='font-medium'>{day.date}</div>
        <div className='text-muted-foreground text-sm'>
          上传 {day.upload_count} · 已提交 {day.submitted}
        </div>
        <div className='text-sm'>NEEDS_FIX {day.needs_fix}</div>
        <div className='text-muted-foreground text-xs'>
          {Object.entries(day.by_state)
            .map(([key, value]) => `${key}: ${value}`)
            .join(' · ')}
        </div>
      </CardContent>
    </Card>
  )
}

function DetailGrid({ values }: { values: Record<string, ReactNode> }) {
  return (
    <dl className='grid gap-3 sm:grid-cols-2'>
      {Object.entries(values).map(([key, value]) => (
        <div key={key} className='min-w-0 rounded-md border p-3'>
          <dt className='text-muted-foreground text-xs'>{key}</dt>
          <dd className='mt-1 text-sm break-words'>{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function ErrorNotice({ message }: { message: string }) {
  return (
    <Card className='border-destructive/40'>
      <CardContent className='text-destructive p-4 text-sm'>
        {message}
      </CardContent>
    </Card>
  )
}

function EmptyRow({ colSpan }: { colSpan: number }) {
  return (
    <TableRow>
      <TableCell
        colSpan={colSpan}
        className='text-muted-foreground h-24 text-center'
      >
        暂无数据
      </TableCell>
    </TableRow>
  )
}
