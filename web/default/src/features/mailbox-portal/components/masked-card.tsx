import { useTranslation } from 'react-i18next'

import { cardLast4 } from '../api'

export function MaskedCard(props: { last4?: string }) {
  const { t } = useTranslation()
  const last4 = cardLast4(props.last4)
  return (
    <span className='text-muted-foreground text-xs tabular-nums'>
      {t('mailboxPortal.card')}:{' '}
      {last4 ? `**** ${last4}` : t('mailboxPortal.cardUnavailable')}
    </span>
  )
}
