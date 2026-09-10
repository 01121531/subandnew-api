import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { formatCost } from '../lib/usage'

export function CostValue(props: { value: number | null | undefined }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const triggerId = useId()
  if (props.value == null || !Number.isFinite(props.value)) return '--'
  const fullValue = `USD ${props.value.toLocaleString('en-US', {
    minimumFractionDigits: 8,
    maximumFractionDigits: 8,
  })}`
  return (
    <Tooltip open={open} onOpenChange={setOpen} triggerId={triggerId}>
      <TooltipTrigger
        id={triggerId}
        type='button'
        closeOnClick={false}
        delay={0}
        className='focus-visible:ring-ring max-w-full cursor-help rounded-sm [overflow-wrap:anywhere] [white-space:inherit] text-inherit tabular-nums underline decoration-current/40 decoration-dotted underline-offset-4 outline-none focus-visible:ring-2'
        aria-label={`${t('supplier.ui_full_cost')}: ${fullValue}`}
        aria-describedby={open ? `${triggerId}-tooltip` : undefined}
        onClick={(event) => {
          event.preventDefault()
          event.stopPropagation()
          setOpen(true)
        }}
      >
        {formatCost(props.value)}
      </TooltipTrigger>
      <TooltipContent
        id={`${triggerId}-tooltip`}
        role='tooltip'
        className='supplier-portal max-w-[min(20rem,calc(100vw-2rem))] [overflow-wrap:anywhere] tabular-nums'
      >
        {t('supplier.ui_full_cost')}: {fullValue}
      </TooltipContent>
    </Tooltip>
  )
}
