import { useQuery } from '@tanstack/react-query'
import { Activity, Pencil, Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { adminApi } from '../admin-api'
import { useAdminMutation } from '../hooks/use-admin-mutation'
import type { Binding } from '../types'
import { Confirm, Empty, QueryState } from './common'

export function Bindings(props: {
  supplierId: number
  canManage: boolean
  onEdit: (binding: Binding | 'new') => void
  onPendingChange: (pending: boolean) => void
}) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['supplier-admin', props.supplierId, 'bindings'],
    queryFn: () => adminApi.bindings(props.supplierId),
  })
  const [deleting, setDeleting] = useState<Binding | null>(null)
  const test = useAdminMutation(
    (id: number) => adminApi.testBinding(props.supplierId, id),
    (data) => {
      if (data.ok) toast.success(t('supplier.testOk'))
      else toast.error(t('supplier.testFailed'))
    }
  )
  const remove = useAdminMutation(
    (id: number) => adminApi.deleteBinding(props.supplierId, id),
    () => {
      setDeleting(null)
      toast.success(t('supplier.completed'))
    }
  )
  const locked = test.isPending || remove.isPending || !!deleting
  const { onPendingChange } = props
  useEffect(() => {
    onPendingChange(locked)
    return () => onPendingChange(false)
  }, [locked, onPendingChange])
  const actions = (binding: Binding) =>
    props.canManage && (
      <div className='flex shrink-0 gap-1'>
        <Button
          variant='ghost'
          size='icon'
          title={t('supplier.test')}
          aria-label={t('supplier.test')}
          disabled={locked}
          onClick={() => {
            if (!locked && props.canManage) test.mutate(binding.id)
          }}
        >
          <Activity
            className={
              test.isPending && test.variables === binding.id
                ? 'animate-pulse'
                : ''
            }
          />
        </Button>
        <Button
          variant='ghost'
          size='icon'
          title={t('supplier.edit')}
          aria-label={t('supplier.edit')}
          disabled={locked}
          onClick={() => props.onEdit(binding)}
        >
          <Pencil />
        </Button>
        <Button
          variant='ghost'
          size='icon'
          className='text-destructive'
          title={t('supplier.delete')}
          aria-label={t('supplier.delete')}
          disabled={locked}
          onClick={() => setDeleting(binding)}
        >
          <Trash2 />
        </Button>
      </div>
    )
  const status = (binding: Binding) => (
    <Badge variant={binding.enabled ? 'secondary' : 'outline'}>
      {binding.enabled ? t('supplier.enabled') : t('supplier.disabled')}
    </Badge>
  )
  return (
    <div
      className='grid min-w-0 gap-4'
      aria-busy={test.isPending || remove.isPending}
    >
      {props.canManage && (
        <Button
          className='justify-self-start'
          disabled={locked}
          onClick={() => props.onEdit('new')}
        >
          <Plus />
          {t('supplier.newBinding')}
        </Button>
      )}
      <QueryState
        pending={query.isPending}
        error={query.error}
        hasData={query.data !== undefined}
        retry={() => void query.refetch()}
      >
        {!query.data?.length ? (
          <Empty />
        ) : (
          <>
            <div className='grid gap-3 md:hidden'>
              {query.data.map((binding) => (
                <article
                  key={binding.id}
                  className='min-w-0 rounded-md border p-3'
                >
                  <div className='flex flex-wrap items-start justify-between gap-2'>
                    <h3 className='min-w-0 flex-1 text-sm font-semibold [overflow-wrap:anywhere] break-words'>
                      {binding.instance_name}
                    </h3>
                    {status(binding)}
                  </div>
                  <dl className='mt-3 grid gap-1 text-sm'>
                    <dt className='text-muted-foreground text-xs'>
                      {t('supplier.username')}
                    </dt>
                    <dd className='[overflow-wrap:anywhere] break-words'>
                      {binding.remote_username || '--'}
                    </dd>
                  </dl>
                  {props.canManage && (
                    <div className='mt-3 flex justify-end border-t pt-2'>
                      {actions(binding)}
                    </div>
                  )}
                </article>
              ))}
            </div>
            <div className='hidden md:block'>
              <Table>
                <TableHeader>
                  <TableRow>
                    {['instance', 'username', 'status'].map((key) => (
                      <TableHead key={key}>{t(`supplier.${key}`)}</TableHead>
                    ))}
                    {props.canManage && (
                      <TableHead className='w-28'>
                        {t('supplier.actions')}
                      </TableHead>
                    )}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {query.data.map((binding) => (
                    <TableRow key={binding.id}>
                      <TableCell className='max-w-64 font-medium [overflow-wrap:anywhere] whitespace-normal'>
                        {binding.instance_name}
                      </TableCell>
                      <TableCell className='max-w-64 [overflow-wrap:anywhere] whitespace-normal'>
                        {binding.remote_username || '--'}
                      </TableCell>
                      <TableCell>{status(binding)}</TableCell>
                      {props.canManage && (
                        <TableCell>{actions(binding)}</TableCell>
                      )}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </>
        )}
      </QueryState>
      <Confirm
        open={!!deleting && props.canManage}
        title={deleting?.instance_name ?? ''}
        description={t('supplier.deleteBindingConfirm')}
        pending={remove.isPending}
        onClose={() => setDeleting(null)}
        onConfirm={() => {
          if (deleting && props.canManage && !remove.isPending) {
            remove.mutate(deleting.id)
          }
        }}
      />
    </div>
  )
}
