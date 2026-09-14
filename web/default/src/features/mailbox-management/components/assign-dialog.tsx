import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import { mailboxApi } from '../api'
import { useMailboxMutation, useMailboxQuery } from '../hooks'
import { parseVersionedIDs } from '../lib/schemas'
import type { VersionedID } from '../types'
import { Confirm, Field, Modal, QueryState } from './common'

export function AssignDialog(props: {
  items: VersionedID[]
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [operator, setOperator] = useState('')
  const [manual, setManual] = useState('')
  const [error, setError] = useState('')
  const [confirm, setConfirm] = useState(false)
  const options = useMailboxQuery(['operator-options'], (signal) =>
    mailboxApi.operatorOptions(signal)
  )
  const mutation = useMailboxMutation(async () => {
    await mailboxApi.assign({
      items: props.items.length ? props.items : parseVersionedIDs(manual),
      operator_id: Number(operator),
    })
  }, props.onClose)
  function proceed() {
    try {
      if (!props.items.length) parseVersionedIDs(manual)
      setError('')
      setConfirm(true)
    } catch {
      setError('mailbox.admin.invalidIDs')
    }
  }
  return (
    <>
      <Modal
        title={t('mailbox.admin.assign')}
        dirty={operator !== '' || !!manual}
        pending={mutation.isPending}
        onClose={props.onClose}
        footer={
          <Button
            disabled={mutation.isPending || operator === '' || options.isError}
            onClick={proceed}
          >
            {t('mailbox.admin.apply')}
          </Button>
        }
      >
        <div className='space-y-4'>
          {props.items.length ? (
            <p>{t('mailbox.admin.selected', { count: props.items.length })}</p>
          ) : (
            <Field
              id='mailbox-ids'
              label={t('mailbox.admin.idVersions')}
              error={error}
            >
              <Textarea
                id='mailbox-ids'
                value={manual}
                disabled={mutation.isPending}
                onChange={(event) => setManual(event.target.value)}
              />
            </Field>
          )}
          <QueryState
            pending={options.isPending}
            error={options.error}
            retry={() => void options.refetch()}
          >
            <Field id='mailbox-assignee' label={t('mailbox.admin.operator')}>
              <NativeSelect
                id='mailbox-assignee'
                className='w-full'
                value={operator}
                disabled={mutation.isPending}
                onChange={(event) => setOperator(event.target.value)}
              >
                <option value=''>{t('mailbox.admin.chooseOperator')}</option>
                <option value='0'>{t('mailbox.admin.recall')}</option>
                {options.data?.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.display_name ||
                      item.name ||
                      item.username ||
                      `#${item.id}`}
                  </option>
                ))}
              </NativeSelect>
            </Field>
          </QueryState>
        </div>
      </Modal>
      {confirm && (
        <Confirm
          title={
            operator === '0'
              ? t('mailbox.admin.recall')
              : t('mailbox.admin.assign')
          }
          description={`${t('mailbox.admin.assignConfirm')} ${t('mailbox.admin.revocationWarning')}`}
          pending={mutation.isPending}
          onClose={() => setConfirm(false)}
          onConfirm={() => mutation.submit(undefined)}
        />
      )}
    </>
  )
}
