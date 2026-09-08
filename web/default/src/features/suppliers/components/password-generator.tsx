import { Copy, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { copyToClipboard } from '@/lib/copy-to-clipboard'

import { generateSupplierPassword } from '../lib/password'

export function PasswordGenerator(props: {
  value: string
  disabled: boolean
  onGenerate: (value: string) => void
}) {
  const { t } = useTranslation()
  const [generated, setGenerated] = useState('')
  const generate = () => {
    try {
      const value = generateSupplierPassword()
      setGenerated(value)
      props.onGenerate(value)
    } catch {
      toast.error(t('supplier.requestFailed'))
    }
  }
  const copy = async () => {
    const copied = await copyToClipboard(generated)
    if (copied) toast.success(t('supplier.copied'))
    else toast.error(t('supplier.copyFailed'))
  }
  return (
    <div className='grid gap-2'>
      <Button
        type='button'
        variant='outline'
        disabled={props.disabled}
        className='justify-self-start'
        onClick={generate}
      >
        <RefreshCw />
        {t('supplier.generatePassword')}
      </Button>
      {!!generated && generated === props.value && (
        <div className='flex items-center gap-2 rounded-md border p-2'>
          <code
            className='min-w-0 flex-1 text-sm break-all'
            aria-label={t('supplier.generatedPassword')}
          >
            {generated}
          </code>
          <Button
            type='button'
            size='icon'
            variant='ghost'
            aria-label={t('supplier.copyPassword')}
            title={t('supplier.copyPassword')}
            onClick={() => void copy()}
          >
            <Copy />
          </Button>
        </div>
      )}
    </div>
  )
}
