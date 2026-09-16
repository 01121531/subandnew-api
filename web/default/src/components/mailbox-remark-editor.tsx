import { History, Pencil, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'

export type RemarkRevision = {
  id: number
  old_remark: string
  new_remark: string
  admin_id: number
  operator_id: number
  created_at: number
  version: number
}
export type RemarkHistory = { items: RemarkRevision[]; has_more: boolean }
export function MailboxRemarkEditor<
  T extends { id: number; version: number; remark?: string },
>(props: {
  submission: T
  canEdit: boolean
  save: (remark: string, version: number) => Promise<T>
  history: (page: number, signal: AbortSignal) => Promise<RemarkHistory>
  onSaved: (value: T) => void
  errorText: (error: unknown) => string
}) {
  const { t, i18n } = useTranslation()
  const [mode, setMode] = useState<'edit' | 'history'>()
  const [draft, setDraft] = useState('')
  const [version, setVersion] = useState(0)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const [page, setPage] = useState(1)
  const [records, setRecords] = useState<RemarkHistory>()
  const [loading, setLoading] = useState(false)
  const [retry, setRetry] = useState(0)
  const lock = useRef(false),
    alive = useRef(true)
  const history = useRef(props.history)
  history.current = props.history
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  useEffect(() => {
    if (mode !== 'history') return
    const controller = new AbortController()
    setLoading(true)
    setRecords(undefined)
    setError(undefined)
    void history
      .current(page, controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setRecords(value)
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(error)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [mode, page, retry])
  function close() {
    if (lock.current) return
    if (
      mode === 'edit' &&
      draft !== (props.submission.remark ?? '') &&
      !window.confirm(t('mailbox.remarkEdit.discard'))
    ) {
      return
    }
    setMode(undefined)
    setDraft('')
    setError(undefined)
    setRecords(undefined)
  }
  async function save() {
    if (lock.current || !props.canEdit || [...draft].length > 2000) {
      return
    }
    lock.current = true
    setPending(true)
    setError(undefined)
    try {
      const result = await props.save(draft, version)
      if (alive.current) {
        props.onSaved(result)
        setMode(undefined)
        setDraft('')
      }
    } catch (error) {
      if (alive.current) setError(error)
    } finally {
      lock.current = false
      if (alive.current) setPending(false)
    }
  }
  return (
    <>
      <div className='flex flex-wrap gap-2'>
        {props.canEdit && (
          <Button
            variant='outline'
            onClick={() => {
              setDraft(props.submission.remark ?? '')
              setVersion(props.submission.version)
              setError(undefined)
              setMode('edit')
            }}
          >
            <Pencil />
            {t('mailbox.remarkEdit.title')}
          </Button>
        )}
        <Button
          variant='ghost'
          onClick={() => {
            setPage(1)
            setError(undefined)
            setMode('history')
          }}
        >
          <History />
          {t('mailbox.remarkEdit.history')}
        </Button>
      </div>
      {mode && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open) close()
          }}
        >
          <DialogContent
            showCloseButton={false}
            className='flex max-h-[90dvh] min-w-0 flex-col overflow-hidden rounded-lg'
          >
            <div className='flex items-start justify-between gap-2'>
              <DialogTitle>
                {t(
                  mode === 'edit'
                    ? 'mailbox.remarkEdit.title'
                    : 'mailbox.remarkEdit.history'
                )}
              </DialogTitle>
              <Button
                size='icon'
                variant='ghost'
                disabled={pending}
                aria-label={t('mailbox.admin.close')}
                onClick={close}
              >
                <X />
              </Button>
            </div>
            <DialogDescription>
              {t('mailbox.remarkEdit.hint')}
            </DialogDescription>
            <div className='min-h-0 space-y-3 overflow-y-auto'>
              {mode === 'edit' ? (
                <form
                  id='mailbox-edit-remark'
                  onSubmit={(event) => {
                    event.preventDefault()
                    void save()
                  }}
                >
                  <Textarea
                    aria-label={t('mailbox.remarkEdit.title')}
                    rows={6}
                    value={draft}
                    disabled={pending}
                    onChange={(event) => setDraft(event.target.value)}
                  />
                  <p className='text-muted-foreground text-xs'>
                    {[...draft].length}/2000
                  </p>
                </form>
              ) : (
                <>
                  {loading && <p role='status'>{t('mailbox.admin.loading')}</p>}
                  {records?.items.length === 0 && (
                    <p>{t('mailbox.admin.empty')}</p>
                  )}
                  {records?.items.map((record) => (
                    <section
                      key={record.id}
                      className='space-y-2 border-b pb-3 text-sm'
                    >
                      <p>
                        {record.admin_id
                          ? t('mailbox.admin.adminActor', {
                              id: record.admin_id,
                            })
                          : t('mailbox.admin.operatorActor', {
                              id: record.operator_id,
                            })}{' '}
                        ·{' '}
                        {new Date(record.created_at * 1000).toLocaleString(
                          i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US',
                          { timeZone: 'Asia/Shanghai' }
                        )}
                      </p>
                      <p className='break-words whitespace-pre-wrap'>
                        {t('mailbox.remarkEdit.before')}:{' '}
                        {record.old_remark || '--'}
                      </p>
                      <p className='break-words whitespace-pre-wrap'>
                        {t('mailbox.remarkEdit.after')}:{' '}
                        {record.new_remark || '--'}
                      </p>
                    </section>
                  ))}
                  <div className='flex gap-2'>
                    <Button
                      variant='outline'
                      disabled={loading || page <= 1}
                      onClick={() => setPage(page - 1)}
                    >
                      {t('mailbox.admin.previous')}
                    </Button>
                    <Button
                      variant='outline'
                      disabled={loading || !records?.has_more}
                      onClick={() => setPage(page + 1)}
                    >
                      {t('mailbox.admin.next')}
                    </Button>
                  </div>
                </>
              )}
              {error != null && (
                <p role='alert' className='text-destructive'>
                  {props.errorText(error)}
                </p>
              )}
              {error != null && mode === 'history' && (
                <Button onClick={() => setRetry(retry + 1)}>
                  {t('mailbox.admin.retry')}
                </Button>
              )}
            </div>
            <footer className='flex shrink-0 justify-end gap-2'>
              <Button variant='outline' onClick={close} disabled={pending}>
                {t('mailbox.admin.cancel')}
              </Button>
              {mode === 'edit' && (
                <Button
                  type='submit'
                  form='mailbox-edit-remark'
                  disabled={
                    pending ||
                    !props.canEdit ||
                    [...draft].length > 2000 ||
                    draft === (props.submission.remark ?? '')
                  }
                >
                  {t('mailbox.admin.confirm')}
                </Button>
              )}
            </footer>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}
