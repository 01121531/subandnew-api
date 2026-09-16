import { afterEach, beforeEach, expect, spyOn, test } from 'bun:test'

import { AxiosError } from 'axios'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { mailboxApi } from './api'

const original = useAuthStore.getState().auth.user
const admin = { id: 1, username: 'test', role: ROLE.SUPER_ADMIN }
const blob = new Blob(['test'], {
  type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
})
let post: ReturnType<typeof spyOn<typeof api, 'post'>>
beforeEach(() => {
  useAuthStore.getState().auth.setUser(admin)
  post = spyOn(api, 'post').mockResolvedValue({ data: blob })
})
afterEach(() => {
  post.mockRestore()
  useAuthStore.getState().auth.setUser(original)
})

test('exports without page, filters or selected IDs and without query caching', async () => {
  const controller = new AbortController()
  expect(await mailboxApi.exportCompleted(controller.signal)).toBe(blob)
  expect(post).toHaveBeenCalledWith(
    '/api/mailbox-management/accounts/export-completed',
    {},
    expect.objectContaining({
      responseType: 'blob',
      signal: controller.signal,
    })
  )
})

test('each of the three grants is required before requesting credentials', async () => {
  for (const missing of ['view', 'review', 'credentials']) {
    useAuthStore.getState().auth.setUser({
      ...admin,
      role: ROLE.ADMIN,
      permissions: {
        admin_permissions: {
          mailbox_management: {
            view: missing !== 'view',
            review: missing !== 'review',
            credentials: missing !== 'credentials',
          },
        },
      },
    })
    await expect(
      mailboxApi.exportCompleted(new AbortController().signal)
    ).rejects.toMatchObject({ code: 'mailbox_permission_denied' })
  }
  expect(post).not.toHaveBeenCalled()
})

test('a logout or cancelled download cannot return an in-flight credential file', async () => {
  const pending = mailboxApi.exportCompleted(new AbortController().signal)
  useAuthStore.getState().auth.setUser(null)
  await expect(pending).rejects.toMatchObject({
    code: 'mailbox_permission_denied',
  })
  useAuthStore.getState().auth.setUser(admin)
  post.mockResolvedValue({ data: blob })
  const controller = new AbortController()
  controller.abort()
  await expect(
    mailboxApi.exportCompleted(controller.signal)
  ).rejects.toMatchObject({ code: 'mailbox_permission_denied' })
})

test('blob errors expose only a safe error code', async () => {
  const error = new AxiosError('failure')
  Object.assign(error, {
    response: {
      status: 400,
      data: new Blob([
        JSON.stringify({ message: 'mailbox_export_completed_empty' }),
      ]),
    },
  })
  post.mockRejectedValue(error)
  await expect(
    mailboxApi.exportCompleted(new AbortController().signal)
  ).rejects.toMatchObject({
    code: 'mailbox_export_completed_empty',
    status: 400,
  })
})

test('all data export ignores UI paging; issue export preserves only its selected scope', async () => {
  const signal = new AbortController().signal
  expect(await mailboxApi.exportAll(signal)).toBe(blob)
  expect(post).toHaveBeenLastCalledWith(
    '/api/mailbox-management/accounts/export-all',
    {},
    expect.objectContaining({ responseType: 'blob', signal })
  )
  const filters = {
    account_type: 'opening' as const,
    search: 'test',
    status: 'processed',
    kind: 'card',
    operator_id: 7,
  }
  await mailboxApi.exportIssues({ ...filters, scope: 'filtered' }, signal)
  expect(post).toHaveBeenLastCalledWith(
    '/api/mailbox-management/issues/export',
    { ...filters, scope: 'filtered' },
    expect.objectContaining({ signal })
  )
  await mailboxApi.exportIssues({ ...filters, scope: 'all' }, signal)
  expect(post).toHaveBeenLastCalledWith(
    '/api/mailbox-management/issues/export',
    { scope: 'all' },
    expect.objectContaining({ signal })
  )
})

test('new exports discard files after permissions change and reject missing grants', async () => {
  for (const download of [
    mailboxApi.exportAll,
    (signal: AbortSignal) => mailboxApi.exportIssues({ scope: 'all' }, signal),
  ]) {
    useAuthStore.getState().auth.setUser(admin)
    const pending = download(new AbortController().signal)
    useAuthStore.getState().auth.setUser(null)
    await expect(pending).rejects.toMatchObject({
      code: 'mailbox_permission_denied',
    })
    for (const missing of ['view', 'review', 'credentials']) {
      useAuthStore.getState().auth.setUser({
        ...admin,
        role: ROLE.ADMIN,
        permissions: {
          admin_permissions: {
            mailbox_management: {
              view: missing !== 'view',
              review: missing !== 'review',
              credentials: missing !== 'credentials',
            },
          },
        },
      })
      await expect(
        download(new AbortController().signal)
      ).rejects.toMatchObject({ code: 'mailbox_permission_denied' })
    }
  }
})
