import { useNavigate } from '@tanstack/react-router'
import { KeyRound, Pencil, RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import { formatTimestamp, MANAGED_INSTANCE_KINDS } from '../lib'
import type { ManagedInstance } from '../types'
import { StatusBadge } from './status-badge'

type InstancesTableProps = {
  instances: ManagedInstance[]
  canUpdate: boolean
  canCheck: boolean
  canRotate: boolean
  canDelete: boolean
  selectable: boolean
  selectedIds: ReadonlySet<number>
  checkingId: number | null
  onToggleSelection: (instance: ManagedInstance, selected: boolean) => void
  onToggleAll: (selected: boolean) => void
  onEdit: (instance: ManagedInstance) => void
  onCheck: (instance: ManagedInstance) => void
  onRotate: (instance: ManagedInstance) => void
  onDelete: (instance: ManagedInstance) => void
}

function productLabel(instance: ManagedInstance): string {
  return (
    MANAGED_INSTANCE_KINDS.find((kind) => kind.value === instance.kind)
      ?.label || instance.kind
  )
}

export function InstancesTable(props: InstancesTableProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const selectedCount = props.instances.reduce(
    (count, instance) => count + Number(props.selectedIds.has(instance.id)),
    0
  )
  const allSelected =
    props.instances.length > 0 && selectedCount === props.instances.length
  const partiallySelected = selectedCount > 0 && !allSelected
  const openInstance = (id: number) =>
    void navigate({ to: '/instances/$id', params: { id: String(id) } })
  if (props.instances.length === 0) {
    return (
      <Empty className='min-h-64 border'>
        <EmptyHeader>
          <EmptyTitle>{t('No managed instances')}</EmptyTitle>
          <EmptyDescription>
            {t('Add an instance or adjust the current filters.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <>
      <div className='grid gap-2 md:hidden'>
        {props.instances.map((instance) => (
          <div
            key={instance.id}
            role='link'
            tabIndex={0}
            className={cn(
              'hover:bg-muted/50 focus-visible:ring-ring grid min-w-0 cursor-pointer gap-3 rounded-lg border p-3 transition-colors focus-visible:ring-2 focus-visible:outline-none',
              props.selectedIds.has(instance.id) &&
                'border-primary/40 bg-primary/[0.03]'
            )}
            onClick={() => openInstance(instance.id)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') openInstance(instance.id)
            }}
          >
            <div className='flex min-w-0 items-start justify-between gap-3'>
              <div className='flex min-w-0 items-start gap-3'>
                {props.selectable && (
                  <Checkbox
                    className='mt-0.5'
                    checked={props.selectedIds.has(instance.id)}
                    aria-label={t('Select {{name}}', { name: instance.name })}
                    onKeyDown={(event) => event.stopPropagation()}
                    onClick={(event) => event.stopPropagation()}
                    onCheckedChange={(checked) =>
                      props.onToggleSelection(instance, checked)
                    }
                  />
                )}
                <div className='min-w-0'>
                  <div className='truncate font-medium'>{instance.name}</div>
                  {instance.base_url && (
                    <div className='text-muted-foreground truncate text-xs'>
                      {instance.base_url}
                    </div>
                  )}
                </div>
              </div>
              <StatusBadge status={instance.status} />
            </div>
            <div className='grid grid-cols-2 gap-3 text-xs'>
              <div>
                <div className='text-muted-foreground'>{t('Product')}</div>
                <div>
                  {productLabel(instance)} / {instance.environment}
                </div>
              </div>
              <div>
                <div className='text-muted-foreground'>{t('Last checked')}</div>
                <div>{formatTimestamp(instance.last_checked_at)}</div>
              </div>
            </div>
            <div className='flex h-8 justify-end gap-1 border-t pt-2'>
              {props.canCheck && (
                <ActionButton
                  label={t('Check now')}
                  onClick={() => props.onCheck(instance)}
                >
                  <RefreshCw
                    className={
                      props.checkingId === instance.id ? 'animate-spin' : ''
                    }
                  />
                </ActionButton>
              )}
              {props.canUpdate && (
                <ActionButton
                  label={t('Edit')}
                  onClick={() => props.onEdit(instance)}
                >
                  <Pencil />
                </ActionButton>
              )}
              {props.canRotate && (
                <ActionButton
                  label={t('Rotate credential')}
                  onClick={() => props.onRotate(instance)}
                >
                  <KeyRound />
                </ActionButton>
              )}
              {props.canDelete && (
                <ActionButton
                  label={t('Delete')}
                  onClick={() => props.onDelete(instance)}
                  destructive
                >
                  <Trash2 />
                </ActionButton>
              )}
            </div>
          </div>
        ))}
      </div>
      <div className='hidden overflow-auto rounded-lg border md:block'>
        <Table className='min-w-[820px]'>
          <TableHeader>
            <TableRow>
              {props.selectable && (
                <TableHead className='w-10 pr-0'>
                  <Checkbox
                    checked={allSelected}
                    indeterminate={partiallySelected}
                    aria-label={t('Select all instances')}
                    onCheckedChange={props.onToggleAll}
                  />
                </TableHead>
              )}
              <TableHead>{t('Instance')}</TableHead>
              <TableHead>{t('Product')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead>{t('Version')}</TableHead>
              <TableHead>{t('Last checked')}</TableHead>
              <TableHead className='w-40 text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.instances.map((instance) => (
              <TableRow
                key={instance.id}
                tabIndex={0}
                className={cn(
                  'focus-visible:ring-ring cursor-pointer focus-visible:ring-2 focus-visible:outline-none focus-visible:ring-inset',
                  props.selectedIds.has(instance.id) && 'bg-primary/[0.03]'
                )}
                onClick={() => openInstance(instance.id)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') openInstance(instance.id)
                }}
              >
                {props.selectable && (
                  <TableCell className='pr-0'>
                    <Checkbox
                      checked={props.selectedIds.has(instance.id)}
                      aria-label={t('Select {{name}}', {
                        name: instance.name,
                      })}
                      onKeyDown={(event) => event.stopPropagation()}
                      onClick={(event) => event.stopPropagation()}
                      onCheckedChange={(checked) =>
                        props.onToggleSelection(instance, checked)
                      }
                    />
                  </TableCell>
                )}
                <TableCell>
                  <div className='grid max-w-80 gap-0.5'>
                    <span className='truncate font-medium'>
                      {instance.name}
                    </span>
                    {instance.base_url && (
                      <span className='text-muted-foreground truncate text-xs'>
                        {instance.base_url}
                      </span>
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  <div className='grid gap-0.5'>
                    <span>{productLabel(instance)}</span>
                    <span className='text-muted-foreground text-xs'>
                      {instance.environment}
                    </span>
                  </div>
                </TableCell>
                <TableCell>
                  <StatusBadge status={instance.status} />
                </TableCell>
                <TableCell>{instance.version || '-'}</TableCell>
                <TableCell>
                  <div className='grid gap-0.5'>
                    <span>{formatTimestamp(instance.last_checked_at)}</span>
                    {instance.consecutive_failures > 0 && (
                      <span className='text-destructive text-xs'>
                        {t('{{count}} consecutive failures', {
                          count: instance.consecutive_failures,
                        })}
                      </span>
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  <div className='flex h-8 justify-end gap-1'>
                    {props.canCheck && (
                      <ActionButton
                        label={t('Check now')}
                        onClick={() => props.onCheck(instance)}
                      >
                        <RefreshCw
                          className={
                            props.checkingId === instance.id
                              ? 'animate-spin'
                              : ''
                          }
                        />
                      </ActionButton>
                    )}
                    {props.canUpdate && (
                      <ActionButton
                        label={t('Edit')}
                        onClick={() => props.onEdit(instance)}
                      >
                        <Pencil />
                      </ActionButton>
                    )}
                    {props.canRotate && (
                      <ActionButton
                        label={t('Rotate credential')}
                        onClick={() => props.onRotate(instance)}
                      >
                        <KeyRound />
                      </ActionButton>
                    )}
                    {props.canDelete && (
                      <ActionButton
                        label={t('Delete')}
                        onClick={() => props.onDelete(instance)}
                        destructive
                      >
                        <Trash2 />
                      </ActionButton>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </>
  )
}

type ActionButtonProps = {
  label: string
  onClick: () => void
  destructive?: boolean
  children: React.ReactNode
}

function ActionButton(props: ActionButtonProps) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant='ghost'
            size='icon-sm'
            className={
              props.destructive
                ? 'text-destructive hover:text-destructive'
                : undefined
            }
            aria-label={props.label}
            onKeyDown={(event) => event.stopPropagation()}
            onClick={(event) => {
              event.stopPropagation()
              props.onClick()
            }}
          />
        }
      >
        {props.children}
      </TooltipTrigger>
      <TooltipContent>{props.label}</TooltipContent>
    </Tooltip>
  )
}
