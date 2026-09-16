import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { MailboxRemarkEditor } from '@/components/mailbox-remark-editor'
import { MailboxSubmissionRemark } from '@/components/mailbox-submission-remark'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { useAuthStore } from '@/stores/auth-store'

import { mailboxApi } from '../api'
import { useMailboxMutation } from '../hooks'
import { errorKey } from '../lib/errors'
import { canMailbox } from '../lib/permissions'
import { reviewSchema } from '../lib/schemas'
import type { AccountType, Submission } from '../types'
import { Confirm, Field, Modal, Status, Time } from './common'
import { PrivateImage } from './private-image'

export function ReviewDialog(props: {
  accountType: AccountType
  submission: Submission
  onClose: () => void
}) {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const client = useQueryClient()
  const [submission, setSubmission] = useState(props.submission)
  const [confirm, setConfirm] = useState(false)
  const form = useForm<z.infer<typeof reviewSchema>>({
    resolver: zodResolver(reviewSchema),
    defaultValues: { status: 'approved', reason: '' },
  })
  const mutation = useMailboxMutation(async () => {
    await mailboxApi.review(submission.id, {
      account_type: props.accountType,
      ...form.getValues(),
      version: submission.version,
    })
  }, props.onClose)
  const reviewable =
    submission.status === 'pending' || submission.status === 'submitted'
  return (
    <>
      <Modal
        title={submission.email}
        description={t(`mailbox.admin.pools.${props.accountType}`)}
        dirty={form.formState.isDirty}
        pending={mutation.isPending}
        onClose={props.onClose}
        footer={
          reviewable && (
            <Button
              type='submit'
              form='mailbox-review-form'
              disabled={mutation.isPending}
            >
              {t('mailbox.admin.submitReview')}
            </Button>
          )
        }
      >
        <div className='space-y-5'>
          <div className='flex flex-wrap items-center gap-3 text-sm'>
            <Status value={submission.status} review />
            <span className='break-all'>{submission.operator_name}</span>
            <Time value={submission.created_at} />
          </div>
          <MailboxSubmissionRemark
            remark={submission.remark}
            labelKey='mailbox.admin.submissionRemark'
          />
          {canMailbox(user, 'view') && canMailbox(user, 'review') && (
            <MailboxRemarkEditor
              submission={submission}
              canEdit={!mutation.isPending}
              save={(remark, version) =>
                mailboxApi.editRemark(
                  submission.id,
                  props.accountType,
                  version,
                  remark
                )
              }
              history={(page, signal) =>
                mailboxApi.remarkHistory(
                  submission.id,
                  props.accountType,
                  page,
                  signal
                )
              }
              onSaved={(value) => {
                setSubmission(value)
                void client.invalidateQueries({ queryKey: ['mailbox-admin'] })
              }}
              errorText={(error) => t(errorKey(error))}
            />
          )}
          <p className='border-l-2 border-amber-500 pl-3 text-sm'>
            {t('mailbox.admin.screenshotRedaction')}
          </p>
          <div className='grid gap-3 sm:grid-cols-2'>
            {submission.attachments?.map((attachment, index) => (
              <PrivateImage
                key={`${props.accountType}:${attachment.id}`}
                accountType={props.accountType}
                attachment={attachment}
                index={index}
              />
            ))}
          </div>
          {submission.review_reason && (
            <div className='space-y-2 border-t pt-3'>
              <h3 className='text-sm font-medium'>
                {t('mailbox.admin.reviewReason')}
              </h3>
              <p className='text-sm break-words whitespace-pre-wrap'>
                {submission.review_reason}
              </p>
              <p className='text-muted-foreground text-xs'>
                <Time value={submission.reviewed_at} />
              </p>
            </div>
          )}
          {reviewable && (
            <form
              id='mailbox-review-form'
              className='grid gap-4 border-t pt-4'
              onSubmit={form.handleSubmit(() => setConfirm(true))}
            >
              <Field
                id='mailbox-review-status'
                label={t('mailbox.admin.decision')}
              >
                <NativeSelect
                  id='mailbox-review-status'
                  {...form.register('status')}
                  disabled={mutation.isPending}
                >
                  <option value='approved'>{t('mailbox.admin.approve')}</option>
                  <option value='rejected'>{t('mailbox.admin.reject')}</option>
                </NativeSelect>
              </Field>
              <Field
                id='mailbox-review-reason'
                label={t('mailbox.admin.reviewReason')}
                error={form.formState.errors.reason?.message}
              >
                <Textarea
                  id='mailbox-review-reason'
                  maxLength={2000}
                  {...form.register('reason')}
                  disabled={mutation.isPending}
                />
              </Field>
            </form>
          )}
        </div>
      </Modal>
      {confirm && (
        <Confirm
          title={t('mailbox.admin.submitReview')}
          description={`${t('mailbox.admin.reviewConfirm')} ${t('mailbox.admin.revocationWarning')}`}
          pending={mutation.isPending}
          onClose={() => setConfirm(false)}
          onConfirm={() => mutation.submit(undefined)}
        />
      )}
    </>
  )
}
