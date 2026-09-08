import {
  Ellipsis,
  FileClock,
  KeyRound,
  Link2,
  Pencil,
  ShieldOff,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

type SupplierAction =
  | 'bindings'
  | 'edit'
  | 'audits'
  | 'reset'
  | 'revoke'
  | 'delete'
export function SupplierActions(props: {
  canManage: boolean
  canAudit: boolean
  onAction: (action: SupplierAction) => void
}) {
  const { t } = useTranslation()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant='ghost'
            size='icon'
            aria-label={t('supplier.actions')}
          />
        }
      >
        <Ellipsis />
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        <DropdownMenuItem onClick={() => props.onAction('bindings')}>
          <Link2 />
          {t('supplier.bindings')}
        </DropdownMenuItem>
        {props.canManage && (
          <DropdownMenuItem onClick={() => props.onAction('edit')}>
            <Pencil />
            {t('supplier.edit')}
          </DropdownMenuItem>
        )}
        {props.canAudit && (
          <DropdownMenuItem onClick={() => props.onAction('audits')}>
            <FileClock />
            {t('supplier.audits')}
          </DropdownMenuItem>
        )}
        {props.canManage && (
          <>
            <DropdownMenuItem onClick={() => props.onAction('reset')}>
              <KeyRound />
              {t('supplier.resetPassword')}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => props.onAction('revoke')}>
              <ShieldOff />
              {t('supplier.revoke')}
            </DropdownMenuItem>
            <DropdownMenuItem
              className='text-destructive'
              onClick={() => props.onAction('delete')}
            >
              <Trash2 />
              {t('supplier.delete')}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
