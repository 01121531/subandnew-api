import { useQuery } from '@tanstack/react-query'
import { Copy, ExternalLink, Plus, RefreshCw, Settings, X } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import '@/styles/supplier-portal.css'
import { hasPermission } from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { adminApi } from './admin-api'
import { Audits } from './components/audits'
import { BindingForm } from './components/binding-form'
import { Bindings } from './components/bindings'
import {
  Confirm,
  Empty,
  Pagination,
  QueryState,
  Time,
} from './components/common'
import { DefaultPolicyDialog } from './components/default-policy'
import { NamingSummary } from './components/naming-summary'
import { PortalSettingsDialog } from './components/portal-settings'
import { ResetPassword } from './components/reset-password'
import { SupplierActions } from './components/supplier-actions'
import { SupplierForm } from './components/supplier-form'
import { SupplierOwnership } from './components/take-ownership'
import { useAdminMutation } from './hooks/use-admin-mutation'
import type { FormLeaveGuard } from './hooks/use-form-leave-guard'
import type { Binding, Supplier } from './types'

export function Suppliers() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const isRoot = user?.role === ROLE.SUPER_ADMIN
  const canManage = hasPermission(user, 'supplier', 'manage')
  const canAudit = hasPermission(user, 'supplier', 'audit')
  const loginUrl = new URL('/supplier/sign-in', window.location.origin).href
  const copyLoginUrl = async () => {
    try {
      await navigator.clipboard.writeText(loginUrl)
      toast.success(t('supplier.loginLinkCopied'))
    } catch {
      toast.error(t('supplier.loginLinkCopyFailed'))
    }
  }
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState(false)
  const [defaultsOpen, setDefaultsOpen] = useState(false)
  const [portalSettingsOpen, setPortalSettingsOpen] = useState(false)
  const [detail, setDetail] = useState<{
    supplier: Supplier
    view: 'bindings' | 'audits' | 'edit'
  } | null>(null)
  const [reset, setReset] = useState<Supplier | null>(null)
  const [revoking, setRevoking] = useState<Supplier | null>(null)
  const [deleting, setDeleting] = useState<Supplier | null>(null)
  const query = useQuery({
    queryKey: ['supplier-admin', 'list', page],
    queryFn: () => adminApi.list(page),
  })
  const revoke = useAdminMutation(
    (id: number) => adminApi.revoke(id),
    () => {
      setRevoking(null)
      toast.success(t('supplier.completed'))
    }
  )
  const remove = useAdminMutation(
    (id: number) => adminApi.delete(id),
    () => {
      setDeleting(null)
      setDetail(null)
      toast.success(t('supplier.completed'))
      if (query.data?.items.length === 1 && page > 1) setPage(page - 1)
    }
  )
  const actions = (supplier: Supplier) => (
    <SupplierActions
      canManage={canManage}
      canAudit={canAudit}
      onAction={(action) => {
        if (action === 'bindings' || action === 'audits') {
          setDetail({ supplier, view: action })
        }
        if (action === 'edit' && canManage) {
          setDetail({ supplier, view: 'edit' })
        }
        if (action === 'reset') setReset(supplier)
        if (action === 'revoke') setRevoking(supplier)
        if (action === 'delete') setDeleting(supplier)
      }}
    />
  )
  return (
    <div className='supplier-portal flex min-h-0 flex-1 flex-col'>
      {portalSettingsOpen && isRoot && (
        <PortalSettingsDialog
          canManage={canManage}
          onClose={() => setPortalSettingsOpen(false)}
        />
      )}
      {defaultsOpen && isRoot && (
        <DefaultPolicyDialog
          canManage={canManage}
          onClose={() => setDefaultsOpen(false)}
        />
      )}
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('supplier.title')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions className='w-full justify-start sm:w-auto sm:justify-end'>
          {isRoot && (
            <Button
              variant='outline'
              onClick={() => setPortalSettingsOpen(true)}
            >
              <Settings />
              {t('supplier.portalSettings')}
            </Button>
          )}
          {isRoot && (
            <Button variant='outline' onClick={() => setDefaultsOpen(true)}>
              <Settings />
              {t('supplier.defaultPolicy')}
            </Button>
          )}
          <div className='flex items-center gap-1'>
            <a
              href={loginUrl}
              target='_blank'
              rel='noopener noreferrer'
              referrerPolicy='no-referrer'
              className={buttonVariants({ variant: 'outline' })}
            >
              <ExternalLink />
              {t('supplier.loginEntry')}
            </a>
            <Button
              variant='ghost'
              size='icon'
              title={t('supplier.copyLoginLink')}
              aria-label={t('supplier.copyLoginLink')}
              onClick={() => void copyLoginUrl()}
            >
              <Copy />
            </Button>
          </div>
          <Button
            variant='ghost'
            size='icon'
            title={t('supplier.refresh')}
            aria-label={t('supplier.refresh')}
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            <RefreshCw />
          </Button>
          {canManage && (
            <Button onClick={() => setEditing(true)}>
              <Plus />
              {t('supplier.create')}
            </Button>
          )}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='grid min-w-0 gap-4'>
            <QueryState
              pending={query.isPending}
              error={query.error}
              hasData={query.data !== undefined}
              retry={() => void query.refetch()}
            >
              {!query.data?.items.length ? (
                <Empty />
              ) : (
                <>
                  <div className='grid gap-3 md:hidden'>
                    {query.data.items.map((supplier) => (
                      <article
                        key={supplier.id}
                        className='min-w-0 rounded-md border p-3'
                      >
                        <div className='flex items-start gap-2'>
                          <div className='min-w-0 flex-1'>
                            <Button
                              variant='link'
                              className='h-auto max-w-full justify-start px-0 text-left font-semibold [overflow-wrap:anywhere] whitespace-normal'
                              onClick={() =>
                                setDetail({ supplier, view: 'bindings' })
                              }
                            >
                              {supplier.name}
                            </Button>
                            <p className='text-muted-foreground mt-1 text-xs break-all'>
                              {supplier.username}
                            </p>
                          </div>
                          {actions(supplier)}
                        </div>
                        <div className='mt-3 flex flex-wrap gap-1'>
                          <Badge variant='secondary'>
                            {supplier.enabled
                              ? t('supplier.enabled')
                              : t('supplier.disabled')}
                          </Badge>
                          {supplier.view_accounts && (
                            <Badge variant='outline'>
                              {t('supplier.accounts')}
                            </Badge>
                          )}
                          {supplier.view_usage && (
                            <Badge variant='outline'>
                              {t('supplier.usage')}
                            </Badge>
                          )}
                          {supplier.manage_proxies && (
                            <Badge variant='outline'>
                              {t('supplier.manageProxies')}
                            </Badge>
                          )}
                          {supplier.upload_accounts && (
                            <Badge variant='outline'>
                              {t('supplier.uploadAccounts')}
                            </Badge>
                          )}
                        </div>
                        <p className='text-muted-foreground mt-3 text-xs'>
                          {t('supplier.updatedAt')}:{' '}
                          <Time value={supplier.updated_at} />
                        </p>
                      </article>
                    ))}
                  </div>
                  <div className='hidden md:block'>
                    <Table>
                      <TableHeader>
                        <TableRow>
                          {[
                            'supplier',
                            'status',
                            'permissions',
                            'updatedAt',
                            'actions',
                          ].map((key) => (
                            <TableHead key={key}>
                              {t(`supplier.${key}`)}
                            </TableHead>
                          ))}
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {query.data.items.map((supplier) => (
                          <TableRow key={supplier.id}>
                            <TableCell className='w-1/3 max-w-80 min-w-40 whitespace-normal'>
                              <Button
                                variant='link'
                                className='h-auto max-w-full justify-start px-0 text-left font-semibold [overflow-wrap:anywhere] whitespace-normal'
                                onClick={() =>
                                  setDetail({ supplier, view: 'bindings' })
                                }
                              >
                                {supplier.name}
                              </Button>
                              <p className='text-muted-foreground mt-1 text-xs [overflow-wrap:anywhere]'>
                                {supplier.username}
                              </p>
                            </TableCell>
                            <TableCell>
                              <Badge
                                variant={
                                  supplier.enabled ? 'secondary' : 'outline'
                                }
                              >
                                {supplier.enabled
                                  ? t('supplier.enabled')
                                  : t('supplier.disabled')}
                              </Badge>
                            </TableCell>
                            <TableCell>
                              <div className='flex max-w-72 flex-wrap gap-1'>
                                {supplier.view_accounts && (
                                  <Badge variant='outline'>
                                    {t('supplier.accounts')}
                                  </Badge>
                                )}
                                {supplier.view_usage && (
                                  <Badge variant='outline'>
                                    {t('supplier.usage')}
                                  </Badge>
                                )}
                                {supplier.manage_proxies && (
                                  <Badge variant='secondary'>
                                    {t('supplier.manageProxies')}
                                  </Badge>
                                )}
                                {supplier.upload_accounts && (
                                  <Badge variant='secondary'>
                                    {t('supplier.uploadAccounts')}
                                  </Badge>
                                )}
                              </div>
                            </TableCell>
                            <TableCell>
                              <Time value={supplier.updated_at} />
                            </TableCell>
                            <TableCell>{actions(supplier)}</TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                </>
              )}
              <Pagination
                page={page}
                size={50}
                total={query.data?.total ?? 0}
                onPage={setPage}
                pending={query.isFetching}
              />
            </QueryState>
            {editing && canManage && (
              <SupplierForm onClose={() => setEditing(false)} />
            )}
            {reset && canManage && (
              <ResetPassword
                supplierId={reset.id}
                onClose={() => setReset(null)}
              />
            )}
            {detail && (
              <SupplierDetail
                key={detail.supplier.id}
                supplier={detail.supplier}
                initialView={detail.view}
                canManage={canManage}
                canAudit={canAudit}
                onClose={() => setDetail(null)}
              />
            )}
            <Confirm
              open={!!revoking}
              title={revoking?.name ?? ''}
              description={t('supplier.revokeConfirm')}
              pending={revoke.isPending}
              onClose={() => setRevoking(null)}
              onConfirm={() => {
                if (revoking && canManage && !revoke.isPending) {
                  revoke.mutate(revoking.id)
                }
              }}
            />
            <Confirm
              open={!!deleting}
              title={deleting?.name ?? ''}
              description={t('supplier.deleteSupplierConfirm')}
              pending={remove.isPending}
              onClose={() => setDeleting(null)}
              onConfirm={() => {
                if (deleting && canManage && !remove.isPending) {
                  remove.mutate(deleting.id)
                }
              }}
            />
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    </div>
  )
}

function SupplierDetail(props: {
  supplier: Supplier
  initialView: 'bindings' | 'audits' | 'edit'
  canManage: boolean
  canAudit: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [supplier, setSupplier] = useState(props.supplier)
  const [view, setView] = useState(props.initialView)
  const [binding, setBinding] = useState<Binding | 'new' | null>(null)
  const [pending, setPending] = useState(false)
  const guard = useRef<FormLeaveGuard>(null)
  const leave = (next: () => void) => {
    if (pending) return
    if (guard.current) guard.current.requestLeave(next)
    else next()
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) leave(props.onClose)
      }}
    >
      <DialogContent
        showCloseButton={false}
        className='supplier-portal flex h-[min(44rem,90dvh)] max-h-[90dvh] flex-col gap-0 overflow-hidden rounded-lg p-0 sm:max-w-4xl'
      >
        <header className='flex shrink-0 items-start justify-between gap-3 border-b p-4'>
          <div className='min-w-0'>
            <DialogTitle className='leading-snug [overflow-wrap:anywhere]'>
              {supplier.name}
            </DialogTitle>
            <p className='text-muted-foreground mt-1 text-xs [overflow-wrap:anywhere]'>
              {supplier.username}
            </p>
          </div>
          <Button
            variant='ghost'
            size='icon'
            title={t('supplier.done')}
            aria-label={t('supplier.done')}
            disabled={pending}
            onClick={() => leave(props.onClose)}
          >
            <X />
          </Button>
        </header>
        <SupplierOwnership
          supplier={supplier}
          disabled={pending}
          onTaken={(saved) => {
            setSupplier(saved)
            setBinding(null)
            setView('bindings')
          }}
        />
        <Tabs
          value={view}
          onValueChange={(value) => {
            if (
              value !== 'bindings' &&
              value !== 'audits' &&
              value !== 'edit'
            ) {
              return
            }
            if (
              (value === 'audits' && !props.canAudit) ||
              (value === 'edit' && !props.canManage)
            ) {
              return
            }
            leave(() => {
              setBinding(null)
              setView(value)
            })
          }}
          className='min-h-0 flex-1 gap-0'
        >
          <TabsList
            variant='line'
            className='mx-4 my-2 shrink-0'
            activateOnFocus={false}
          >
            <TabsTrigger value='bindings' disabled={pending}>
              {t('supplier.bindings')}
            </TabsTrigger>
            {props.canAudit && (
              <TabsTrigger value='audits' disabled={pending}>
                {t('supplier.audits')}
              </TabsTrigger>
            )}
            {props.canManage && (
              <TabsTrigger value='edit' disabled={pending}>
                {t('supplier.edit')}
              </TabsTrigger>
            )}
          </TabsList>
          <TabsContent value='bindings' className='flex min-h-0 flex-col'>
            {binding && props.canManage ? (
              <BindingForm
                supplierId={supplier.id}
                binding={binding === 'new' ? undefined : binding}
                guardRef={guard}
                onPendingChange={setPending}
                onClose={() => setBinding(null)}
              />
            ) : (
              <>
                <div className='min-h-0 flex-1 overflow-y-auto p-4 pt-2'>
                  <NamingSummary rule={supplier.effective_naming} />
                  <Bindings
                    supplierId={supplier.id}
                    canManage={props.canManage}
                    onEdit={(item) => {
                      if (props.canManage && !pending) setBinding(item)
                    }}
                    onPendingChange={setPending}
                  />
                </div>
                <footer className='flex shrink-0 justify-end border-t p-4'>
                  <Button
                    variant='outline'
                    disabled={pending}
                    onClick={() => leave(props.onClose)}
                  >
                    {t('supplier.done')}
                  </Button>
                </footer>
              </>
            )}
          </TabsContent>
          {props.canAudit && (
            <TabsContent value='audits' className='flex min-h-0 flex-col'>
              <div className='min-h-0 flex-1 overflow-y-auto p-4 pt-2'>
                <Audits supplierId={supplier.id} />
              </div>
              <footer className='flex shrink-0 justify-end border-t p-4'>
                <Button variant='outline' onClick={() => leave(props.onClose)}>
                  {t('supplier.done')}
                </Button>
              </footer>
            </TabsContent>
          )}
          {props.canManage && (
            <TabsContent value='edit' className='flex min-h-0 flex-col'>
              <SupplierForm
                supplier={supplier}
                embedded
                guardRef={guard}
                onPendingChange={setPending}
                onClose={() => setView('bindings')}
                onSaved={(saved) => {
                  setSupplier(saved)
                  setView('bindings')
                }}
              />
            </TabsContent>
          )}
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
