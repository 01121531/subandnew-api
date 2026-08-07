import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import { rotateManagedInstanceCredential } from '../api'
import type { ManagedInstance } from '../types'

type CredentialSheetProps = {
  instance: ManagedInstance | null
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}

export function CredentialSheet(props: CredentialSheetProps) {
  const { t } = useTranslation()
  const [authType, setAuthType] = useState('bearer_pat')
  const [secret, setSecret] = useState('')
  const [userId, setUserId] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!props.instance) return
    setAuthType(props.instance.credential?.auth_type || 'bearer_pat')
    setSecret('')
    setUserId('')
  }, [props.instance])

  const submit = async () => {
    if (!props.instance || !secret.trim()) return
    setSubmitting(true)
    try {
      const response = await rotateManagedInstanceCredential(
        props.instance.id,
        {
          auth_type: authType,
          secret,
          user_id: userId.trim(),
          expires_at: 0,
        }
      )
      if (!response.success) return
      setSecret('')
      toast.success(t('Credential rotated'))
      props.onOpenChange(false)
      props.onSaved()
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Sheet open={props.instance != null} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[440px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Rotate credential')}</SheetTitle>
          <SheetDescription>
            {props.instance?.name}. {t('The existing secret cannot be viewed.')}
          </SheetDescription>
        </SheetHeader>
        <div className='flex flex-1 flex-col gap-4 overflow-auto px-4 py-4'>
          <div className='grid gap-2'>
            <Label htmlFor='credential-auth-type'>{t('Authentication')}</Label>
            <NativeSelect
              id='credential-auth-type'
              className='w-full'
              value={authType}
              onChange={(event) => setAuthType(event.target.value)}
            >
              <NativeSelectOption value='bearer_pat'>
                Bearer PAT
              </NativeSelectOption>
              <NativeSelectOption value='admin_token'>
                {t('Admin token')}
              </NativeSelectOption>
              <NativeSelectOption value='legacy_access_token'>
                {t('Legacy access token')}
              </NativeSelectOption>
            </NativeSelect>
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='credential-user-id'>{t('Legacy user ID')}</Label>
            <Input
              id='credential-user-id'
              value={userId}
              onChange={(event) => setUserId(event.target.value)}
              autoComplete='off'
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='credential-secret'>{t('New secret')}</Label>
            <Input
              id='credential-secret'
              type='password'
              value={secret}
              onChange={(event) => setSecret(event.target.value)}
              autoComplete='new-password'
            />
          </div>
        </div>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose
            render={<Button variant='outline' />}
            disabled={submitting}
          >
            {t('Cancel')}
          </SheetClose>
          <Button disabled={submitting || !secret.trim()} onClick={submit}>
            {submitting ? t('Saving...') : t('Rotate')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
