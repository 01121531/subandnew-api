import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useBlocker } from '@tanstack/react-router'
import { RefreshCw, Save, X } from 'lucide-react'
import { motion, Reorder } from 'motion/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import { getManagedInstanceOrder, saveManagedInstanceOrder } from '../api'
import { isInstanceOrderConflict } from '../errors'
import {
  isInstanceOrderConsumer,
  moveOrderItem,
  sameInstanceOrder,
} from '../order'
import type { ManagedInstanceOrder } from '../types'
import { InstanceOrderRow } from './instance-order-row'

const ORDER_KEY = ['managed-instance-order'] as const
const EMPTY_ITEMS: ManagedInstanceOrder['items'] = []

export function InstanceOrderSheet(props: { onClose: () => void }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [draft, setDraft] = useState<ManagedInstanceOrder | null>(null)
  const [discard, setDiscard] = useState<'close' | 'reload' | null>(null)
  const [conflict, setConflict] = useState(false)
  const [announcement, setAnnouncement] = useState('')
  const query = useQuery({
    queryKey: ORDER_KEY,
    queryFn: async () => {
      const result = await getManagedInstanceOrder()
      if (!result.success) throw new Error(result.message)
      return result
    },
    staleTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    retry: false,
  })
  const saved = query.data?.data
  const items = draft?.items ?? saved?.items ?? EMPTY_ITEMS
  const dirty =
    !!draft && !!saved && !sameInstanceOrder(draft.items, saved.items)
  const mutation = useMutation({
    mutationFn: async () => {
      if (!draft) throw new Error('No instance order draft')
      const result = await saveManagedInstanceOrder(
        draft.version,
        draft.items.map((item) => item.id)
      )
      if (!result.success) throw new Error(result.message)
      return result
    },
    onSuccess: (result) => {
      client.setQueryData(ORDER_KEY, result)
      setDraft(null)
      setConflict(false)
      void client.invalidateQueries({
        predicate: (entry) => isInstanceOrderConsumer(entry.queryKey),
      })
      toast.success(t('instanceOrder.saved'))
    },
    onError: (error) => setConflict(isInstanceOrderConflict(error)),
    retry: false,
  })
  const pending = mutation.isPending
  const disabled =
    pending || query.isFetching || query.isError || !saved || conflict
  const blocker = useBlocker({
    shouldBlockFn: () => dirty || pending,
    enableBeforeUnload: dirty || pending,
    withResolver: true,
  })
  const reload = () => {
    setDraft(null)
    setConflict(false)
    mutation.reset()
    void query.refetch()
  }
  const requestLeave = (action: 'close' | 'reload') => {
    if (pending) return
    if (dirty) setDiscard(action)
    else if (action === 'close') props.onClose()
    else reload()
  }
  const reorder = (next: ManagedInstanceOrder['items']) => {
    if (disabled || !saved) return
    setDraft({ version: draft?.version ?? saved.version, items: next })
  }
  const move = (from: number, to: number) => {
    const next = moveOrderItem(items, from, to)
    if (next === items || disabled) return
    reorder(next)
    setAnnouncement(
      t('instanceOrder.moved', {
        name: items[from].name,
        position: to + 1,
      })
    )
  }

  return (
    <>
      <Sheet
        open
        onOpenChange={(open) => {
          if (!open) requestLeave('close')
        }}
      >
        <SheetContent className='w-full sm:max-w-xl' showCloseButton={false}>
          {!pending && (
            <Button
              variant='ghost'
              size='icon-sm'
              className='absolute top-3 right-3'
              aria-label={t('instanceOrder.close')}
              title={t('instanceOrder.close')}
              onClick={() => requestLeave('close')}
            >
              <X aria-hidden='true' />
            </Button>
          )}
          <SheetHeader className='pr-14'>
            <SheetTitle>{t('instanceOrder.title')}</SheetTitle>
            <span className='text-muted-foreground text-xs' role='status'>
              {dirty
                ? t('instanceOrder.unsaved')
                : t('instanceOrder.count', { count: items.length })}
            </span>
          </SheetHeader>
          <motion.div
            layoutScroll
            className='min-h-0 flex-1 overflow-y-auto'
            aria-busy={pending || query.isFetching}
          >
            {query.isFetching && (
              <p className='px-4 py-2' role='status'>
                {t('instanceOrder.loading')}
              </p>
            )}
            {query.isError && (
              <p className='text-destructive px-4 py-2' role='alert'>
                {t('instanceOrder.loadError')}
              </p>
            )}
            {mutation.isError && (
              <p className='text-destructive px-4 py-2' role='alert'>
                {conflict
                  ? t('instanceOrder.conflict')
                  : t('instanceOrder.saveError')}
              </p>
            )}
            {saved && items.length === 0 && (
              <p className='text-muted-foreground px-4 py-2'>
                {t('instanceOrder.empty')}
              </p>
            )}
            <Reorder.Group
              axis='y'
              values={items}
              onReorder={reorder}
              aria-label={t('instanceOrder.title')}
            >
              {items.map((item, index) => (
                <InstanceOrderRow
                  key={item.id}
                  item={item}
                  index={index}
                  count={items.length}
                  disabled={disabled}
                  onMove={move}
                />
              ))}
            </Reorder.Group>
            <span className='sr-only' aria-live='polite'>
              {announcement}
            </span>
          </motion.div>
          <SheetFooter className='border-t sm:flex-row sm:justify-end'>
            <Button
              variant='outline'
              disabled={pending || query.isFetching}
              onClick={() => requestLeave('reload')}
            >
              <RefreshCw />
              {t('instanceOrder.reload')}
            </Button>
            <Button
              variant='outline'
              disabled={pending}
              onClick={() => requestLeave('close')}
            >
              {t('instanceOrder.cancel')}
            </Button>
            <Button
              disabled={disabled || !dirty}
              onClick={() => mutation.mutate()}
            >
              <Save />
              {pending ? t('instanceOrder.saving') : t('instanceOrder.save')}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
      <ConfirmDialog
        open={discard !== null || blocker.status === 'blocked'}
        onOpenChange={(open) => {
          if (!open && !pending) {
            setDiscard(null)
            blocker.reset?.()
          }
        }}
        title={t('instanceOrder.discardTitle')}
        desc={t('instanceOrder.discardDescription')}
        confirmText={t('instanceOrder.discard')}
        cancelBtnText={t('instanceOrder.cancel')}
        isLoading={pending}
        handleConfirm={() => {
          if (pending) return
          if (blocker.status === 'blocked') blocker.proceed()
          else if (discard === 'close') props.onClose()
          else reload()
          setDiscard(null)
        }}
      />
    </>
  )
}
