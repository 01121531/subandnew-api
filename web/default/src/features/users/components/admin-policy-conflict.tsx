import { useQuery } from '@tanstack/react-query'
import { useFormContext } from 'react-hook-form'

import { Button } from '@/components/ui/button'
import { ADMIN_DATA_FIELD_KEYS } from '@/lib/admin-data-policy'

import { getUser } from '../api'
import { useAdminEditorLabels } from '../lib/admin-editor-labels'
import type { UserFormValues } from '../lib/user-form'

export function AdminPolicyConflict(props: {
  userId: number
  onResolved: () => void
}) {
  const label = useAdminEditorLabels()
  const form = useFormContext<UserFormValues>()
  const latest = useQuery({
    queryKey: ['users', 'policy-conflict', props.userId],
    queryFn: async () => {
      const result = await getUser(props.userId)
      if (!result.success || !result.data?.admin_data_policy) {
        throw new Error(result.message || 'Policy unavailable')
      }
      return result.data.admin_data_policy
    },
    enabled: false,
    gcTime: 0,
  })
  return (
    <section
      className='border-destructive space-y-3 rounded-md border p-4'
      aria-label={label('conflict')}
    >
      <p role='alert' className='text-destructive text-sm'>
        {label('conflict')}
      </p>
      <Button
        type='button'
        variant='outline'
        disabled={latest.isFetching}
        onClick={() => void latest.refetch()}
      >
        {label('latest')}
      </Button>
      {latest.isError && (
        <p role='alert' className='text-destructive text-sm'>
          {label('loadError')}
        </p>
      )}
      {latest.isSuccess && !latest.isFetching && (
        <>
          <dl className='grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm'>
            <dt>{label('revision')}</dt>
            <dd>{latest.data.revision}</dd>
            <dt>{label('instances')}</dt>
            <dd className='break-words'>
              {latest.data.instance_scope === 'all'
                ? label('all')
                : latest.data.instance_ids.map((id) => `#${id}`).join(', ') ||
                  '0'}
            </dd>
            <dt>{label('fields')}</dt>
            <dd className='break-words'>
              {Object.entries(latest.data.fields)
                .filter(([, allowed]) => allowed)
                .map(([key]) => {
                  const known = ADMIN_DATA_FIELD_KEYS.find(
                    (field) => field === key
                  )
                  return known ? label(known) : key
                })
                .join(', ') || '0'}
            </dd>
          </dl>
          <Button
            type='button'
            variant='outline'
            onClick={() => {
              const draft = form.getValues('admin_data_policy')
              if (!draft) return
              form.setValue(
                'admin_data_policy',
                { ...draft, revision: latest.data.revision },
                { shouldDirty: true }
              )
              props.onResolved()
            }}
          >
            {label('useRevision')}
          </Button>
        </>
      )}
    </section>
  )
}
