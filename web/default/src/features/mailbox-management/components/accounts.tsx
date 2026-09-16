import {
  Eye,
  FileUp,
  KeyRound,
  UserRoundCheck,
  Trash2,
  ArchiveRestore,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useAuthStore } from '@/stores/auth-store'

import { mailboxApi } from '../api'
import { useMailboxQuery } from '../hooks'
import { canMailbox } from '../lib/permissions'
import type { Account, AccountType, VersionedID } from '../types'
import { AccountOperatorFilter } from './account-operator-filter'
import { ArchiveDialog } from './archive-dialog'
import { AssignDialog } from './assign-dialog'
import { Pager, QueryState, SearchBar, Status, Time } from './common'
import { CredentialDetail } from './credential-detail'
import { ExportCompletedButton } from './export-completed-button'
import { ImportDialog } from './import-dialog'
import { PoolScope } from './pool-scope'
import { TemporaryCvvDialog } from './temporary-cvv-dialog'

export function Accounts() {
  return (
    <PoolScope>
      {(accountType) => (
        <AccountPool key={accountType} accountType={accountType} />
      )}
    </PoolScope>
  )
}

function AccountPool(props: { accountType: AccountType }) {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const view = canMailbox(user, 'view')
  const assign = canMailbox(user, 'assign')
  const credentials = canMailbox(user, 'credentials')
  const manage = canMailbox(user, 'manage')
  const [archived, setArchived] = useState(false)
  const [archiveItems, setArchiveItems] = useState<VersionedID[]>()
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [operator, setOperator] = useState('')
  const [selected, setSelected] = useState<VersionedID[]>([])
  const [showImport, setShowImport] = useState(false)
  const [showAssign, setShowAssign] = useState(false)
  const [detail, setDetail] = useState<number>()
  const [accountID, setAccountID] = useState('')
  const [cvvAccount, setCvvAccount] = useState<Account>()
  const query = useMailboxQuery(
    ['accounts', props.accountType, archived, page, search, status, operator],
    (signal) =>
      mailboxApi.accounts(
        {
          account_type: props.accountType,
          archived,
          page,
          page_size: 20,
          search,
          status,
          operator_id: operator ? Number(operator) : undefined,
        },
        signal
      ),
    view
  )
  const rows = query.data?.items ?? []
  function toggle(item: VersionedID, checked: boolean) {
    setSelected((current) =>
      checked
        ? [
            ...current.filter((row) => row.id !== item.id),
            { id: item.id, version: item.version },
          ]
        : current.filter((row) => row.id !== item.id)
    )
  }
  function changePage(value: number) {
    setSelected([])
    setPage(value)
  }
  function openButton(id: number) {
    const row = rows.find((item) => item.id === id)
    return (
      <div className='flex shrink-0 items-center gap-1'>
        {!archived && (
          <Button
            variant='ghost'
            size='icon'
            title={t('mailbox.admin.details')}
            aria-label={t('mailbox.admin.details')}
            onClick={() => setDetail(id)}
          >
            <Eye />
          </Button>
        )}
        {manage && row && (
          <Button
            variant='ghost'
            size='icon'
            title={t(`mailbox.archive.${archived ? 'restore' : 'delete'}`)}
            aria-label={t(`mailbox.archive.${archived ? 'restore' : 'delete'}`)}
            disabled={query.isFetching || query.isError}
            onClick={() =>
              setArchiveItems([{ id: row.id, version: row.version }])
            }
          >
            {archived ? <ArchiveRestore /> : <Trash2 />}
          </Button>
        )}
        {row &&
          !archived &&
          props.accountType === 'opening' &&
          credentials &&
          canMailbox(user, 'manage') &&
          ['unassigned', 'pending', 'rejected'].includes(row.status) && (
            <Button
              variant='ghost'
              size='icon'
              title={t('mailbox.admin.provideCvv')}
              aria-label={t('mailbox.admin.provideCvv')}
              onClick={() => setCvvAccount(row)}
            >
              <KeyRound />
            </Button>
          )}
      </div>
    )
  }
  return (
    <section className='min-w-0 space-y-3'>
      <div className='flex flex-wrap items-center gap-2 pt-4'>
        {view && (
          <div
            className='flex flex-wrap gap-1'
            role='group'
            aria-label={t('mailbox.archive.view')}
          >
            {[false, true].map((value) => (
              <Button
                key={String(value)}
                variant={archived === value ? 'secondary' : 'ghost'}
                aria-pressed={archived === value}
                onClick={() => {
                  setArchived(value)
                  setStatus('')
                  setOperator('')
                  setSearch('')
                  changePage(1)
                }}
              >
                {t(`mailbox.archive.${value ? 'archived' : 'active'}`)}
              </Button>
            ))}
          </div>
        )}
        {manage && !archived && (
          <Button onClick={() => setShowImport(true)}>
            <FileUp />
            {t('mailbox.admin.importTitle')}
          </Button>
        )}
        {props.accountType === 'opening' &&
          !archived &&
          view &&
          credentials &&
          canMailbox(user, 'review') && <ExportCompletedButton />}
        {assign && !archived && (
          <Button
            variant='outline'
            disabled={
              view && (!selected.length || query.isFetching || query.isError)
            }
            onClick={() => setShowAssign(true)}
          >
            <UserRoundCheck />
            {t('mailbox.admin.assign')}
            {selected.length > 0 && ` (${selected.length})`}
          </Button>
        )}
        {manage && view && (
          <Button
            variant='outline'
            disabled={!selected.length || query.isFetching || query.isError}
            onClick={() => setArchiveItems([...selected])}
          >
            {archived ? <ArchiveRestore /> : <Trash2 />}
            {t(`mailbox.archive.${archived ? 'restore' : 'delete'}`)}
            {selected.length > 0 && ` (${selected.length})`}
          </Button>
        )}
        {!view && credentials && (
          <form
            className='flex min-w-0 flex-wrap gap-2'
            onSubmit={(event) => {
              event.preventDefault()
              const id = Number(accountID)
              if (Number.isSafeInteger(id) && id > 0) setDetail(id)
            }}
          >
            <Input
              className='w-44'
              type='number'
              min={1}
              step={1}
              required
              aria-label={t('mailbox.admin.accountID')}
              placeholder={t('mailbox.admin.accountID')}
              value={accountID}
              onChange={(event) => setAccountID(event.target.value)}
            />
            <Button type='submit' variant='outline'>
              <Eye />
              {t('mailbox.admin.details')}
            </Button>
          </form>
        )}
      </div>
      {view && (
        <>
          <SearchBar
            value={search}
            onChange={(value) => {
              setSearch(value)
              changePage(1)
            }}
            status={archived ? undefined : status}
            onStatus={
              archived
                ? undefined
                : (value) => {
                    setStatus(value)
                    changePage(1)
                  }
            }
            pending={query.isFetching}
            refresh={() => {
              setSelected([])
              void query.refetch()
            }}
          >
            {!archived && (
              <AccountOperatorFilter
                accountType={props.accountType}
                value={operator}
                onChange={(value) => {
                  setOperator(value)
                  changePage(1)
                }}
              />
            )}
          </SearchBar>
          <QueryState
            pending={query.isPending}
            error={query.error}
            retry={() => void query.refetch()}
            empty={rows.length === 0}
          >
            <div className='hidden md:block'>
              <Table>
                <TableHeader>
                  <TableRow>
                    {(assign || manage) && (
                      <TableHead className='w-10'>
                        <input
                          type='checkbox'
                          aria-label={t('mailbox.admin.selectPage')}
                          checked={
                            rows.length > 0 &&
                            rows.every((row) =>
                              selected.some((item) => item.id === row.id)
                            )
                          }
                          onChange={(event) =>
                            setSelected(
                              event.target.checked
                                ? rows.map((row) => ({
                                    id: row.id,
                                    version: row.version,
                                  }))
                                : []
                            )
                          }
                        />
                      </TableHead>
                    )}
                    <TableHead>{t('mailbox.admin.email')}</TableHead>
                    <TableHead>{t('mailbox.admin.status')}</TableHead>
                    <TableHead>{t('mailbox.admin.operator')}</TableHead>
                    <TableHead>
                      {t(
                        archived
                          ? 'mailbox.archive.archivedAt'
                          : 'mailbox.admin.assignedAt'
                      )}
                    </TableHead>
                    <TableHead>
                      <span className='sr-only'>
                        {t('mailbox.admin.actions')}
                      </span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((row) => (
                    <TableRow key={row.id}>
                      {(assign || manage) && (
                        <TableCell>
                          <input
                            type='checkbox'
                            aria-label={t('mailbox.admin.selectEmail', {
                              email: row.email,
                            })}
                            checked={selected.some(
                              (item) => item.id === row.id
                            )}
                            onChange={(event) =>
                              toggle(row, event.target.checked)
                            }
                          />
                        </TableCell>
                      )}
                      <TableCell className='max-w-72 break-all whitespace-normal'>
                        <span className='font-medium'>{row.email}</span>
                        {props.accountType === 'opening' && row.card_last4 && (
                          <div className='text-muted-foreground text-xs'>
                            {t('mailbox.admin.cardEnding', {
                              last4: row.card_last4,
                            })}
                          </div>
                        )}
                        <div className='text-muted-foreground text-xs'>
                          #{row.id} / v{row.version}
                        </div>
                      </TableCell>
                      <TableCell>
                        {archived ? (
                          t('mailbox.archive.archived')
                        ) : (
                          <Status value={row.status} />
                        )}
                      </TableCell>
                      <TableCell className='max-w-48 break-all whitespace-normal'>
                        {row.operator_name || '--'}
                      </TableCell>
                      <TableCell>
                        <Time
                          value={
                            archived ? (row.archived_at ?? 0) : row.assigned_at
                          }
                        />
                      </TableCell>
                      <TableCell>{openButton(row.id)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            <div className='grid gap-3 md:hidden'>
              {rows.map((row) => (
                <article
                  key={row.id}
                  className='min-w-0 space-y-3 rounded-lg border p-3'
                >
                  <div className='flex items-start gap-3'>
                    {(assign || manage) && (
                      <input
                        className='mt-1'
                        type='checkbox'
                        aria-label={t('mailbox.admin.selectEmail', {
                          email: row.email,
                        })}
                        checked={selected.some((item) => item.id === row.id)}
                        onChange={(event) => toggle(row, event.target.checked)}
                      />
                    )}
                    <h3 className='min-w-0 flex-1 text-sm font-medium break-all'>
                      {row.email}
                      {props.accountType === 'opening' && row.card_last4 && (
                        <span className='text-muted-foreground block text-xs font-normal'>
                          {t('mailbox.admin.cardEnding', {
                            last4: row.card_last4,
                          })}
                        </span>
                      )}
                    </h3>
                    {openButton(row.id)}
                  </div>
                  <div className='flex flex-wrap items-center justify-between gap-2 text-sm'>
                    {archived ? (
                      t('mailbox.archive.archived')
                    ) : (
                      <Status value={row.status} />
                    )}
                    <span className='break-all'>
                      {row.operator_name || '--'}
                    </span>
                  </div>
                  <div className='text-muted-foreground flex flex-wrap justify-between gap-2 text-xs'>
                    <span>
                      #{row.id} / v{row.version}
                    </span>
                    <Time
                      value={
                        archived ? (row.archived_at ?? 0) : row.assigned_at
                      }
                    />
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
            onPage={changePage}
          />
        </>
      )}
      {archiveItems && (
        <ArchiveDialog
          accountType={props.accountType}
          items={archiveItems}
          restore={archived}
          onClose={() => {
            setArchiveItems(undefined)
            setSelected([])
            void query.refetch()
          }}
        />
      )}
      {cvvAccount && (
        <TemporaryCvvDialog
          account={cvvAccount}
          onClose={() => setCvvAccount(undefined)}
        />
      )}
      {showImport && (
        <ImportDialog
          accountType={props.accountType}
          onClose={() => setShowImport(false)}
        />
      )}
      {showAssign && (
        <AssignDialog
          accountType={props.accountType}
          items={selected}
          onClose={() => {
            setShowAssign(false)
            setSelected([])
          }}
        />
      )}
      {detail !== undefined && (
        <CredentialDetail
          accountType={props.accountType}
          key={detail}
          id={detail}
          canView={view}
          canCredentials={credentials}
          onClose={() => setDetail(undefined)}
        />
      )}
    </section>
  )
}
