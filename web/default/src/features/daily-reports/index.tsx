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
  exportDailyReportSuppliers,
  exportDailyReportUploads,
  getDailyReportSupplierBills,
  getDailyReportUploads,
  getDailyReports,
  listDailyReportRules,
  type DailyReportOverview,
  type DailyReportRule,
  type SupplierBill,
  type SupplierBillsOverview,
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
  const [supplierStart, setSupplierStart] = useState(shiftDate(today, -6))
  const [supplierEnd, setSupplierEnd] = useState(today)
  const [supplierInstance, setSupplierInstance] = useState('')
  const [accountDate, setAccountDate] = useState(today)
  const [accountInstance, setAccountInstance] = useState('')
  const [accountSupplier, setAccountSupplier] = useState('')
  const [accountRule, setAccountRule] = useState('')
  const [accountMode, setAccountMode] = useState<'full' | 'filtered'>('full')
  const [uploadStart, setUploadStart] = useState(shiftDate(today, -6))
  const [uploadEnd, setUploadEnd] = useState(today)
  const [uploadInstance, setUploadInstance] = useState('')
  const [supplierDetail, setSupplierDetail] = useState<SupplierBill | null>(
    null
  )
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
  const routerInstances = useMemo(
    () => instances.filter((item) => item.kind === 'router'),
    [instances]
  )
  const claudeInstances = useMemo(
    () => instances.filter((item) => item.kind === 'claude_gateway'),
    [instances]
  )
  const nevermoreInstances = useMemo(
    () => instances.filter((item) => item.kind === 'nevermore'),
    [instances]
  )
  const ruleItems = rulesQuery.data?.data?.items
  const rules = useMemo(() => ruleItems ?? [], [ruleItems])
  const accountRules = useMemo(
    () =>
      rules.filter(
        (rule) =>
          rule.enabled &&
          (!accountInstance || rule.instance_id === Number(accountInstance)) &&
          (!accountSupplier || rule.supplier_code === accountSupplier)
      ),
    [accountInstance, accountSupplier, rules]
  )
  const supplierNames = useMemo(
    () => [
      ...new Map(
        accountRules.map((rule) => [rule.supplier_code, rule.supplier_name])
      ),
    ],
    [accountRules]
  )

  const supplierQuery = useQuery({
    queryKey: [
      'daily-report-suppliers',
      supplierStart,
      supplierEnd,
      supplierInstance,
    ],
    queryFn: () =>
      getDailyReportSupplierBills({
        start_date: supplierStart,
        end_date: supplierEnd,
        instance_ids: instanceIDs(routerInstances, supplierInstance),
      }),
    enabled: tab === 'suppliers' && routerInstances.length > 0,
    staleTime: 30_000,
  })
  const accountQuery = useQuery({
    queryKey: [
      'daily-report-accounts',
      accountDate,
      accountInstance,
      accountSupplier,
      accountRule,
    ],
    queryFn: () =>
      getDailyReports({
        date: accountDate,
        source: 'managed_accounts',
        instance_ids: instanceIDs(claudeInstances, accountInstance),
        supplier_code: accountSupplier || undefined,
        rule_id: accountRule ? Number(accountRule) : undefined,
      }),
    enabled: tab === 'accounts' && claudeInstances.length > 0,
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
        if (
          supplierStart > supplierEnd ||
          shiftDate(supplierStart, 31) < supplierEnd
        ) {
          toast.error('供货商数据最多查询 31 天')
          return
        }
        await exportDailyReportSuppliers({
          start_date: supplierStart,
          end_date: supplierEnd,
          instance_ids: instanceIDs(routerInstances, supplierInstance),
        })
      } else if (tab === 'accounts') {
        await exportDailyReportAccounts({
          date: accountDate,
          instance_ids: instanceIDs(claudeInstances, accountInstance),
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
            startDate={supplierStart}
            endDate={supplierEnd}
            setStartDate={setSupplierStart}
            setEndDate={setSupplierEnd}
            instance={supplierInstance}
            setInstance={setSupplierInstance}
            instances={routerInstances}
            query={supplierQuery}
            detail={supplierDetail}
            setDetail={setSupplierDetail}
          />
        )}
        {tab === 'accounts' && (
          <AccountReport
            date={accountDate}
            setDate={setAccountDate}
            instance={accountInstance}
            setInstance={setAccountInstance}
            supplier={accountSupplier}
            setSupplier={setAccountSupplier}
            rule={accountRule}
            setRule={setAccountRule}
            mode={accountMode}
            setMode={setAccountMode}
            instances={claudeInstances}
            rules={accountRules}
            supplierNames={supplierNames}
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
  startDate: string
  endDate: string
  setStartDate: (value: string) => void
  setEndDate: (value: string) => void
  instance: string
  setInstance: (value: string) => void
  instances: ManagedInstance[]
  query: ReportQuery<SupplierBillsOverview>
  detail: SupplierBill | null
  setDetail: (value: SupplierBill | null) => void
}) {
  const data = props.query.data?.data as
    | Awaited<ReturnType<typeof getDailyReportSupplierBills>>['data']
    | undefined
  const items = data?.items ?? []
  return (
    <>
      <Card>
        <CardContent className='grid gap-3 p-4 md:grid-cols-[1fr_1fr_220px_auto]'>
          <DateRangeFields {...props} />
          <NativeSelect
            value={props.instance}
            onChange={(event) => props.setInstance(event.target.value)}
          >
            <NativeSelectOption value=''>全部 Router 实例</NativeSelectOption>
            {props.instances.map((item) => (
              <NativeSelectOption key={item.id} value={String(item.id)}>
                {item.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <div className='text-muted-foreground self-center text-sm'>
            北京时间 · 最多 31 天
          </div>
        </CardContent>
      </Card>
      {props.query.isError && (
        <ErrorNotice message={queryErrorMessage(props.query.error)} />
      )}
      <div className='grid gap-3 sm:grid-cols-3'>
        <MetricCard label='账单数' value={data?.bill_count ?? 0} />
        <MetricCard
          label='应付金额'
          value={formatNumber(data?.total_payable ?? 0)}
        />
        <MetricCard
          label='总请求次数'
          value={formatNumber(data?.total_requests ?? 0)}
        />
      </div>
      <Card>
        <CardHeader>
          <CardTitle>供应商账单</CardTitle>
        </CardHeader>
        <CardContent className='p-0'>
          <div className='overflow-x-auto'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>供应商</TableHead>
                  <TableHead>账单日期</TableHead>
                  <TableHead>请求数</TableHead>
                  <TableHead>输入 Token</TableHead>
                  <TableHead>输出 Token</TableHead>
                  <TableHead>缓存 Token</TableHead>
                  <TableHead>原价</TableHead>
                  <TableHead>应付</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <EmptyRow colSpan={10} />
                ) : (
                  items.map((item) => (
                    <TableRow
                      key={`${item.instance_id}-${item.supplier_code}-${item.bill_date}`}
                    >
                      <TableCell>
                        {item.supplier_name || item.supplier_code}
                      </TableCell>
                      <TableCell>{item.bill_date}</TableCell>
                      <TableCell>{formatNumber(item.requests)}</TableCell>
                      <TableCell>{formatNumber(item.input_tokens)}</TableCell>
                      <TableCell>{formatNumber(item.output_tokens)}</TableCell>
                      <TableCell>{formatNumber(item.cache_tokens)}</TableCell>
                      <TableCell>
                        {formatNumber(item.original_amount)} {item.currency}
                      </TableCell>
                      <TableCell>
                        {formatNumber(item.payable_amount)} {item.currency}
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            item.status === 'succeeded'
                              ? 'secondary'
                              : 'destructive'
                          }
                        >
                          {item.status}
                        </Badge>
                      </TableCell>
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
        <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-xl'>
          <DialogHeader>
            <DialogTitle>供应商账单详情</DialogTitle>
            <DialogDescription>账单原始周期数据与采集快照</DialogDescription>
          </DialogHeader>
          {props.detail && (
            <DetailGrid
              values={{
                供应商: props.detail.supplier_name,
                账单日期: props.detail.bill_date,
                时区: props.detail.timezone,
                请求数: formatNumber(props.detail.requests),
                输入Token: formatNumber(props.detail.input_tokens),
                输出Token: formatNumber(props.detail.output_tokens),
                缓存Token: formatNumber(props.detail.cache_tokens),
                原价金额: formatNumber(props.detail.original_amount),
                应付金额: formatNumber(props.detail.payable_amount),
                Token明细: JSON.stringify(props.detail.token_details ?? {}),
                快照时间: formatTimestamp(props.detail.observed_at),
                状态: props.detail.status,
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}

function AccountReport(props: {
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
  const data = props.query.data?.data as DailyReportOverview | undefined
  const rows = data?.items ?? []
  const totals = rows.reduce(
    (result, row) => {
      const metric = props.mode === 'filtered' ? row.filtered : row.full
      if (
        props.mode === 'full' &&
        result.instances.has(row.snapshot.instance_id)
      ) {
        result.accounts = Math.max(result.accounts, row.snapshot.account_count)
        return result
      }
      result.instances.add(row.snapshot.instance_id)
      result.requests += metric.requests
      result.tokens += metric.total_tokens
      result.cost += metric.cost
      result.accounts += row.snapshot.account_count
      return result
    },
    {
      requests: 0,
      tokens: 0,
      cost: 0,
      accounts: 0,
      instances: new Set<number>(),
    }
  )
  return (
    <>
      <Card>
        <CardContent className='grid gap-3 p-4 md:grid-cols-[180px_220px_220px_220px_1fr]'>
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
              props.setSupplier('')
              props.setRule('')
            }}
          >
            <NativeSelectOption value=''>
              全部 Claude Gateway
            </NativeSelectOption>
            {props.instances.map((item) => (
              <NativeSelectOption key={item.id} value={String(item.id)}>
                {item.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <NativeSelect
            value={props.supplier}
            onChange={(event) => {
              props.setSupplier(event.target.value)
              props.setRule('')
            }}
          >
            <NativeSelectOption value=''>全部供应商</NativeSelectOption>
            {props.supplierNames.map(([code, name]) => (
              <NativeSelectOption key={code} value={code}>
                {name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <NativeSelect
            value={props.rule}
            onChange={(event) => props.setRule(event.target.value)}
          >
            <NativeSelectOption value=''>全部已启用规则</NativeSelectOption>
            {props.rules.map((rule) => (
              <NativeSelectOption key={rule.id} value={String(rule.id)}>
                {rule.supplier_name} · {rule.source_template_name || '手动规则'}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <div className='bg-muted flex items-center gap-1 rounded-md p-1'>
            <Button
              size='sm'
              variant={props.mode === 'full' ? 'secondary' : 'ghost'}
              onClick={() => props.setMode('full')}
            >
              全部消耗
            </Button>
            <Button
              size='sm'
              variant={props.mode === 'filtered' ? 'secondary' : 'ghost'}
              onClick={() => props.setMode('filtered')}
            >
              筛选消耗
            </Button>
          </div>
        </CardContent>
      </Card>
      {props.query.isError && (
        <ErrorNotice message={queryErrorMessage(props.query.error)} />
      )}
      <div className='grid gap-3 sm:grid-cols-4'>
        <MetricCard label='请求数' value={formatNumber(totals.requests)} />
        <MetricCard label='总 Token' value={formatNumber(totals.tokens)} />
        <MetricCard label='费用' value={formatNumber(totals.cost)} />
        <MetricCard label='账号数' value={formatNumber(totals.accounts)} />
      </div>
      <Card>
        <CardHeader>
          <CardTitle>每日账户消耗</CardTitle>
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
                    const metric =
                      props.mode === 'filtered' ? row.filtered : row.full
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
                          {formatNumber(metric.cost)} {metric.currency}
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

function formatTimestamp(value: number) {
  if (!value) return '-'
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    dateStyle: 'medium',
    timeStyle: 'medium',
  }).format(new Date(value * 1000))
}
