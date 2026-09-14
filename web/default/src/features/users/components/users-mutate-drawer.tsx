/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import {
  EMPTY_PERMISSION_CATALOG,
  normalizeAdminPermissions,
} from '@/lib/admin-permissions'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { createUser, getPermissionCatalog, getUser, updateUser } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import {
  USER_FORM_DEFAULT_VALUES,
  transformFormDataToPayload,
  transformUserToFormDefaults,
  userFormSchema,
  type UserFormValues,
} from '../lib'
import {
  isAdminPolicyConflict,
  canManageUser,
} from '../lib/admin-editor-access'
import { useAdminEditorLabels } from '../lib/admin-editor-labels'
import {
  createAdminFormDefaults,
  explicitAdminPermissions,
} from '../lib/user-form'
import type { User } from '../types'
import { AdminDataPolicyEditor } from './admin-data-policy-editor'
import { AdminPolicyConflict } from './admin-policy-conflict'
import { useUsers } from './users-provider'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: User
  createAdmin?: boolean
}

export function UsersMutateDrawer({
  open,
  onOpenChange,
  currentRow,
  createAdmin = false,
}: Props) {
  const { t } = useTranslation()
  const label = useAdminEditorLabels()
  const isUpdate = !!currentRow
  const { triggerRefresh } = useUsers()
  const currentUser = useAuthStore((state) => state.auth.user)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [conflict, setConflict] = useState(false)
  const canManage = currentRow
    ? canManageUser(currentUser, currentRow)
    : currentUser?.role === ROLE.SUPER_ADMIN
  const catalogQuery = useQuery({
    queryKey: ['admin-permission-catalog'],
    queryFn: getPermissionCatalog,
    staleTime: 5 * 60 * 1000,
    enabled: open && canManage && currentUser?.role === ROLE.SUPER_ADMIN,
  })
  const catalog = catalogQuery.data ?? EMPTY_PERMISSION_CATALOG
  const form = useForm<UserFormValues>({
    resolver: zodResolver(userFormSchema),
    defaultValues: createAdmin
      ? createAdminFormDefaults()
      : USER_FORM_DEFAULT_VALUES,
  })
  const userQuery = useQuery({
    queryKey: ['users', 'edit', currentRow?.id],
    queryFn: async () => {
      if (!currentRow) throw new Error('User ID required')
      const result = await getUser(currentRow.id)
      if (!result.success || !result.data) {
        throw new Error(result.message || 'User request failed')
      }
      return result.data
    },
    enabled: open && !!currentRow && canManage,
    staleTime: 0,
    refetchOnWindowFocus: false,
  })
  const saveMutation = useMutation({
    mutationFn: (payload: ReturnType<typeof transformFormDataToPayload>) =>
      payload.id !== undefined
        ? updateUser({ ...payload, id: payload.id })
        : createUser(payload),
    onError: () => {},
  })

  useEffect(() => {
    if (
      open &&
      userQuery.data &&
      !userQuery.isFetching &&
      !form.formState.isDirty
    ) {
      form.reset(transformUserToFormDefaults(userQuery.data))
    }
  }, [open, userQuery.data, userQuery.isFetching, form, form.formState.isDirty])

  const selectedRole = form.watch('role') ?? currentRow?.role ?? 0
  const canEditPermissions =
    canManage &&
    currentUser?.role === ROLE.SUPER_ADMIN &&
    selectedRole === ROLE.ADMIN
  const loading = isUpdate && (userQuery.isPending || userQuery.isFetching)
  const loadFailed = isUpdate && userQuery.isError
  const permissionsReady =
    !canEditPermissions ||
    (catalogQuery.isSuccess && catalog.resources.length > 0)

  const onSubmit = async (values: UserFormValues) => {
    if (!canManage || loading || loadFailed || !permissionsReady || conflict) {
      return
    }
    if (!isUpdate && values.role !== (createAdmin ? ROLE.ADMIN : ROLE.USER)) {
      return
    }
    if (userQuery.data && !canManageUser(currentUser, userQuery.data)) return
    if (
      !isUpdate &&
      ((values.password?.length ?? 0) < 8 ||
        (values.password?.length ?? 0) > 20)
    ) {
      form.setError('password', {
        message: t('Password must be between 8 and 20 characters'),
      })
      return
    }
    setIsSubmitting(true)
    try {
      const payload = transformFormDataToPayload(
        values,
        currentRow?.id,
        catalog
      )
      if (
        !canEditPermissions ||
        (isUpdate && !form.formState.dirtyFields.admin_data_policy)
      ) {
        delete payload.admin_data_policy
      }
      if (
        !canEditPermissions ||
        (isUpdate && !form.formState.dirtyFields.admin_permissions)
      ) {
        delete payload.admin_permissions
      }
      const result = await saveMutation.mutateAsync(payload)
      if (!result.success) {
        if (isAdminPolicyConflict(result)) {
          setConflict(true)
          return
        }
        toast.error(result.message || t(ERROR_MESSAGES.UPDATE_FAILED))
        return
      }
      toast.success(
        t(
          isUpdate
            ? SUCCESS_MESSAGES.USER_UPDATED
            : SUCCESS_MESSAGES.USER_CREATED
        )
      )
      onOpenChange(false)
      triggerRefresh()
    } catch (error) {
      if (isAdminPolicyConflict(error)) setConflict(true)
      else handleServerError(error)
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        aria-describedby={undefined}
        className={sideDrawerContentClassName('sm:max-w-[600px]')}
      >
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {canEditPermissions
              ? label(isUpdate ? 'update' : 'create')
              : t(isUpdate ? 'Update User' : 'Create User')}
          </SheetTitle>
        </SheetHeader>
        <Form {...form}>
          <form
            id='user-form'
            onSubmit={form.handleSubmit(onSubmit)}
            className={sideDrawerFormClassName()}
          >
            {(loadFailed ||
              (canEditPermissions &&
                !permissionsReady &&
                !catalogQuery.isPending)) && (
              <div role='alert' className='text-destructive text-sm'>
                {label('loadError')}
                <Button
                  type='button'
                  variant='ghost'
                  onClick={() => {
                    if (isUpdate) void userQuery.refetch()
                    if (canEditPermissions) void catalogQuery.refetch()
                  }}
                >
                  {label('retry')}
                </Button>
              </div>
            )}
            {loading && <p role='status'>{t('Loading...')}</p>}
            {conflict && currentRow && (
              <AdminPolicyConflict
                userId={currentRow.id}
                onResolved={() => setConflict(false)}
              />
            )}
            <fieldset
              disabled={!canManage || loading || loadFailed || isSubmitting}
              className='min-w-0 space-y-6'
            >
              <SideDrawerSection>
                {canEditPermissions && (
                  <h3 className='text-sm font-medium'>{label('basic')}</h3>
                )}
                <FormField
                  control={form.control}
                  name='username'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Username')}</FormLabel>
                      <FormControl>
                        <Input {...field} disabled={isUpdate} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='display_name'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Display Name')}</FormLabel>
                      <FormControl>
                        <Input {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='password'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Password')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='password'
                          placeholder={
                            isUpdate
                              ? t('Leave empty to keep unchanged')
                              : t('Enter password (8-20 characters)')
                          }
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                {isUpdate && (
                  <FormField
                    control={form.control}
                    name='remark'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Remark')}</FormLabel>
                        <FormControl>
                          <Textarea {...field} rows={3} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}
              </SideDrawerSection>

              {canEditPermissions && catalog.resources.length > 0 && (
                <SideDrawerSection>
                  <h3 className='text-sm font-medium'>{label('functions')}</h3>
                  <FormField
                    control={form.control}
                    name='admin_permissions'
                    render={({ field }) => {
                      const normalize = isUpdate
                        ? normalizeAdminPermissions
                        : explicitAdminPermissions
                      const selected = normalize(field.value, catalog)
                      return (
                        <FormItem>
                          <div className='space-y-3'>
                            {catalog.resources.map((resource) => (
                              <div
                                key={resource.resource}
                                className='space-y-2 border-b pb-3'
                              >
                                <div className='text-sm font-medium'>
                                  {t(resource.label_key)}
                                </div>
                                {resource.actions.map((action) => (
                                  <label
                                    key={action.action}
                                    className='flex items-center gap-3 text-sm'
                                  >
                                    <Checkbox
                                      checked={
                                        selected[resource.resource]?.[
                                          action.action
                                        ] === true
                                      }
                                      onCheckedChange={(checked) =>
                                        field.onChange({
                                          ...selected,
                                          [resource.resource]: {
                                            ...selected[resource.resource],
                                            [action.action]: checked === true,
                                          },
                                        })
                                      }
                                    />
                                    {t(action.label_key)}
                                  </label>
                                ))}
                              </div>
                            ))}
                          </div>
                          <FormMessage />
                        </FormItem>
                      )
                    }}
                  />
                </SideDrawerSection>
              )}
              {canEditPermissions && (
                <AdminDataPolicyEditor catalog={catalog} />
              )}
            </fieldset>
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
          <Button
            form='user-form'
            type='submit'
            disabled={
              isSubmitting ||
              !canManage ||
              loading ||
              loadFailed ||
              !permissionsReady ||
              conflict
            }
          >
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
