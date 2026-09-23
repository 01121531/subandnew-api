import { useQuery } from '@tanstack/react-query'
import { CircleAlert, Loader2 } from 'lucide-react'
import type { ReactNode } from 'react'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  getUsageRecordsExportWarnings,
  type UsageRecordExportTask,
} from '@/features/usage-records/api'

export function WarningDetailsDialog({
  item,
}: {
  item: UsageRecordExportTask
}) {
  const query = useQuery({
    queryKey: ['managed-export-warnings', item.task_id],
    queryFn: () => getUsageRecordsExportWarnings(item.task_id),
    enabled: false,
    staleTime: 60_000,
  })
  if (item.warning_count <= 0) return null

  let body: ReactNode
  if (query.isLoading) {
    body = (
      <div className='text-muted-foreground flex items-center gap-2 text-sm'>
        <Loader2 className='size-4 animate-spin' />
        正在加载警告明细...
      </div>
    )
  } else if (query.isError) {
    body = (
      <div className='text-destructive flex items-center gap-2 text-sm'>
        <CircleAlert className='size-4' />
        警告明细加载失败，请稍后重试。
      </div>
    )
  } else if (query.data?.data) {
    const data = query.data.data
    body = (
      <>
        {data.messages.length > 0 && (
          <div className='bg-warning/10 text-warning rounded-md p-3 text-sm whitespace-pre-wrap'>
            {data.messages.map((message) => (
              <div key={message}>{message}</div>
            ))}
          </div>
        )}
        {data.truncated && (
          <div className='text-warning text-sm'>
            警告明细已截断，仅显示部分记录。
          </div>
        )}
        {data.items.length === 0 ? (
          <div className='text-muted-foreground rounded-md border border-dashed p-4 text-sm'>
            该任务只有警告数量记录，未保存逐条明细。
          </div>
        ) : (
          <div className='divide-border divide-y overflow-hidden rounded-md border'>
            {data.items.map((warning, index) => (
              <div
                key={`${String(warning.row ?? index)}-${warning.warning_code ?? 'warning'}`}
                className='grid gap-1 px-3 py-3 text-sm sm:grid-cols-[8rem_minmax(0,1fr)] sm:gap-3'
              >
                <div className='text-muted-foreground'>
                  {warning.account_name ||
                    warning.account_email ||
                    warning.account_id ||
                    `第 ${String(warning.row ?? index + 1)} 行`}
                </div>
                <div className='min-w-0 break-words'>
                  <div className='font-medium'>{warning.warning_message}</div>
                  {warning.warning_code && (
                    <div className='text-muted-foreground mt-1 font-mono text-xs'>
                      {warning.warning_code}
                    </div>
                  )}
                  {warning.instance_name && (
                    <div className='text-muted-foreground mt-1 text-xs'>
                      {warning.instance_name}
                    </div>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </>
    )
  } else {
    body = (
      <div className='text-muted-foreground text-sm'>
        暂无可展示的警告明细。
      </div>
    )
  }

  return (
    <Dialog>
      <DialogTrigger
        render={
          <button
            type='button'
            className='text-warning hover:text-warning/80 inline-flex items-center gap-1 underline-offset-2 hover:underline'
            aria-label={`查看 ${item.warning_count} 条警告详情`}
            onClick={(event) => {
              event.stopPropagation()
              void query.refetch()
            }}
          />
        }
      >
        · {item.warning_count} 条警告
      </DialogTrigger>
      <DialogContent className='max-h-[min(82vh,760px)] gap-0 overflow-hidden p-0 sm:max-w-2xl'>
        <DialogHeader className='border-border border-b px-5 py-4 pr-12'>
          <DialogTitle>导出警告详情</DialogTitle>
          <DialogDescription>
            {item.instance_name} · {item.task_id}
          </DialogDescription>
        </DialogHeader>
        <div className='max-h-[65vh] space-y-4 overflow-y-auto px-5 py-4'>
          {body}
        </div>
      </DialogContent>
    </Dialog>
  )
}
