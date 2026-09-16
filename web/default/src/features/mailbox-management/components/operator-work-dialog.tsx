import { X } from 'lucide-react'
import { useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog'
import { useAuthStore } from '@/stores/auth-store'

import { mailboxApi } from '../api'
import { useMailboxQuery } from '../hooks'
import { canMailboxWork } from '../lib/permissions'
import type { WorkSelection } from '../lib/work'
import type { Operator, WorkAccount, WorkRangeQuery } from '../types'
import { QueryState } from './common'
import { WorkAccounts } from './work-accounts'
import { WorkDateFilter, WorkSummaryView } from './work-controls'
import { WorkHistoryView } from './work-history'

export function OperatorWorkDialog(props: {
  operator: Operator
  range: WorkRangeQuery
  selection: WorkSelection
  onClose: () => void
}) {
  const user = useAuthStore((state) => state.auth.user)
  if (!canMailboxWork(user)) return null
  return <OperatorWorkContent {...props} />
}

function OperatorWorkContent(props: {
  operator: Operator
  range: WorkRangeQuery
  selection: WorkSelection
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [range, setRange] = useState(props.range)
  const [selection, setSelection] = useState(props.selection)
  const [accountsRevision, setAccountsRevision] = useState(0)
  const [account, setAccount] = useState<WorkAccount>()
  const listRef = useRef<HTMLDivElement>(null)
  const scroll = useRef(0)
  const returnFocus = useRef<HTMLElement | null>(null)
  const historyRef = useRef<HTMLDivElement>(null)
  const query = useMailboxQuery(
    ['operator-work-summary', props.operator.id, range],
    (signal) => mailboxApi.workSummary(props.operator.id, range, signal)
  )
  useLayoutEffect(() => {
    if (!account && listRef.current) {
      listRef.current.scrollTop = scroll.current
      returnFocus.current?.focus({ preventScroll: true })
    } else if (account) historyRef.current?.focus()
  }, [account])
  const operator = query.data?.operator ?? props.operator
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <DialogContent
        showCloseButton={false}
        className='flex h-[90dvh] max-h-[90dvh] min-w-0 flex-col gap-0 overflow-hidden rounded-lg p-0 sm:max-w-4xl'
      >
        <header className='flex shrink-0 items-start justify-between gap-2 border-b p-4'>
          <div className='min-w-0 space-y-2'>
            <DialogTitle className='text-lg break-all'>
              {operator.display_name || operator.username}
            </DialogTitle>
            <DialogDescription className='break-all'>
              {operator.username} · {t('mailbox.work.title')}
            </DialogDescription>
            <Badge variant='outline'>
              {t(
                operator.enabled
                  ? 'mailbox.admin.enabled'
                  : 'mailbox.admin.disabled'
              )}
            </Badge>
          </div>
          <Button
            variant='ghost'
            size='icon'
            aria-label={t('mailbox.admin.close')}
            title={t('mailbox.admin.close')}
            onClick={props.onClose}
          >
            <X />
          </Button>
        </header>
        <div
          ref={listRef}
          className={account ? 'hidden' : 'min-h-0 flex-1 overflow-y-auto p-4'}
        >
          <WorkDateFilter value={range} onChange={setRange} />
          <QueryState
            pending={query.isPending}
            error={query.error}
            retry={() => void query.refetch()}
          >
            {query.data && (
              <WorkSummaryView
                operatorName={operator.display_name || operator.username}
                summary={query.data.summary}
                range={query.data.range}
                onSelect={(value) => {
                  setSelection(value)
                  setAccountsRevision((revision) => revision + 1)
                }}
              />
            )}
          </QueryState>
          <WorkAccounts
            key={JSON.stringify([range, selection, accountsRevision])}
            operatorID={props.operator.id}
            range={range}
            selection={selection}
            onSelection={setSelection}
            onAccount={(item) => {
              scroll.current = listRef.current?.scrollTop ?? 0
              returnFocus.current =
                document.activeElement instanceof HTMLElement
                  ? document.activeElement
                  : null
              setAccount(item)
            }}
          />
        </div>
        {account && (
          <div
            ref={historyRef}
            tabIndex={-1}
            className='min-h-0 flex-1 overflow-y-auto p-4 outline-none'
          >
            <WorkHistoryView
              key={`${account.account_type}:${account.id}`}
              operatorID={props.operator.id}
              account={account}
              onBack={() => setAccount(undefined)}
            />
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
