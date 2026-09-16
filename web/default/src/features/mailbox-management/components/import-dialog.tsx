import { Download, FileUp } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import { mailboxApi } from '../api'
import { useMailboxMutation } from '../hooks'
import { safeCode } from '../lib/errors'
import { importFailureCSV } from '../lib/import-report'
import type {
  AccountType,
  ImportFormat,
  ImportPreview,
  ImportResult,
  ImportSource,
} from '../types'
import { Field, Modal } from './common'
import { ImportExamples } from './import-examples'

export function ImportDialog(props: {
  accountType: AccountType
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [format, setFormat] = useState<ImportFormat>('text')
  const [text, setText] = useState('')
  const [file, setFile] = useState<File>()
  const [preview, setPreview] = useState<ImportPreview>()
  const [result, setResult] = useState<ImportResult>()
  const [ignoreExtraFields, setIgnoreExtraFields] = useState(false)
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
    if (
      !source.current ||
      !preview ||
      result ||
      (preview.ready ?? (preview.valid ? preview.total : 0)) === 0
    ) {
      return
    }
    const imported = await mailboxApi.import(source.current)
    source.current = undefined
    setResult(imported)
    const message = t('mailbox.admin.importSummary', {
      imported: imported.imported,
      failed: imported.failed,
    })
    if (imported.failed > 0) toast.warning(message)
    else toast.success(message)
  })
  const pending = previewMutation.isPending || commit.isPending
  const ready = preview?.ready ?? (preview?.valid ? preview.total : 0)
  const failures = result?.failures ?? preview?.failures ?? []
  const describe = (code: string) =>
    t(`mailbox.errors.${safeCode(code)}`, {
      defaultValue: t('mailbox.admin.invalidRow'),
    })
  function downloadFailures() {
    try {
      const csv = importFailureCSV(
        failures,
        [
          t('mailbox.admin.reportRow'),
          t('mailbox.admin.reportEmail'),
          t('mailbox.admin.reportReason'),
        ],
        describe
      )
      const url = URL.createObjectURL(
        new Blob([csv], { type: 'text/csv;charset=utf-8' })
      )
      const link = document.createElement('a')
      link.href = url
      link.download = `mailbox-${props.accountType}-import-failures.csv`
      document.body.append(link)
      link.click()
      link.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch {
      toast.error(t('mailbox.admin.reportDownloadFailed'))
    }
  }
  function reset() {
    source.current = undefined
    setPreview(undefined)
    setResult(undefined)
    setFile(undefined)
    setText('')
  }
  function runPreview() {
    const ignore_extra_fields =
      props.accountType === 'refund' && ignoreExtraFields
    if (file) {
      previewMutation.submit({
        format,
        file,
        account_type: props.accountType,
        ignore_extra_fields,
        allow_partial: true,
      })
    } else if (format !== 'xlsx' && text.trim()) {
      previewMutation.submit({
        format,
        text,
        account_type: props.accountType,
        ignore_extra_fields,
        allow_partial: true,
      })
    }
  }
  return (
    <Modal
      title={t('mailbox.admin.importTitle')}
      description={t(`mailbox.admin.pools.${props.accountType}`)}
      dirty={!result && (!!text || !!file || !!preview)}
      pending={pending}
      onClose={props.onClose}
      footer={
        <>
          {result && (
            <>
              <Button variant='outline' onClick={reset}>
                {t('mailbox.admin.startOver')}
              </Button>
              <Button onClick={props.onClose}>
                {t('mailbox.admin.importDone')}
              </Button>
            </>
          )}
          {!result && preview && (
            <>
              <Button variant='outline' disabled={pending} onClick={reset}>
                {t('mailbox.admin.startOver')}
              </Button>
              <Button
                disabled={pending || ready === 0}
                onClick={() => commit.submit(undefined)}
              >
                <FileUp />
                {t('mailbox.admin.importAtomic', { count: ready })}
              </Button>
            </>
          )}
          {!result && !preview && (
            <Button
              disabled={
                pending || (!file && (!text.trim() || format === 'xlsx'))
              }
              onClick={runPreview}
            >
              {t('mailbox.admin.preview')}
            </Button>
          )}
        </>
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
        <p>
          {t(
            props.accountType === 'opening'
              ? 'mailbox.admin.temporaryCvvNotice'
              : 'mailbox.admin.noCvv'
          )}
        </p>
      </div>
      {!preview ? (
        <div className='grid gap-4'>
          <ImportExamples accountType={props.accountType} pending={pending} />
          {props.accountType === 'refund' && (
            <label className='flex items-start gap-3 text-sm'>
              <Checkbox
                checked={ignoreExtraFields}
                disabled={pending}
                onCheckedChange={setIgnoreExtraFields}
              />
              <span>{t('mailbox.admin.ignoreExtraFields')}</span>
            </label>
          )}
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
              failures.length === 0
                ? 'text-emerald-700 dark:text-emerald-400'
                : 'text-amber-700 dark:text-amber-400'
            }
          >
            {result
              ? t('mailbox.admin.importSummary', {
                  imported: result.imported,
                  failed: result.failed,
                })
              : t('mailbox.admin.previewPartial', {
                  ready,
                  failed: failures.length,
                })}
          </p>
          {failures.length > 0 && (
            <>
              <Button
                variant='outline'
                disabled={pending}
                onClick={downloadFailures}
              >
                <Download />
                {t('mailbox.admin.downloadFailures')}
              </Button>
              <p className='text-muted-foreground text-sm'>
                {t('mailbox.admin.failureReportNotice')}
              </p>
              <ul
                className='max-h-64 space-y-2 overflow-y-auto'
                aria-label={t('mailbox.admin.issues')}
              >
                {failures.map((failure) => (
                  <li
                    key={failure.row}
                    className='text-destructive text-sm break-words'
                  >
                    {t('mailbox.admin.row', { row: failure.row })}:{' '}
                    {failure.email || t('mailbox.admin.unrecognizedEmail')}
                    {' · '}
                    {failure.codes.map(describe).join('; ')}
                  </li>
                ))}
              </ul>
            </>
          )}
          {!result && !!preview.notices?.length && (
            <ul className='max-h-48 space-y-2 overflow-y-auto border-l-2 border-amber-500 pl-3'>
              {preview.notices.map((notice) => (
                <li
                  key={`${notice.row}-${notice.code}`}
                  className='text-sm break-words'
                >
                  {t('mailbox.admin.row', { row: notice.row })}:{' '}
                  {t(`mailbox.errors.${safeCode(notice.code)}`, {
                    defaultValue: t('mailbox.admin.invalidRow'),
                  })}
                </li>
              ))}
            </ul>
          )}
          {!result && (
            <ol className='divide-y'>
              {preview.rows
                .filter(
                  (row) => !failures.some((failure) => failure.row === row.row)
                )
                .map((row) => (
                  <li
                    key={row.row}
                    className='flex flex-wrap gap-3 py-2 text-sm'
                  >
                    <span className='text-muted-foreground'>{row.row}</span>
                    <span className='min-w-0 break-all'>{row.email}</span>
                    {!row.otp_available && (
                      <span className='text-muted-foreground'>
                        {t('mailbox.admin.noOtp')}
                      </span>
                    )}
                    {props.accountType === 'opening' && row.card_last4 && (
                      <span className='text-muted-foreground'>
                        {t('mailbox.admin.cardEnding', {
                          last4: row.card_last4,
                        })}
                      </span>
                    )}
                  </li>
                ))}
            </ol>
          )}
        </div>
      )}
    </Modal>
  )
}
