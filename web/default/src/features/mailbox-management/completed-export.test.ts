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
    expect.objectContaining({ responseType: 'blob', signal: controller.signal })
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
