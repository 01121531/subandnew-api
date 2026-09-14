import { ArrowDown, ArrowUp, GripVertical } from 'lucide-react'
import { Reorder, useDragControls } from 'motion/react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import type { ManagedInstanceOrder } from '../types'

export function InstanceOrderRow(props: {
  item: ManagedInstanceOrder['items'][number]
  index: number
  count: number
  disabled: boolean
  onMove: (from: number, to: number) => void
}) {
  const { t } = useTranslation()
  const controls = useDragControls()
  return (
    <Reorder.Item
      value={props.item}
      dragListener={false}
      dragControls={controls}
      className='bg-background relative flex min-h-16 items-center gap-2 border-b px-3 py-2'
    >
      <Button
        variant='ghost'
        size='icon'
        className='size-11 shrink-0 cursor-grab touch-none active:cursor-grabbing'
        disabled={props.disabled}
        aria-label={t('instanceOrder.reorder', { name: props.item.name })}
        title={t('instanceOrder.reorder', { name: props.item.name })}
        onPointerDown={(event) => {
          if (!props.disabled) controls.start(event)
        }}
        onKeyDown={(event) => {
          if (props.disabled || !['ArrowUp', 'ArrowDown'].includes(event.key)) {
            return
          }
          event.preventDefault()
          props.onMove(
            props.index,
            props.index + (event.key === 'ArrowUp' ? -1 : 1)
          )
        }}
      >
        <GripVertical aria-hidden='true' />
      </Button>
      <span className='text-muted-foreground w-8 shrink-0 text-center text-xs tabular-nums'>
        {props.index + 1}
      </span>
      <span className='min-w-0 flex-1 text-sm font-medium break-all'>
        {props.item.name}
      </span>
      <div className='flex shrink-0'>
        <Button
          variant='ghost'
          size='icon'
          className='size-11'
          disabled={props.disabled || props.index === 0}
          aria-label={t('instanceOrder.moveUp', { name: props.item.name })}
          title={t('instanceOrder.up')}
          onClick={() => props.onMove(props.index, props.index - 1)}
        >
          <ArrowUp aria-hidden='true' />
        </Button>
        <Button
          variant='ghost'
          size='icon'
          className='size-11'
          disabled={props.disabled || props.index === props.count - 1}
          aria-label={t('instanceOrder.moveDown', { name: props.item.name })}
          title={t('instanceOrder.down')}
          onClick={() => props.onMove(props.index, props.index + 1)}
        >
          <ArrowDown aria-hidden='true' />
        </Button>
      </div>
    </Reorder.Item>
  )
}
