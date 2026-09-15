import { useMutation } from '@tanstack/react-query'
import { RefreshCw, Send, Trash2, Upload } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { mailboxApi } from '../api'
import {
  assertCurrentAssignment,
  canSubmit,
  validateImages,
} from '../lib/guards'
import { mailboxClient } from '../session'
import type { Account, Attachment } from '../types'
import { ErrorMessage } from './common'

type Draft = {
  file: File
  url: string
  attachment?: Attachment
  error?: unknown
}
export type LeaveState = { dirty: boolean; pending: boolean }

export function DraftSubmission(props: {
  account: Account
  csrf: string
  leave: React.RefObject<LeaveState>
  onSubmitted: () => void
}) {
  const { t } = useTranslation()
  const [drafts, setDrafts] = useState<Draft[]>([])
  const [error, setError] = useState<unknown>(null)
  const input = useRef<HTMLInputElement>(null)
  const urls = useRef(new Set<string>())
  const controller = useRef(new AbortController())
  const lock = useRef(false)
  const leave = props.leave
  useEffect(() => {
    const active = new AbortController()
    controller.current = active
    const currentUrls = urls.current
    return () => {
      active.abort()
      for (const url of currentUrls) URL.revokeObjectURL(url)
      currentUrls.clear()
      leave.current = { dirty: false, pending: false }
    }
  }, [leave])
  const upload = useMutation({
    mutationFn: async (items: Draft[]) => {
      for (const draft of items) {
        if (controller.current.signal.aborted) return
        try {
          assertCurrentAssignment(
            props.account,
            await mailboxApi.account(
              props.account.id,
              controller.current.signal,
              props.account.account_type ?? 'refund'
            ),
            'submit'
          )
          const attachment = await mailboxApi.upload(
            props.csrf,
            props.account.assignment_id,
            draft.file,
            controller.current.signal,
            props.account.account_type ?? 'refund'
          )
          assertCurrentAssignment(
            props.account,
            await mailboxApi.account(
              props.account.id,
              controller.current.signal,
              props.account.account_type ?? 'refund'
            ),
            'submit'
          )
          if (!controller.current.signal.aborted) {
            setDrafts((current) =>
              current.map((item) =>
                item.url === draft.url
                  ? { ...item, attachment, error: undefined }
                  : item
              )
            )
          }
        } catch (failure) {
          if (controller.current.signal.aborted) return
          setDrafts((current) =>
            current.map((item) =>
              item.url === draft.url ? { ...item, error: failure } : item
            )
          )
        }
      }
    },
    onSettled: () => {
      lock.current = false
    },
  })
  const submit = useMutation({
    mutationFn: async () => {
      assertCurrentAssignment(
        props.account,
        await mailboxApi.account(
          props.account.id,
          controller.current.signal,
          props.account.account_type ?? 'refund'
        ),
        'submit'
      )
      const ids = drafts.map((item) => item.attachment?.id ?? '')
      if (
        ids.length < 1 ||
        ids.length > 5 ||
        ids.some((id) => !id) ||
        new Set(ids).size !== ids.length
      ) {
        throw new Error('Invalid draft selection')
      }
      await mailboxApi.submit(
        props.csrf,
        props.account.assignment_id,
        props.account.assignment_version,
        ids,
        controller.current.signal,
        props.account.account_type ?? 'refund'
      )
    },
    onSuccess: () => {
      if (controller.current.signal.aborted) return
      for (const url of urls.current) URL.revokeObjectURL(url)
      urls.current.clear()
      setDrafts([])
      props.leave.current = { dirty: false, pending: false }
      void mailboxClient.invalidateQueries({ queryKey: ['mailbox'] })
      props.onSubmitted()
    },
    onError: (failure) => {
      if (!controller.current.signal.aborted) setError(failure)
      // Reconcile an ambiguous response before allowing another attempt.
      void mailboxClient.invalidateQueries({
        queryKey: [
          'mailbox',
          'account',
          props.account.id,
          props.account.account_type ?? 'refund',
        ],
      })
    },
    onSettled: () => {
      lock.current = false
    },
  })
  const pending = upload.isPending || submit.isPending
  props.leave.current = { dirty: drafts.length > 0, pending }
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (drafts.length || pending) event.preventDefault()
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [drafts.length, pending])
  function select(files: File[]) {
    if (lock.current) return
    try {
      validateImages(files, drafts.length)
      const items = files.map((file) => {
        const url = URL.createObjectURL(file)
        urls.current.add(url)
        return { file, url }
      })
      setError(null)
      setDrafts((current) => [...current, ...items])
      lock.current = true
      upload.mutate(items)
    } catch (failure) {
      setError(failure)
    }
  }
  if (!canSubmit(props.account)) return null
  return (
    <section className='grid gap-4 py-5'>
      <p
        id='mailbox-redaction-warning'
        role='note'
        className='border-l-2 border-amber-500 pl-3 text-sm text-amber-800 dark:text-amber-300'
      >
        {t('mailboxPortal.redactCardWarning')}
      </p>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h3 className='text-sm font-semibold'>
          {t('mailboxPortal.screenshots')}{' '}
          <span className='text-muted-foreground font-normal'>
            {drafts.length}/5
          </span>
        </h3>
        <input
          ref={input}
          type='file'
          className='hidden'
          accept='image/png,image/jpeg,image/webp'
          multiple
          disabled={pending || drafts.length >= 5}
          aria-label={t('mailboxPortal.upload')}
          aria-describedby='mailbox-redaction-warning'
          onChange={(event) => {
            select([...(event.target.files ?? [])])
            event.target.value = ''
          }}
        />
        <Button
          variant='outline'
          disabled={pending || drafts.length >= 5}
          aria-describedby='mailbox-redaction-warning'
          onClick={() => input.current?.click()}
        >
          <Upload />
          {t('mailboxPortal.upload')}
        </Button>
      </div>
      <div className='grid grid-cols-1 gap-3 min-[400px]:grid-cols-2'>
        {drafts.map((draft) => (
          <div
            key={draft.url}
            className='min-w-0 overflow-hidden rounded border'
          >
            <img
              src={draft.url}
              alt={t('mailboxPortal.screenshot')}
              className='bg-muted/40 aspect-[4/3] w-full object-contain'
            />
            <div className='flex items-center justify-between gap-2 border-t px-2 py-1'>
              <span className='text-muted-foreground text-xs'>
                {t(
                  draft.attachment
                    ? 'mailboxPortal.uploaded'
                    : 'mailboxPortal.draft'
                )}
              </span>
              <div className='flex'>
                {!!draft.error && (
                  <Button
                    variant='ghost'
                    size='icon'
                    disabled={pending}
                    title={t('mailboxPortal.retry')}
                    aria-label={t('mailboxPortal.retry')}
                    onClick={() => {
                      if (!lock.current) {
                        lock.current = true
                        upload.mutate([draft])
                      }
                    }}
                  >
                    <RefreshCw />
                  </Button>
                )}
                <Button
                  variant='ghost'
                  size='icon'
                  disabled={pending}
                  title={t('mailboxPortal.remove')}
                  aria-label={t('mailboxPortal.remove')}
                  onClick={() => {
                    URL.revokeObjectURL(draft.url)
                    urls.current.delete(draft.url)
                    setDrafts((items) =>
                      items.filter((item) => item.url !== draft.url)
                    )
                  }}
                >
                  <Trash2 />
                </Button>
              </div>
            </div>
            {!!draft.error && (
              <div className='p-2'>
                <ErrorMessage error={draft.error} />
              </div>
            )}
          </div>
        ))}
      </div>
      <ErrorMessage error={error} />
      <div className='bg-background sticky bottom-0 -mx-4 border-t px-4 py-3 sm:-mx-6 sm:px-6'>
        <Button
          className='h-10 w-full'
          disabled={
            pending ||
            !drafts.length ||
            drafts.some((item) => !item.attachment || item.error)
          }
          onClick={() => {
            if (!lock.current) {
              lock.current = true
              setError(null)
              submit.mutate()
            }
          }}
        >
          <Send />
          {t(pending ? 'mailboxPortal.loading' : 'mailboxPortal.submitReview')}
        </Button>
      </div>
    </section>
  )
}
