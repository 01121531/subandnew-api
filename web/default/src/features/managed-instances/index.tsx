/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'
import { Plus, RefreshCw, Search, X } from 'lucide-react'
import { useDeferredValue, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  checkManagedInstance,
  deleteManagedInstance,
  getManagedInstances,
} from './api'
import { BatchRefreshSheet } from './components/batch-refresh-sheet'
import { CredentialSheet } from './components/credential-sheet'
import { InstanceFormSheet } from './components/instance-form-sheet'
import { InstancesTable } from './components/instances-table'
import { statusCounts } from './lib'
import type { ManagedInstance, ManagedInstanceFilters } from './types'

const EMPTY_INSTANCES: ManagedInstance[] = []

export function ManagedInstances() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const isRoot = user?.role === ROLE.SUPER_ADMIN
  const [filters, setFilters] = useState<ManagedInstanceFilters>({
    search: '',
    kind: '',
    status: '',
  })
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<ManagedInstance | null>(null)
  const [rotating, setRotating] = useState<ManagedInstance | null>(null)
  const [deleting, setDeleting] = useState<ManagedInstance | null>(null)
  const [checkingId, setCheckingId] = useState<number | null>(null)
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const deferredSearch = useDeferredValue(filters.search)
  const queryFilters = useMemo(
    () => ({ ...filters, search: deferredSearch.trim() }),
    [deferredSearch, filters]
  )

  const instancesQuery = useQuery({
    queryKey: ['managed-instances', queryFilters],
    queryFn: () => getManagedInstances(queryFilters),
    refetchInterval: 30_000,
  })
  const instances = instancesQuery.data?.data.items ?? EMPTY_INSTANCES
  const counts = statusCounts(instances)
  const selectedInstances = useMemo(
    () => instances.filter((instance) => selectedIds.has(instance.id)),
    [instances, selectedIds]
  )

  const canCreate = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
    ADMIN_PERMISSION_ACTIONS.CREATE
  )
  const canUpdate =
    isRoot &&
    hasPermission(
      user,
      ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
      ADMIN_PERMISSION_ACTIONS.UPDATE
    )
  const canDelete = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
    ADMIN_PERMISSION_ACTIONS.DELETE
  )
  const canCheck = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
    ADMIN_PERMISSION_ACTIONS.OPERATE
  )
  const canRotate = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
    ADMIN_PERMISSION_ACTIONS.SECRET_ROTATE
  )
  const canBatchOperate = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_INSTANCE,
    ADMIN_PERMISSION_ACTIONS.BATCH_OPERATE
  )

  useEffect(() => {
    const visibleIds = new Set(instances.map((instance) => instance.id))
    setSelectedIds((current) => {
      const next = new Set([...current].filter((id) => visibleIds.has(id)))
      return next.size === current.size ? current : next
    })
  }, [instances])

  const refresh = () => void instancesQuery.refetch()

  const check = async (instance: ManagedInstance) => {
    setCheckingId(instance.id)
    try {
      const result = await checkManagedInstance(instance.id)
      if (result.success) toast.success(t('Probe task queued'))
      window.setTimeout(refresh, 1200)
    } finally {
      setCheckingId(null)
    }
  }

  const remove = async () => {
    if (!deleting) return
    const result = await deleteManagedInstance(deleting.id)
    if (result.success) {
      toast.success(t('Instance removed'))
      setDeleting(null)
      refresh()
    }
  }

  const toggleSelection = (instance: ManagedInstance, selected: boolean) => {
    setSelectedIds((current) => {
      if (selected && !current.has(instance.id) && current.size >= 50) {
        toast.error(t('You can select at most 50 instances.'))
        return current
      }
      const next = new Set(current)
      if (selected) next.add(instance.id)
      else next.delete(instance.id)
      return next
    })
  }

  const toggleAll = (selected: boolean) => {
    if (!selected) {
      setSelectedIds(new Set())
      return
    }
    const next = new Set(instances.slice(0, 50).map((instance) => instance.id))
    setSelectedIds(next)
    if (instances.length > 50) {
      toast.info(t('The first 50 instances were selected.'))
    }
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('Instance center')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            size='icon-sm'
            aria-label={t('Refresh')}
            onClick={refresh}
          >
            <RefreshCw
              className={instancesQuery.isFetching ? 'animate-spin' : ''}
            />
          </Button>
          {canCreate && (
            <Button
              size='sm'
              onClick={() => {
                setEditing(null)
                setFormOpen(true)
              }}
            >
              <Plus />
              {t('Add instance')}
            </Button>
          )}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='grid gap-4'>
            <div className='grid grid-cols-2 overflow-hidden rounded-lg border md:grid-cols-5 [&>*:last-child]:col-span-2 md:[&>*:last-child]:col-span-1'>
              <SummaryCell label={t('Total')} value={counts.total} />
              <SummaryCell
                label={t('Healthy')}
                value={counts.healthy}
                tone='healthy'
              />
              <SummaryCell
                label={t('Degraded')}
                value={counts.degraded}
                tone='degraded'
              />
              <SummaryCell
                label={t('Offline')}
                value={counts.offline}
                tone='offline'
              />
              <SummaryCell
                label={t('Auth failed')}
                value={counts.auth_failed}
                tone='auth'
              />
            </div>

            <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
              <div className='relative min-w-0 flex-1 sm:max-w-sm'>
                <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2' />
                <Input
                  className='pl-8'
                  value={filters.search}
                  placeholder={t('Search instances')}
                  onChange={(event) =>
                    setFilters((value) => ({
                      ...value,
                      search: event.target.value,
                    }))
                  }
                />
              </div>
              <NativeSelect
                className='w-full sm:w-40'
                value={filters.kind}
                onChange={(event) =>
                  setFilters((value) => ({
                    ...value,
                    kind: event.target.value,
                  }))
                }
              >
                <NativeSelectOption value=''>
                  {t('All products')}
                </NativeSelectOption>
                <NativeSelectOption value='new_api'>New API</NativeSelectOption>
                <NativeSelectOption value='huichuan'>
                  HUICHUAN-AI
                </NativeSelectOption>
                <NativeSelectOption value='sub2api'>Sub2API</NativeSelectOption>
                <NativeSelectOption value='generic'>
                  {t('Generic')}
                </NativeSelectOption>
              </NativeSelect>
              <NativeSelect
                className='w-full sm:w-40'
                value={filters.status}
                onChange={(event) =>
                  setFilters((value) => ({
                    ...value,
                    status: event.target.value,
                  }))
                }
              >
                <NativeSelectOption value=''>
                  {t('All statuses')}
                </NativeSelectOption>
                <NativeSelectOption value='healthy'>
                  {t('Healthy')}
                </NativeSelectOption>
                <NativeSelectOption value='degraded'>
                  {t('Degraded')}
                </NativeSelectOption>
                <NativeSelectOption value='offline'>
                  {t('Offline')}
                </NativeSelectOption>
                <NativeSelectOption value='auth_failed'>
                  {t('Auth failed')}
                </NativeSelectOption>
                <NativeSelectOption value='unknown'>
                  {t('Unknown')}
                </NativeSelectOption>
              </NativeSelect>
            </div>

            {canBatchOperate && (
              <div className='flex min-h-10 flex-col gap-2 border-y py-2 sm:flex-row sm:items-center sm:justify-between'>
                <div className='flex min-w-0 items-center gap-2'>
                  <span className='text-sm font-medium tabular-nums'>
                    {t('{{count}} selected', {
                      count: selectedInstances.length,
                    })}
                  </span>
                  <span className='text-muted-foreground text-xs'>
                    {t('Select 2 to 50 instances for a batch operation.')}
                  </span>
                </div>
                <div className='flex items-center justify-end gap-2'>
                  {selectedInstances.length > 0 && (
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => setSelectedIds(new Set())}
                    >
                      <X />
                      {t('Clear selection')}
                    </Button>
                  )}
                  <BatchRefreshSheet
                    instances={selectedInstances}
                    onFinished={() => setSelectedIds(new Set())}
                  />
                </div>
              </div>
            )}

            <InstancesTable
              instances={instances}
              canUpdate={canUpdate}
              canCheck={canCheck}
              canRotate={canRotate}
              canDelete={canDelete}
              selectable={canBatchOperate}
              selectedIds={selectedIds}
              checkingId={checkingId}
              onToggleSelection={toggleSelection}
              onToggleAll={toggleAll}
              onEdit={(instance) => {
                setEditing(instance)
                setFormOpen(true)
              }}
              onCheck={check}
              onRotate={setRotating}
              onDelete={setDeleting}
            />
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <InstanceFormSheet
        open={formOpen}
        instance={editing}
        onOpenChange={setFormOpen}
        onSaved={refresh}
      />
      <CredentialSheet
        instance={rotating}
        onOpenChange={(open) => !open && setRotating(null)}
        onSaved={refresh}
      />
      <ConfirmDialog
        open={deleting != null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t('Remove instance?')}
        desc={t(
          'This removes only the control-plane relationship. Remote resources are not changed.'
        )}
        destructive
        confirmText={t('Remove')}
        handleConfirm={() => void remove()}
      />
    </>
  )
}

type SummaryCellProps = {
  label: string
  value: number
  tone?: 'healthy' | 'degraded' | 'offline' | 'auth'
}

function SummaryCell(props: SummaryCellProps) {
  const valueClass = {
    healthy: 'text-emerald-700 dark:text-emerald-400',
    degraded: 'text-amber-700 dark:text-amber-400',
    offline: 'text-red-700 dark:text-red-400',
    auth: 'text-fuchsia-700 dark:text-fuchsia-400',
  }[props.tone || 'healthy']
  return (
    <div className='border-border flex min-h-20 flex-col justify-center border-r border-b px-4 py-3 last:border-r-0 md:border-b-0'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span
        className={`text-2xl font-semibold ${props.tone ? valueClass : ''}`}
      >
        {props.value}
      </span>
    </div>
  )
}
