import { useQuery } from '@tanstack/react-query'
import { X, MessageSquareWarning } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import { mailboxApi } from '../api'
import { canSubmit } from '../lib/guards'
import type { AccountType } from '../types'
import { QueryState, Status } from './common'
import { Credentials } from './credentials'
import { DraftSubmission, type LeaveState } from './draft-submission'

export function AccountDetail(props: {
  accountType: AccountType
  id: number
  csrf: string
  onClose: () => void
  onSubmitted: () => void
  onReported: () => void
}) {
  const { t } = useTranslation()
  const leave = useRef<LeaveState>({ dirty: false, pending: false })
  const [issue, setIssue] = useState(false)
  const query = useQuery({
    queryKey: ['mailbox', 'account', props.id, props.accountType],
    queryFn: ({ signal }) =>
      mailboxApi.account(props.id, signal, props.accountType),
  })
  function close() {
    if (leave.current.pending) return
    if (
      leave.current.dirty &&
      !window.confirm(t('mailboxPortal.discardDrafts'))
    ) {
      return
    }
    props.onClose()
  }
  const account = query.data
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) close()
      }}
    >
      <SheetContent
        className='w-full gap-0 sm:max-w-[560px]'
        showCloseButton={false}
      >
        <SheetHeader className='shrink-0 border-b pr-14'>
          <SheetTitle className='text-base [overflow-wrap:anywhere]'>
            {account?.email ?? t('mailboxPortal.accountDetails')}
          </SheetTitle>
          <Button
            variant='ghost'
            size='icon'
            className='absolute top-3 right-3'
            title={t('mailboxPortal.close')}
            aria-label={t('mailboxPortal.close')}
            onClick={close}
          >
            <X />
          </Button>
        </SheetHeader>
        <div className='min-h-0 flex-1 overflow-y-auto px-4 sm:px-6'>
          <QueryState
            pending={query.isPending}
            error={query.error}
            retry={() => void query.refetch()}
          >
            {account && (
              <div
                key={`${account.account_type}:${account.assignment_id}:${account.assignment_version}:${account.status}`}
              >
                <div className='flex flex-wrap items-center justify-between gap-2 border-b py-4'>
                  <span className='text-muted-foreground text-xs'>
                    {t('mailboxPortal.assignment', {
                      id: account.assignment_id,
                    })}
                  </span>
                  <Status status={account.status} />
                </div>
                {!issue && <Credentials account={account} csrf={props.csrf} />}
                {canSubmit(account) && (
                  <Button
                    variant='outline'
                    className='my-3'
                    onClick={() => {
                      if (leave.current.pending) return
                      if (
                        leave.current.dirty &&
                        !window.confirm(t('mailboxPortal.discardDrafts'))
                      ) {
                        return
                      }
                      setIssue(!issue)
                    }}
                  >
                    <MessageSquareWarning />
                    {t(issue ? 'mailbox.issues.back' : 'mailbox.issues.report')}
                  </Button>
                )}
                {canSubmit(account) && (
                  <DraftSubmission
                    key={String(issue)}
                    issue={issue}
                    account={account}
                    csrf={props.csrf}
                    leave={leave}
                    onSubmitted={issue ? props.onReported : props.onSubmitted}
                  />
                )}
              </div>
            )}
          </QueryState>
        </div>
      </SheetContent>
    </Sheet>
  )
}
