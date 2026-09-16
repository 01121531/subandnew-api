import { Download, Loader2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'

import { mailboxApi } from '../api'
import { errorKey } from '../lib/errors'

export function ExportCompletedButton() {
  const { t } = useTranslation()
  const [pending, setPending] = useState(false)
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])

  async function download() {
    if (request.current) return
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    try {
      const data = await mailboxApi.exportCompleted(controller.signal)
      if (controller.signal.aborted) return
      const url = URL.createObjectURL(data)
      const link = document.createElement('a')
      link.href = url
      link.download = 'opening-completed.xlsx'
      document.body.append(link)
      link.click()
      link.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      toast.success(t('mailbox.completedExport.success'))
    } catch (error) {
      if (!controller.signal.aborted) toast.error(t(errorKey(error)))
    } finally {
      request.current = null
      if (!controller.signal.aborted) setPending(false)
    }
  }

  return (
    <Button
      variant='outline'
      disabled={pending}
      aria-busy={pending}
      onClick={() => void download()}
    >
      {pending ? <Loader2 className='animate-spin' /> : <Download />}
      {t(`mailbox.completedExport.${pending ? 'pending' : 'button'}`)}
    </Button>
  )
}
