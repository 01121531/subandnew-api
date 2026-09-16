import { RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { mailboxApi } from '../api'
import { useMailboxQuery } from '../hooks'
import { safeCode } from '../lib/errors'
import type { AccountType, Audit } from '../types'
import { Pager, QueryState, Time } from './common'
import { PoolScope } from './pool-scope'

export function Audits() {
  return (
    <PoolScope>
      {(accountType) => (
        <PoolAudits key={accountType} accountType={accountType} />
      )}
    </PoolScope>
  )
}

function PoolAudits(props: { accountType: AccountType }) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const query = useMailboxQuery(['audits', props.accountType, page], (signal) =>
    mailboxApi.audits(
      { page, page_size: 20, account_type: props.accountType },
      signal
    )
  )
  const rows = query.data?.items ?? []
  function result(code: string, status: number) {
    return code
      ? t(`mailbox.errors.${safeCode(code)}`, {
          defaultValue: t('mailbox.errors.mailbox_request_failed'),
        })
      : String(status)
  }
  return (
    <section className='min-w-0'>
      <div className='flex justify-end py-4'>
        <Button
          variant='outline'
          size='icon'
          disabled={query.isFetching}
          title={t('mailbox.admin.refresh')}
          aria-label={t('mailbox.admin.refresh')}
          onClick={() => void query.refetch()}
        >
          <RefreshCw />
        </Button>
      </div>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
        empty={!rows.length}
      >
        <div className='hidden md:block'>
          <Table>
            <TableHeader>
              <TableRow>
                {['time', 'action', 'actor', 'targets', 'result', 'ip'].map(
                  (key) => (
                    <TableHead key={key}>{t(`mailbox.admin.${key}`)}</TableHead>
                  )
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((item) => (
                <TableRow key={item.id}>
                  <TableCell>
                    <Time value={item.created_at} />
                  </TableCell>
                  <TableCell className='max-w-48 break-words whitespace-normal'>
                    {t(`mailbox.admin.auditActions.${item.action}`, {
                      defaultValue: item.action,
                    })}
                  </TableCell>
                  <TableCell>
                    {t(
                      item.admin_id
                        ? 'mailbox.admin.adminActor'
                        : 'mailbox.admin.operatorActor',
                      { id: item.admin_id || item.operator_id }
                    )}
                  </TableCell>
                  <TableCell>
                    <AuditTargets item={item} />
                  </TableCell>
                  <TableCell className='max-w-60 break-words whitespace-normal'>
                    {result(item.error_code, item.status_code)}
                  </TableCell>
                  <TableCell>{item.ip_address || '--'}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <div className='grid gap-3 md:hidden'>
          {rows.map((item) => (
            <article
              key={item.id}
              className='min-w-0 space-y-2 rounded-lg border p-3 text-sm'
            >
              <h3 className='font-medium break-words'>
                {t(`mailbox.admin.auditActions.${item.action}`, {
                  defaultValue: item.action,
                })}
              </h3>
              <div className='text-muted-foreground flex flex-wrap justify-between gap-2 text-xs'>
                <Time value={item.created_at} />
                <span>
                  {t(
                    item.admin_id
                      ? 'mailbox.admin.adminActor'
                      : 'mailbox.admin.operatorActor',
                    { id: item.admin_id || item.operator_id }
                  )}
                </span>
              </div>
              <p className='break-words'>
                {result(item.error_code, item.status_code)}
              </p>
              <div className='flex flex-wrap justify-between gap-2 text-xs'>
                <AuditTargets item={item} />
                <span className='break-all'>{item.ip_address}</span>
              </div>
            </article>
          ))}
        </div>
      </QueryState>
      <Pager
        page={page}
        total={query.data?.total}
        hasMore={query.data?.has_more}
        pending={query.isFetching}
        onPage={setPage}
      />
    </section>
  )
}

function AuditTargets(props: { item: Audit }) {
  const { t } = useTranslation()
  return (
    <div className='grid gap-1'>
      {props.item.account_id > 0 && (
        <span>
          {t('mailbox.admin.accountID')}: #{props.item.account_id}
        </span>
      )}
      {!!props.item.target_operator_id && (
        <span>
          {t('mailbox.admin.targetOperator', {
            id: props.item.target_operator_id,
          })}
        </span>
      )}
      {!props.item.account_id && !props.item.target_operator_id && (
        <span>--</span>
      )}
    </div>
  )
}
