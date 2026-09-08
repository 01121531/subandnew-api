import { useQuery } from '@tanstack/react-query'
import { Activity, Pencil, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
import { BindingForm } from './binding-form'
import { Confirm, Empty, QueryState } from './common'

export function Bindings(props: { supplierId: number; canManage: boolean }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['supplier-admin', props.supplierId, 'bindings'],
    queryFn: () => adminApi.bindings(props.supplierId),
  })
  const [editing, setEditing] = useState<Binding | 'new' | null>(null)
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
  return (
    <div className='grid min-w-0 gap-4'>
      {props.canManage && (
        <Button
          className='justify-self-start'
          onClick={() => setEditing('new')}
        >
          <Plus />
          {t('supplier.newBinding')}
        </Button>
      )}
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
      >
        {!query.data?.length ? (
          <Empty />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                {['instance', 'username', 'status', 'actions'].map((key) => (
                  <TableHead key={key}>{t(`supplier.${key}`)}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.data.map((binding) => (
                <TableRow key={binding.id}>
                  <TableCell className='max-w-56 truncate'>
                    {binding.instance_name}
                  </TableCell>
                  <TableCell>{binding.remote_username || '--'}</TableCell>
                  <TableCell>
                    {binding.enabled
                      ? t('supplier.enabled')
                      : t('supplier.disabled')}
                  </TableCell>
                  <TableCell>
                    {props.canManage && (
                      <div className='flex gap-1'>
                        <Button
                          variant='ghost'
                          size='icon'
                          title={t('supplier.test')}
                          aria-label={t('supplier.test')}
                          disabled={test.isPending}
                          onClick={() => test.mutate(binding.id)}
                        >
                          <Activity />
                        </Button>
                        <Button
                          variant='ghost'
                          size='icon'
                          title={t('supplier.edit')}
                          aria-label={t('supplier.edit')}
                          onClick={() => setEditing(binding)}
                        >
                          <Pencil />
                        </Button>
                        <Button
                          variant='ghost'
                          size='icon'
                          className='text-destructive'
                          title={t('supplier.delete')}
                          aria-label={t('supplier.delete')}
                          onClick={() => setDeleting(binding)}
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </QueryState>
      {editing && (
        <BindingForm
          supplierId={props.supplierId}
          binding={editing === 'new' ? undefined : editing}
          onClose={() => setEditing(null)}
        />
      )}
      <Confirm
        open={!!deleting}
        title={deleting?.instance_name ?? ''}
        description={t('supplier.deleteBindingConfirm')}
        pending={remove.isPending}
        onClose={() => setDeleting(null)}
        onConfirm={() => {
          if (deleting) remove.mutate(deleting.id)
        }}
      />
    </div>
  )
}
