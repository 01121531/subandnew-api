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
import { useQuery } from '@tanstack/react-query'
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
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import {
  EMPTY_PERMISSION_CATALOG,
  normalizeAdminPermissions,
} from '@/lib/admin-permissions'
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
import type { User } from '../types'
import { useUsers } from './users-provider'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: User
}

export function UsersMutateDrawer({ open, onOpenChange, currentRow }: Props) {
  const { t } = useTranslation()
  const isUpdate = !!currentRow
  const { triggerRefresh } = useUsers()
  const currentUser = useAuthStore((state) => state.auth.user)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const { data: catalog = EMPTY_PERMISSION_CATALOG } = useQuery({
    queryKey: ['admin-permission-catalog'],
    queryFn: getPermissionCatalog,
    staleTime: 5 * 60 * 1000,
  })
  const form = useForm<UserFormValues>({
    resolver: zodResolver(userFormSchema),
    defaultValues: USER_FORM_DEFAULT_VALUES,
  })

  useEffect(() => {
    if (open && currentRow) {
      void getUser(currentRow.id).then((result) => {
        if (result.success && result.data) {
          form.reset(transformUserToFormDefaults(result.data))
        }
      })
    } else if (open) form.reset(USER_FORM_DEFAULT_VALUES)
  }, [open, currentRow, form])

  const selectedRole = form.watch('role') ?? currentRow?.role ?? 0
  const canEditPermissions =
    currentUser?.role === ROLE.SUPER_ADMIN && selectedRole >= ROLE.ADMIN

  const onSubmit = async (values: UserFormValues) => {
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
      const result = isUpdate
        ? await updateUser(payload as typeof payload & { id: number })
        : await createUser(payload)
      if (!result.success) {
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
    } catch {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[600px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isUpdate ? t('Update User') : t('Create User')}
          </SheetTitle>
          <SheetDescription>
            {t('Manage control-plane identity and administrator access.')}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='user-form'
            onSubmit={form.handleSubmit(onSubmit)}
            className={sideDrawerFormClassName()}
          >
            <SideDrawerSection>
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
              {!isUpdate && (
                <FormField
                  control={form.control}
                  name='role'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Role')}</FormLabel>
                      <Select
                        items={[
                          { value: '1', label: t('Common User') },
                          { value: '10', label: t('Admin') },
                        ]}
                        value={String(field.value)}
                        onValueChange={(value) =>
                          value !== null && field.onChange(Number(value))
                        }
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            <SelectItem value='1'>
                              {t('Common User')}
                            </SelectItem>
                            <SelectItem value='10'>{t('Admin')}</SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
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
                <h3 className='text-sm font-medium'>
                  {t('Admin Permissions')}
                </h3>
                <FormField
                  control={form.control}
                  name='admin_permissions'
                  render={({ field }) => {
                    const selected = normalizeAdminPermissions(
                      field.value,
                      catalog
                    )
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
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
          <Button form='user-form' type='submit' disabled={isSubmitting}>
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
