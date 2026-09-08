import { ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { NativeSelectOption } from '@/components/ui/native-select'

import type { portalViews } from '../lib/portal-views'
import type { Binding } from '../types'
import { SelectField } from './common'

export function PortalNavigation(props: {
  mobile: boolean
  name: string
  views: ReturnType<typeof portalViews>
  current: string
  bindings: Binding[]
  bindingId: number
  disabled: boolean
  onBinding: (id: number) => void
  onView: (id: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='flex h-full min-w-0 flex-col'>
      <div className='flex min-h-20 items-center gap-2 border-b px-4 py-4'>
        <ShieldCheck className='size-7 shrink-0 text-emerald-600 dark:text-emerald-400' />
        <div className='min-w-0'>
          <p className='text-sm font-semibold'>Claude Gateway</p>
          <p
            className='text-muted-foreground truncate text-xs'
            title={props.name}
          >
            {props.name}
          </p>
        </div>
      </div>
      <div className='p-4'>
        <SelectField
          id={props.mobile ? 'portal-binding-mobile' : 'portal-binding'}
          label={t('supplier.binding')}
          value={props.bindingId}
          disabled={props.disabled}
          onChange={(id) => props.onBinding(Number(id))}
        >
          <NativeSelectOption value={0}>
            {t('supplier.chooseBinding')}
          </NativeSelectOption>
          {props.bindings.map((item) => (
            <NativeSelectOption key={item.id} value={item.id}>
              {item.instance_name}
            </NativeSelectOption>
          ))}
        </SelectField>
      </div>
      <nav aria-label={t('supplier.navigation')} className='grid gap-1 px-3'>
        {props.views.map((item) => (
          <Button
            key={item.id}
            variant='ghost'
            disabled={props.disabled}
            className='aria-[current=page]:bg-primary/10 aria-[current=page]:text-primary h-10 justify-start gap-3 px-3 text-sm'
            aria-current={props.current === item.id ? 'page' : undefined}
            onClick={() => props.onView(item.id)}
          >
            <item.icon className='size-4' />
            {t(`supplier.${item.id}`)}
          </Button>
        ))}
      </nav>
    </div>
  )
}
