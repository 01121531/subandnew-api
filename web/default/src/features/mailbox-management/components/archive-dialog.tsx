import { useTranslation } from 'react-i18next'

import { mailboxApi } from '../api'
import { useMailboxMutation } from '../hooks'
import type { AccountType, VersionedID } from '../types'
import { Confirm } from './common'

export function ArchiveDialog(props: {
  accountType: AccountType
  items: VersionedID[]
  restore: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const mutation = useMailboxMutation(async () => {
    await mailboxApi.archive(props.accountType, props.items, props.restore)
  }, props.onClose)
  return (
    <Confirm
      title={t(`mailbox.archive.${props.restore ? 'restore' : 'delete'}`)}
      description={t(
        `mailbox.archive.${props.restore ? 'restoreConfirm' : 'confirm'}`,
        { count: props.items.length }
      )}
      pending={mutation.isPending}
      onClose={props.onClose}
      onConfirm={() => mutation.submit(undefined)}
    />
  )
}
