import { FileUp } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import { mailboxApi } from '../api'
import { useMailboxMutation } from '../hooks'
import { safeCode } from '../lib/errors'
import type {
  AccountType,
  ImportFormat,
  ImportPreview,
  ImportSource,
} from '../types'
import { Field, Modal } from './common'

export function ImportDialog(props: {
  accountType: AccountType
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [format, setFormat] = useState<ImportFormat>('text')
  const [text, setText] = useState('')
  const [file, setFile] = useState<File>()
  const [preview, setPreview] = useState<ImportPreview>()
  const source = useRef<ImportSource | undefined>(undefined)
  const controller = useRef<AbortController | undefined>(undefined)
  useEffect(
    () => () => {
      controller.current?.abort()
      source.current = undefined
    },
    []
  )
  const previewMutation = useMailboxMutation(async (input: ImportSource) => {
    const request = new AbortController()
    controller.current = request
    const result = await mailboxApi.preview(input, request.signal)
    if (request.signal.aborted) return
    source.current = input
    setPreview(result)
    // Once previewed, only the sanitized server response is rendered.
    setText('')
    setFile(undefined)
  })
  const commit = useMailboxMutation(async () => {
    if (!source.current || !preview?.valid) return
    const result = await mailboxApi.import(source.current)
    source.current = undefined
    toast.success(t('mailbox.admin.imported', { count: result.imported }))
  }, props.onClose)
  const pending = previewMutation.isPending || commit.isPending
  function reset() {
    source.current = undefined
    setPreview(undefined)
    setFile(undefined)
    setText('')
  }
  function runPreview() {
    if (file) {
      previewMutation.submit({ format, file, account_type: props.accountType })
    } else if (format !== 'xlsx' && text.trim()) {
      previewMutation.submit({ format, text, account_type: props.accountType })
    }
  }
  return (
    <Modal
      title={t('mailbox.admin.importTitle')}
      description={t(`mailbox.admin.pools.${props.accountType}`)}
      dirty={!!text || !!file || !!preview}
      pending={pending}
      onClose={props.onClose}
      footer={
        preview ? (
          <>
            <Button variant='outline' disabled={pending} onClick={reset}>
              {t('mailbox.admin.startOver')}
            </Button>
            <Button
              disabled={pending || !preview.valid || preview.total === 0}
              onClick={() => commit.submit(undefined)}
            >
              <FileUp />
              {t('mailbox.admin.importAtomic', { count: preview.total })}
            </Button>
          </>
        ) : (
          <Button
            disabled={pending || (!file && (!text.trim() || format === 'xlsx'))}
            onClick={runPreview}
          >
            {t('mailbox.admin.preview')}
          </Button>
        )
      }
    >
      <div className='mb-4 space-y-2 border-l-2 border-amber-500 pl-3 text-sm'>
        <p>
          {t(
            props.accountType === 'opening'
              ? 'mailbox.admin.openingImport'
              : 'mailbox.admin.refundImport'
          )}
        </p>
        {props.accountType === 'opening' && (
          <p>{t('mailbox.admin.excelPanText')}</p>
        )}
        <p>{t('mailbox.admin.noCvv')}</p>
      </div>
      {!preview ? (
        <div className='grid gap-4'>
          <Field id='mailbox-format' label={t('mailbox.admin.format')}>
            <NativeSelect
              id='mailbox-format'
              value={format}
              disabled={pending}
              onChange={(event) => {
                setFormat(event.target.value as ImportFormat)
                setFile(undefined)
                setText('')
              }}
            >
              <option value='text'>{t('mailbox.admin.text')}</option>
              <option value='csv'>CSV</option>
              <option value='xlsx'>XLSX</option>
            </NativeSelect>
          </Field>
          <Field id='mailbox-import-file' label={t('mailbox.admin.file')}>
            <Input
              key={format}
              id='mailbox-import-file'
              type='file'
              accept={`.${format === 'text' ? 'txt' : format}`}
              disabled={pending}
              onChange={(event) => {
                setFile(event.target.files?.[0])
                setText('')
              }}
            />
          </Field>
          {format !== 'xlsx' && !file && (
            <Field
              id='mailbox-import-text'
              label={t('mailbox.admin.importText')}
            >
              <Textarea
                id='mailbox-import-text'
                className='min-h-48 font-mono'
                value={text}
                autoComplete='off'
                spellCheck={false}
                disabled={pending}
                onChange={(event) => setText(event.target.value)}
              />
            </Field>
          )}
        </div>
      ) : (
        <div className='space-y-4'>
          <p
            role='status'
            className={
              preview.valid
                ? 'text-emerald-700 dark:text-emerald-400'
                : 'text-destructive'
            }
          >
            {t(
              preview.valid
                ? 'mailbox.admin.previewValid'
                : 'mailbox.admin.previewInvalid',
              { count: preview.total }
            )}
          </p>
          {preview.issues.length > 0 && (
            <ul className='space-y-2' aria-label={t('mailbox.admin.issues')}>
              {preview.issues.map((issue) => (
                <li
                  key={`${issue.row}-${issue.code}`}
                  className='text-destructive text-sm break-words'
                >
                  {t('mailbox.admin.row', { row: issue.row })}:{' '}
                  {t(`mailbox.errors.${safeCode(issue.code)}`, {
                    defaultValue: t('mailbox.admin.invalidRow'),
                  })}
                </li>
              ))}
            </ul>
          )}
          <ol className='divide-y'>
            {preview.rows.map((row) => (
              <li key={row.row} className='flex flex-wrap gap-3 py-2 text-sm'>
                <span className='text-muted-foreground'>{row.row}</span>
                <span className='min-w-0 break-all'>{row.email}</span>
                {props.accountType === 'opening' && row.card_last4 && (
                  <span className='text-muted-foreground'>
                    {t('mailbox.admin.cardEnding', { last4: row.card_last4 })}
                  </span>
                )}
              </li>
            ))}
          </ol>
        </div>
      )}
    </Modal>
  )
}
