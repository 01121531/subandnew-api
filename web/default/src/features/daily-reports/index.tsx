import { useQuery } from '@tanstack/react-query'
import { Download, RefreshCw } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
import { getManagedInstances } from '@/features/managed-instances/api'
import type { ManagedInstance } from '@/features/managed-instances/types'

import { exportDailyReport, getDailyReports, listDailyReportRules } from './api'

const formatNumber = (value: number) =>
  value.toLocaleString(undefined, { maximumFractionDigits: 2 })
const today = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Shanghai',
}).format(new Date())

export function DailyReports() {
  const [date, setDate] = useState(today)
  const [source, setSource] = useState('')
  const [instanceId, setInstanceId] = useState('')
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
  const reportsQuery = useQuery({
    queryKey: ['daily-reports', date, source, instanceId],
    queryFn: () =>
      getDailyReports({
        date,
        source,
        instance_ids: instanceId ? [Number(instanceId)] : undefined,
      }),
    enabled: Boolean(date),
    staleTime: 30_000,
  })
  const instances = (instancesQuery.data?.data?.items ??
    []) as ManagedInstance[]
  const rows = useMemo(
    () => reportsQuery.data?.data?.items ?? [],
    [reportsQuery.data]
  )
  const totals = useMemo(
    () =>
      rows.reduce(
        (acc, row) => {
          acc.requests += row.full.requests
          acc.tokens += row.full.total_tokens
          acc.cost += row.full.cost
          acc.accounts += row.snapshot.account_count
          return acc
        },
        { requests: 0, tokens: 0, cost: 0, accounts: 0 }
      ),
    [rows]
  )

  async function handleExport() {
    try {
      await exportDailyReport({
        date,
        source,
        instance_ids: instanceId ? [Number(instanceId)] : undefined,
      })
      toast.success('日报已导出')
    } catch {
      toast.error('日报导出失败')
    }
  }

  let tableBody: ReactNode
  if (reportsQuery.isLoading) {
    tableBody = (
      <TableRow>
        <TableCell colSpan={9}>加载中...</TableCell>
      </TableRow>
    )
  } else if (rows.length === 0) {
    tableBody = (
      <TableRow>
        <TableCell colSpan={9}>暂无日报数据</TableCell>
      </TableRow>
    )
  } else {
    tableBody = rows.map((row) => (
      <TableRow key={`${row.snapshot.instance_id}-${row.snapshot.source}`}>
        <TableCell>
          {instances.find((item) => item.id === row.snapshot.instance_id)
            ?.name ?? row.snapshot.instance_id}
        </TableCell>
        <TableCell>{row.snapshot.source}</TableCell>
        <TableCell>{formatNumber(row.full.requests)}</TableCell>
        <TableCell>{formatNumber(row.full.total_tokens)}</TableCell>
        <TableCell>
          {formatNumber(row.full.cost)} {row.full.currency}
        </TableCell>
        <TableCell>{row.snapshot.account_count}</TableCell>
        <TableCell>{row.snapshot.channel_count}</TableCell>
        <TableCell>{row.snapshot.upload_count}</TableCell>
        <TableCell>
          <Badge
            variant={
              row.snapshot.status === 'succeeded' ? 'secondary' : 'destructive'
            }
          >
            {row.snapshot.stale ? '旧快照' : row.snapshot.status}
          </Badge>
        </TableCell>
      </TableRow>
    ))
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>日报</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button variant='outline' onClick={() => void reportsQuery.refetch()}>
          <RefreshCw />
          刷新
        </Button>
        <Button onClick={() => void handleExport()}>
          <Download />
          导出 Excel
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content className='space-y-4'>
        <Card>
          <CardContent className='grid gap-3 p-4 md:grid-cols-[180px_220px_220px_1fr]'>
            <Input
              type='date'
              value={date}
              onChange={(event) => setDate(event.target.value)}
              aria-label='日报日期'
            />
            <NativeSelect
              value={instanceId}
              onChange={(event) => setInstanceId(event.target.value)}
            >
              <NativeSelectOption value=''>全部实例</NativeSelectOption>
              {instances.map((item) => (
                <NativeSelectOption key={item.id} value={String(item.id)}>
                  {item.name}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <NativeSelect
              value={source}
              onChange={(event) => setSource(event.target.value)}
            >
              <NativeSelectOption value=''>账号管理</NativeSelectOption>
              <NativeSelectOption value='nevermore'>
                Nevermore 上传
              </NativeSelectOption>
              <NativeSelectOption value='router'>
                Router 账单
              </NativeSelectOption>
            </NativeSelect>
            <div className='text-muted-foreground self-center text-sm'>
              北京时间 · 规则 {rulesQuery.data?.data?.items?.length ?? 0} 条
            </div>
          </CardContent>
        </Card>
        <div className='grid gap-3 sm:grid-cols-4'>
          {[
            ['请求数', totals.requests],
            ['总 Token', totals.tokens],
            ['费用', totals.cost],
            ['账号数', totals.accounts],
          ].map(([label, value]) => (
            <Card key={String(label)} size='sm'>
              <CardHeader>
                <CardTitle className='text-muted-foreground text-xs'>
                  {label}
                </CardTitle>
              </CardHeader>
              <CardContent className='text-xl font-semibold'>
                {formatNumber(Number(value))}
              </CardContent>
            </Card>
          ))}
        </div>
        <Card>
          <CardHeader>
            <CardTitle>供应商与平台日报</CardTitle>
          </CardHeader>
          <CardContent className='p-0'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>实例</TableHead>
                  <TableHead>来源</TableHead>
                  <TableHead>请求数</TableHead>
                  <TableHead>总 Token</TableHead>
                  <TableHead>费用</TableHead>
                  <TableHead>账号</TableHead>
                  <TableHead>渠道</TableHead>
                  <TableHead>上传</TableHead>
                  <TableHead>状态</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>{tableBody}</TableBody>
            </Table>
          </CardContent>
        </Card>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
