import { useMutation } from '@tanstack/react-query'
import { ClipboardPaste, RefreshCw, Send, Trash2, Upload } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import type { IssueKind } from '@/features/mailbox-management/types'

import { mailboxApi, MailboxRequestError } from '../api'
import { pastedImages, readClipboardImages } from '../lib/clipboard-images'
import {
  assertCurrentAssignment,
  canSubmit,
  validateImages,
} from '../lib/guards'
import { normalSubmissionSchema } from '../lib/schemas'
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
  issue?: boolean
}) {
  const { t } = useTranslation()
  const [drafts, setDrafts] = useState<Draft[]>([])
  const [error, setError] = useState<unknown>(null)
  const [kind, setKind] = useState<IssueKind>('email_login')
  const [description, setDescription] = useState('')
  const [remark, setRemark] = useState('')
  const [uncertain, setUncertain] = useState(false)
  const [readingClipboard, setReadingClipboard] = useState(false)
  const section = useRef<HTMLElement>(null)
  const uncertainRef = useRef(false)
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
        ids.length > 5 ||
        ids.some((id) => !id) ||
        new Set(ids).size !== ids.length
      ) {
        throw new Error('Invalid draft selection')
      }
      if (props.issue) {
        uncertainRef.current = true
        setUncertain(true)
        await mailboxApi.report(
          props.csrf,
          props.account.assignment_id,
          {
            version: props.account.assignment_version,
            kind,
            description,
            attachment_ids: ids,
          },
          props.account.account_type ?? 'refund',
          controller.current.signal
        )
        return
      }
      const content = normalSubmissionSchema.safeParse({
        attachment_ids: ids,
        remark,
      })
      if (!content.success) {
        throw new MailboxRequestError(content.error.issues[0].message, 400)
      }
      uncertainRef.current = true
      setUncertain(true)
      await mailboxApi.submit(
        props.csrf,
        props.account.assignment_id,
        props.account.assignment_version,
        ids,
        controller.current.signal,
        props.account.account_type ?? 'refund',
        remark
      )
    },
    onSuccess: () => {
      if (controller.current.signal.aborted) return
      for (const url of urls.current) URL.revokeObjectURL(url)
      urls.current.clear()
      setDrafts([])
      setRemark('')
      uncertainRef.current = false
      setUncertain(false)
      props.leave.current = { dirty: false, pending: false }
      void mailboxClient.invalidateQueries({ queryKey: ['mailbox'] })
      props.onSubmitted()
    },
    onError: async (failure) => {
      if (!controller.current.signal.aborted) setError(failure)
      if (
        !props.issue &&
        failure instanceof MailboxRequestError &&
        failure.code !== 'mailbox_request_failed' &&
        failure.status !== 408 &&
        failure.status >= 400 &&
        failure.status < 500
      ) {
        uncertainRef.current = false
        setUncertain(false)
      }
      if (uncertainRef.current) return
      // Known rejections and preflight failures refresh without replaying the POST.
      await mailboxClient.invalidateQueries({
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
    retry: false,
  })
  const reconcile = useMutation({
    mutationFn: async () => {
      if (!props.issue) {
        const latest = await mailboxApi.account(
          props.account.id,
          controller.current.signal,
          props.account.account_type ?? 'refund'
        )
        if (controller.current.signal.aborted) return
        // Normal submissions have no submitted_version. Matching text or images
        // could find an older submission, so only refresh authoritative account state.
        mailboxClient.setQueryData(
          [
            'mailbox',
            'account',
            props.account.id,
            props.account.account_type ?? 'refund',
          ],
          latest
        )
        void mailboxClient.invalidateQueries({
          queryKey: ['mailbox', 'submissions'],
        })
        void mailboxClient.invalidateQueries({
          queryKey: ['mailbox', 'accounts'],
        })
        return
      }
      const data = await mailboxApi.issues(
        {
          account_type: props.account.account_type ?? 'refund',
          assignment_id: props.account.assignment_id,
          page: 1,
          page_size: 100,
        },
        controller.current.signal
      )
      const ids = new Set(drafts.map((item) => item.attachment?.id))
      const found = data.items.some(
        (item) =>
          item.submitted_version === props.account.assignment_version &&
          item.kind === kind &&
          item.description === description.trim() &&
          item.attachments.length === ids.size &&
          item.attachments.every((file) => ids.has(file.id))
      )
      if (found && !controller.current.signal.aborted) {
        props.leave.current = { dirty: false, pending: false }
        void mailboxClient.invalidateQueries({ queryKey: ['mailbox'] })
        props.onSubmitted()
      }
    },
    retry: false,
    onError: (failure) => setError(failure),
  })
  const pending =
    readingClipboard ||
    upload.isPending ||
    submit.isPending ||
    reconcile.isPending
  const submitLabel = props.issue
    ? 'mailbox.issues.submit'
    : 'mailboxPortal.submitReview'
  const dirty = drafts.length > 0 || !!description || !!remark
  const remarkLength = [...remark.trim()].length
  const remarkValidation = normalSubmissionSchema.shape.remark.safeParse(remark)
  const remarkError = remarkValidation.success
    ? null
    : new MailboxRequestError(remarkValidation.error.issues[0].message, 400)
  const validNormalSubmission = normalSubmissionSchema.safeParse({
    attachment_ids: drafts.map((item) => item.attachment?.id ?? ''),
    remark,
  }).success
  props.leave.current = { dirty, pending }
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (dirty || pending) event.preventDefault()
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [dirty, pending])
  const uploadFiles = upload.mutate
  const select = useCallback(
    (files: File[]) => {
      if (
        lock.current ||
        uncertain ||
        controller.current.signal.aborted ||
        !canSubmit(props.account)
      ) {
        return
      }
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
        uploadFiles(items)
      } catch (failure) {
        setError(failure)
      }
    },
    [drafts.length, props.account, uncertain, uploadFiles]
  )
  useEffect(() => {
    // Listen only inside the current account detail, not elsewhere in the page.
    const container =
      section.current?.closest('[role="dialog"]') ?? section.current
    const paste = (event: Event) => {
      const data = (event as ClipboardEvent).clipboardData
      if (!data) return
      const files = pastedImages(data)
      if (!files.length) return
      event.preventDefault()
      select(files)
    }
    container?.addEventListener('paste', paste)
    return () => container?.removeEventListener('paste', paste)
  }, [select])
  async function pasteClipboard() {
    if (lock.current || uncertain || !canSubmit(props.account)) return
    lock.current = true
    setReadingClipboard(true)
    try {
      if (!navigator.clipboard?.read) throw new Error('Clipboard unavailable')
      const files = await readClipboardImages(() => navigator.clipboard.read())
      if (controller.current.signal.aborted) return
      lock.current = false
      if (!files.length) {
        setError(new MailboxRequestError('mailbox_clipboard_empty', 400))
      } else {
        select(files)
      }
    } catch {
      lock.current = false
      if (!controller.current.signal.aborted) {
        setError(new MailboxRequestError('mailbox_clipboard_unavailable', 400))
      }
    } finally {
      if (!controller.current.signal.aborted) setReadingClipboard(false)
    }
  }
  if (!canSubmit(props.account)) return null
  return (
    <section ref={section} className='grid gap-4 py-5'>
      {!props.issue && (
        <div className='grid min-w-0 gap-2'>
          <label
            htmlFor='mailbox-submission-remark'
            className='text-sm font-medium'
          >
            {t('mailboxPortal.remarkOptional')}
          </label>
          <Textarea
            id='mailbox-submission-remark'
            className='min-h-28 [overflow-wrap:anywhere]'
            autoComplete='off'
            disabled={pending || uncertain}
            value={remark}
            aria-describedby='mailbox-remark-warning mailbox-remark-count mailbox-remark-error'
            aria-invalid={!!remarkError}
            onChange={(event) => setRemark(event.target.value)}
          />
          <p
            id='mailbox-remark-warning'
            className='text-sm text-amber-800 dark:text-amber-300'
          >
            {t('mailboxPortal.remarkWarning')}
          </p>
          <p
            id='mailbox-remark-count'
            className='text-muted-foreground text-xs'
            aria-live='polite'
          >
            {t('mailboxPortal.remarkCount', { count: remarkLength })}
          </p>
          <div id='mailbox-remark-error'>
            <ErrorMessage error={remarkError} />
          </div>
        </div>
      )}
      {props.issue && (
        <div className='grid gap-3'>
          <p className='text-sm text-amber-800 dark:text-amber-300'>
            {t('mailbox.issues.warning')}
          </p>
          <label className='grid gap-2 text-sm'>
            {t('mailbox.issues.kind')}
            <NativeSelect
              aria-label={t('mailbox.issues.kind')}
              disabled={pending || uncertain}
              value={kind}
              onChange={(e) => setKind(e.target.value as IssueKind)}
            >
              {(
                [
                  'email_login',
                  'otp',
                  ...(props.account.account_type === 'opening' ? ['card'] : []),
                  'other',
                ] as IssueKind[]
              ).map((value) => (
                <option key={value} value={value}>
                  {t(`mailbox.issues.${value}`)}
                </option>
              ))}
            </NativeSelect>
          </label>
          <label className='grid gap-2 text-sm'>
            {t('mailbox.issues.description')}
            <Textarea
              maxLength={2000}
              disabled={pending || uncertain}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </label>
        </div>
      )}
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
          disabled={pending || uncertain || drafts.length >= 5}
          aria-label={t('mailboxPortal.upload')}
          aria-describedby='mailbox-redaction-warning'
          onChange={(event) => {
            select([...(event.target.files ?? [])])
            event.target.value = ''
          }}
        />
        <div className='flex flex-wrap gap-2'>
          <Button
            variant='outline'
            disabled={pending || uncertain || drafts.length >= 5}
            title={t('mailboxPortal.pasteScreenshotHint')}
            aria-describedby='mailbox-redaction-warning'
            onClick={() => void pasteClipboard()}
          >
            <ClipboardPaste />
            {t('mailboxPortal.pasteScreenshot')}
          </Button>
          <Button
            variant='outline'
            disabled={pending || uncertain || drafts.length >= 5}
            aria-describedby='mailbox-redaction-warning'
            onClick={() => input.current?.click()}
          >
            <Upload />
            {t('mailboxPortal.upload')}
          </Button>
        </div>
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
                    disabled={pending || uncertain}
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
                  disabled={pending || uncertain}
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
      {uncertain && !pending && (
        <div role='alert' className='grid gap-2 text-sm'>
          <p>
            {t(
              props.issue
                ? 'mailbox.issues.uncertain'
                : 'mailboxPortal.submissionUncertain'
            )}
          </p>
          <Button
            variant='outline'
            disabled={reconcile.isPending}
            onClick={() => {
              setError(null)
              reconcile.mutate()
            }}
          >
            <RefreshCw />
            {t(
              props.issue
                ? 'mailbox.issues.check'
                : 'mailboxPortal.checkSubmission'
            )}
          </Button>
        </div>
      )}
      <div className='bg-background sticky bottom-0 -mx-4 border-t px-4 py-3 sm:-mx-6 sm:px-6'>
        <Button
          className='h-10 w-full'
          disabled={
            pending ||
            uncertain ||
            (props.issue ? !description.trim() : !validNormalSubmission) ||
            drafts.some((item) => !item.attachment || item.error)
          }
          onClick={() => {
            if (!lock.current && !uncertainRef.current) {
              lock.current = true
              setError(null)
              submit.mutate()
            }
          }}
        >
          <Send />
          {t(pending ? 'mailboxPortal.loading' : submitLabel)}
        </Button>
      </div>
    </section>
  )
}
