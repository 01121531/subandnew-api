import { Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { buttonVariants } from '@/components/ui/button'
import { mailboxAssistantDownloadUrl } from '@/lib/mailbox-assistant-download'
import { cn } from '@/lib/utils'

export function MailboxAssistantDownload({
  className,
}: {
  className?: string
}) {
  const { t } = useTranslation()
  const version = import.meta.env?.PUBLIC_RELEASE_VERSION
  return (
    <a
      href={mailboxAssistantDownloadUrl(version)}
      target='_blank'
      rel='noopener noreferrer'
      referrerPolicy='no-referrer'
      title={t('mailboxPortal.downloadAssistantHint')}
      className={cn(buttonVariants({ variant: 'outline' }), className)}
    >
      <Download className='size-4 shrink-0' aria-hidden='true' />
      {t('mailboxPortal.downloadAssistant')}
    </a>
  )
}
