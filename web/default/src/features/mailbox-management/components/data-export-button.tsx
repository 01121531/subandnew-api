import { Download, Loader2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'

import { mailboxApi, type IssueExportInput } from '../api'
import { errorKey } from '../lib/errors'
import { Modal } from './common'

export function DataExportButton(props: {
  filters?: Omit<IssueExportInput, 'scope'>
  accountFilters?: Partial<import('../types').ListQuery>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [scope, setScope] = useState<'filtered' | 'all'>('filtered')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  const title = t(`mailbox.dataExport.${props.filters ? 'issues' : 'all'}`)
  const allLabel = props.filters ? 'allIssues' : 'all'

  async function download() {
    if (request.current) return
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setError('')
    try {
      const blob = props.filters
        ? await mailboxApi.exportIssues(
            { ...props.filters, scope },
            controller.signal
          )
        : await mailboxApi.exportAll(
            controller.signal,
            props.accountFilters ? { ...props.accountFilters, scope } : {}
          )
      if (controller.signal.aborted) return
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = props.filters ? 'mailbox-issues.xlsx' : 'mailbox-all.xlsx'
      document.body.append(link)
      link.click()
      link.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      toast.success(t('mailbox.completedExport.success'))
      setOpen(false)
    } catch (cause) {
      if (!controller.signal.aborted) setError(t(errorKey(cause)))
    } finally {
      request.current = null
      if (!controller.signal.aborted) setPending(false)
    }
  }

  return (
    <>
      <Button
        variant='outline'
        disabled={pending}
        onClick={() => {
          setOpen(true)
          setError('')
        }}
      >
        <Download />
        {title}
      </Button>
      {open && (
        <Modal
          title={title}
          pending={pending}
          onClose={() => setOpen(false)}
          footer={
            <Button
              disabled={pending}
              aria-busy={pending}
              onClick={() => void download()}
            >
              {pending ? <Loader2 className='animate-spin' /> : <Download />}
              {pending
                ? t('mailbox.completedExport.pending')
                : t('mailbox.dataExport.confirm')}
            </Button>
          }
        >
          <div className='space-y-4 break-words'>
            <p>
              {t(
                props.filters
                  ? 'mailbox.dataExport.issuesHint'
                  : 'mailbox.dataExport.allHint'
              )}
            </p>
            {(props.filters || props.accountFilters) && (
              <RadioGroup
                value={scope}
                disabled={pending}
                onValueChange={(value) =>
                  setScope(value === 'all' ? 'all' : 'filtered')
                }
                aria-label={t('mailbox.dataExport.scope')}
              >
                {(['filtered', 'all'] as const).map((value) => (
                  <Label key={value} className='flex items-center gap-2 py-2'>
                    <RadioGroupItem value={value} />
                    {t(
                      `mailbox.dataExport.${value === 'all' ? allLabel : 'filtered'}`
                    )}
                  </Label>
                ))}
              </RadioGroup>
            )}
            <p className='rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-100'>
              {t('mailbox.dataExport.sensitive')}
            </p>
            {error && (
              <p role='alert' className='text-destructive text-sm'>
                {error}
              </p>
            )}
          </div>
        </Modal>
      )}
    </>
  )
}
