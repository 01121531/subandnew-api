import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { SideDrawerSection } from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { FormField, FormItem, FormMessage } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  ADMIN_DATA_FIELD_KEYS,
  createDefaultAdminDataPolicy,
  withAdminInstanceScope,
} from '@/lib/admin-data-policy'
import { getAdminDataInstances } from '@/lib/admin-data-policy-api'
import type { PermissionCatalog } from '@/lib/admin-permissions'

import { useAdminEditorLabels } from '../lib/admin-editor-labels'
import type { UserFormValues } from '../lib/user-form'

export function AdminDataPolicyEditor(props: { catalog: PermissionCatalog }) {
  const { t } = useTranslation()
  const label = useAdminEditorLabels()
  const form = useFormContext<UserFormValues>()
  const policy = form.watch('admin_data_policy')
  const [search, setSearch] = useState('')
  const instances = useQuery({
    queryKey: ['users', 'admin-policy-instances'],
    queryFn: ({ signal }) => getAdminDataInstances(signal),
    enabled: policy?.instance_scope === 'selected',
  })
  if (!policy) {
    return (
      <SideDrawerSection>
        <h3 className='text-sm font-medium'>{label('fields')}</h3>
        <p className='text-muted-foreground text-sm'>{label('missing')}</p>
        <Button
          type='button'
          variant='outline'
          onClick={() =>
            form.setValue('admin_data_policy', createDefaultAdminDataPolicy(), {
              shouldDirty: true,
            })
          }
        >
          {label('configure')}
        </Button>
      </SideDrawerSection>
    )
  }
  const available = instances.data ?? []
  const options = [
    ...available.map((instance) => ({ id: instance.id, name: instance.name })),
    ...policy.instance_ids
      .filter((id) => !available.some((instance) => instance.id === id))
      .map((id) => ({ id, name: `#${id}` })),
  ].filter((instance) =>
    `${instance.name} ${instance.id}`
      .toLowerCase()
      .includes(search.toLowerCase())
  )
  const fields = props.catalog.data_fields?.length
    ? props.catalog.data_fields
    : ADMIN_DATA_FIELD_KEYS.map((key) => ({
        key,
        label_key: `adminData.fields.${key}`,
      }))

  return (
    <>
      <SideDrawerSection>
        <h3 className='text-sm font-medium'>{label('instances')}</h3>
        <FormField
          control={form.control}
          name='admin_data_policy'
          render={({ field }) => (
            <FormItem>
              <fieldset className='flex flex-wrap gap-4'>
                <legend className='sr-only'>{label('instances')}</legend>
                {(['selected', 'all'] as const).map((scope) => (
                  <label
                    key={scope}
                    className='flex items-center gap-2 text-sm'
                  >
                    <input
                      type='radio'
                      name='instance-scope'
                      value={scope}
                      checked={policy.instance_scope === scope}
                      onChange={() =>
                        field.onChange(withAdminInstanceScope(policy, scope))
                      }
                    />
                    {label(scope)}
                  </label>
                ))}
              </fieldset>
              {policy.instance_scope === 'selected' && (
                <div className='space-y-2'>
                  <Input
                    aria-label={label('search')}
                    placeholder={label('search')}
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                  />
                  {instances.isPending && (
                    <p role='status' className='text-sm'>
                      {t('Loading...')}
                    </p>
                  )}
                  {instances.isError && (
                    <div role='alert' className='text-destructive text-sm'>
                      {label('instanceError')}
                      <Button
                        type='button'
                        variant='ghost'
                        onClick={() => void instances.refetch()}
                      >
                        {label('retry')}
                      </Button>
                    </div>
                  )}
                  <div className='max-h-56 overflow-y-auto rounded-md border p-3'>
                    {options.map((instance) => (
                      <label
                        key={instance.id}
                        className='flex min-h-9 items-center gap-3 text-sm'
                      >
                        <Checkbox
                          checked={policy.instance_ids.includes(instance.id)}
                          onCheckedChange={(checked) => {
                            const ids = policy.instance_ids.filter(
                              (id) => id !== instance.id
                            )
                            field.onChange({
                              ...policy,
                              instance_ids:
                                checked === true ? [...ids, instance.id] : ids,
                            })
                          }}
                        />
                        <span className='min-w-0 break-words'>
                          {instance.name}{' '}
                          <span className='text-muted-foreground'>
                            #{instance.id}
                          </span>
                        </span>
                      </label>
                    ))}
                    {!instances.isPending &&
                      !instances.isError &&
                      options.length === 0 && (
                        <p className='text-muted-foreground text-sm'>
                          {label('noInstances')}
                        </p>
                      )}
                  </div>
                </div>
              )}
              <FormMessage />
            </FormItem>
          )}
        />
      </SideDrawerSection>
      <SideDrawerSection>
        <h3 className='text-sm font-medium'>{label('fields')}</h3>
        <FormField
          control={form.control}
          name='admin_data_policy'
          render={({ field }) => (
            <FormItem>
              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                {fields.map((definition) => {
                  const knownKey = ADMIN_DATA_FIELD_KEYS.find(
                    (key) => key === definition.key
                  )
                  return (
                    <label
                      key={definition.key}
                      className='flex items-center gap-3 text-sm'
                    >
                      <Checkbox
                        checked={policy.fields[definition.key] === true}
                        onCheckedChange={(checked) =>
                          field.onChange({
                            ...policy,
                            fields: {
                              ...policy.fields,
                              [definition.key]: checked === true,
                            },
                          })
                        }
                      />
                      <span className='min-w-0 break-words'>
                        {t(definition.label_key, {
                          defaultValue: knownKey
                            ? label(knownKey)
                            : definition.key,
                        })}
                      </span>
                    </label>
                  )
                })}
              </div>
              <FormMessage />
            </FormItem>
          )}
        />
        <p className='text-muted-foreground text-xs'>
          {label('revision')}: {policy.revision}
        </p>
      </SideDrawerSection>
    </>
  )
}
