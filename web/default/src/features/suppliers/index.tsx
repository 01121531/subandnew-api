import { useQuery } from '@tanstack/react-query'
import { Plus, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import { adminApi } from './admin-api'
import { Audits } from './components/audits'
import { Bindings } from './components/bindings'
import {
  Confirm,
  Empty,
  Pagination,
  QueryState,
  Time,
} from './components/common'
import { ResetPassword } from './components/reset-password'
import { SupplierActions } from './components/supplier-actions'
import { SupplierForm } from './components/supplier-form'
import { useAdminMutation } from './hooks/use-admin-mutation'
import type { Supplier } from './types'

export function Suppliers() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canManage = hasPermission(user, 'supplier', 'manage')
  const canAudit = hasPermission(user, 'supplier', 'audit')
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<Supplier | 'new' | null>(null)
  const [detail, setDetail] = useState<{
    supplier: Supplier
    view: 'bindings' | 'audits'
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
        if (action === 'edit') setEditing(supplier)
        if (action === 'reset') setReset(supplier)
        if (action === 'revoke') setRevoking(supplier)
        if (action === 'delete') setDeleting(supplier)
      }}
    />
  )
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('supplier.title')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
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
          <Button onClick={() => setEditing('new')}>
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
                            className='h-auto px-0 text-left font-semibold break-words whitespace-normal'
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
                          <Badge variant='outline'>{t('supplier.usage')}</Badge>
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
                          'username',
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
                          <TableCell>
                            <Button
                              variant='link'
                              className='max-w-56 truncate px-0'
                              onClick={() =>
                                setDetail({ supplier, view: 'bindings' })
                              }
                            >
                              {supplier.name}
                            </Button>
                          </TableCell>
                          <TableCell>{supplier.username}</TableCell>
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
                            <div className='flex max-w-sm flex-wrap gap-1'>
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
            <SupplierForm
              supplier={editing === 'new' ? undefined : editing}
              onClose={() => setEditing(null)}
            />
          )}
          {reset && canManage && (
            <ResetPassword
              supplierId={reset.id}
              onClose={() => setReset(null)}
            />
          )}
          <Dialog
            open={!!detail}
            onOpenChange={(open) => {
              if (!open) setDetail(null)
            }}
          >
            <DialogContent className='max-h-[90dvh] overflow-y-auto sm:max-w-4xl'>
              <DialogTitle>
                {detail?.supplier.name} /{' '}
                {t(`supplier.${detail?.view ?? 'bindings'}`)}
              </DialogTitle>
              {detail?.view === 'bindings' && (
                <Bindings
                  key={detail.supplier.id}
                  supplierId={detail.supplier.id}
                  canManage={canManage}
                />
              )}
              {detail?.view === 'audits' && canAudit && (
                <Audits
                  key={detail.supplier.id}
                  supplierId={detail.supplier.id}
                />
              )}
            </DialogContent>
          </Dialog>
          <Confirm
            open={!!revoking}
            title={revoking?.name ?? ''}
            description={t('supplier.revokeConfirm')}
            pending={revoke.isPending}
            onClose={() => setRevoking(null)}
            onConfirm={() => {
              if (revoking && canManage) revoke.mutate(revoking.id)
            }}
          />
          <Confirm
            open={!!deleting}
            title={deleting?.name ?? ''}
            description={t('supplier.deleteSupplierConfirm')}
            pending={remove.isPending}
            onClose={() => setDeleting(null)}
            onConfirm={() => {
              if (deleting && canManage) remove.mutate(deleting.id)
            }}
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
