import { useTranslation } from 'react-i18next'

export function MailboxSubmissionRemark(props: {
  remark?: string
  labelKey: 'mailbox.admin.submissionRemark' | 'mailboxPortal.remark'
}) {
  const { t } = useTranslation()
  if (!props.remark) return null
  return (
    <div className='min-w-0 space-y-1 text-sm'>
      <h3 className='font-medium'>{t(props.labelKey)}</h3>
      <p className='[overflow-wrap:anywhere] whitespace-pre-wrap'>
        {props.remark}
      </p>
    </div>
  )
}
