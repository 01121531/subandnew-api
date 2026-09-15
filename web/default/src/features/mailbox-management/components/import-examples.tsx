import { Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'

import { mailboxApi } from '../api'
import { useMailboxQuery } from '../hooks'
import { importExample } from '../lib/import-examples'
import type { AccountType } from '../types'

export function ImportExamples(props: {
  accountType: AccountType
  pending: boolean
}) {
  const { t } = useTranslation()
  const options = useMailboxQuery(['import-options'], (signal) =>
    mailboxApi.importOptions(signal)
  )
  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      toast.success(t('mailbox.admin.copied'))
    } catch {
      toast.error(t('mailbox.admin.copyFailed'))
    }
  }
  const variants =
    props.accountType === 'opening' && options.data?.temporary_cvv_enabled
      ? [false, true]
      : [false]
  const columns =
    props.accountType === 'opening'
      ? 'mailbox.issues.exampleColumnsOpening'
      : 'mailbox.issues.exampleColumnsRefund'
  return (
    <section className='bg-muted/40 my-4 space-y-3 border-y p-3'>
      <h3 className='text-sm font-medium'>{t('mailbox.issues.example')}</h3>
      <p className='text-muted-foreground text-xs'>
        {t('mailbox.issues.exampleHint')}
      </p>
      {variants.map((cvv) => (
        <div key={String(cvv)} className='space-y-2'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <span className='text-xs'>
              {t(cvv ? 'mailbox.issues.exampleCvv' : columns)}
            </span>
            <Button
              variant='outline'
              size='sm'
              disabled={props.pending}
              onClick={() => void copy(importExample(props.accountType, cvv))}
            >
              <Copy />
              {t('mailbox.issues.copyExample')}
            </Button>
          </div>
          <pre className='text-xs [overflow-wrap:anywhere] whitespace-pre-wrap'>
            {importExample(props.accountType, cvv)}
          </pre>
          {cvv && (
            <p className='text-muted-foreground text-xs'>
              {t('mailbox.admin.temporaryCvvNotice')}
            </p>
          )}
        </div>
      ))}
    </section>
  )
}
