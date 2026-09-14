import { afterEach, describe, expect, spyOn, test } from 'bun:test'

import { AxiosError, AxiosHeaders } from 'axios'

import { api } from '@/lib/api'

import { importBody, mailboxApi, unwrap } from './api'
import { MailboxError } from './lib/errors'
import type { Envelope, ImportSource } from './types'

let request: ReturnType<typeof spyOn<typeof api, 'request'>> | undefined
afterEach(() => request?.mockRestore())
const success = { data: { success: true, data: {} } }
describe('mailbox control-plane API', () => {
  test('account operator metadata includes disabled operators without calling management or credential endpoints', async () => {
    const operators = [
      { id: 3, username: 'enabled-user', display_name: 'Enabled' },
      { id: 9, username: 'disabled-user', display_name: 'Disabled' },
    ]
    request = spyOn(api, 'request').mockResolvedValueOnce({
      data: { success: true, data: operators },
    })
    const controller = new AbortController()
    expect(await mailboxApi.accountOperators(controller.signal)).toEqual(
      operators
    )
    expect(request).toHaveBeenCalledTimes(1)
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/account-operators',
      method: 'GET',
      signal: controller.signal,
      data: undefined,
    })
  })
  test('account operator filtering composes with search, status and pagination', async () => {
    request = spyOn(api, 'request').mockResolvedValue(success)
    await mailboxApi.accounts({
      page: 1,
      page_size: 20,
      search: 'example',
      status: 'submitted',
      operator_id: 9,
    })
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/accounts',
      params: {
        page: 1,
        page_size: 20,
        search: 'example',
        status: 'submitted',
        operator_id: 9,
      },
    })
    await mailboxApi.accounts({
      page: 1,
      page_size: 20,
      operator_id: undefined,
    })
    expect(request.mock.calls[1][0]).toMatchObject({
      params: { operator_id: undefined },
    })
  })
  test('text import preserves password whitespace exactly from preview to commit', async () => {
    request = spyOn(api, 'request').mockResolvedValue(success)
    const source: ImportSource = {
      format: 'text',
      text: 'user@example.com\t  test password  \tTESTONLY\r\n',
    }
    await mailboxApi.preview(source)
    await mailboxApi.import(source)
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/imports/preview',
      method: 'POST',
      headers: { 'X-Mailbox-Request': '1' },
      data: source,
      skipBusinessError: true,
      skipErrorHandler: true,
    })
    expect(request.mock.calls[1][0]).toMatchObject({
      url: '/api/mailbox-management/imports',
      headers: { 'X-Mailbox-Request': '1' },
      data: source,
    })
  })
  test('CSV and XLSX uploads send the same original File on preview and commit', async () => {
    request = spyOn(api, 'request').mockResolvedValue(success)
    for (const format of ['csv', 'xlsx'] as const) {
      const file = new File(['test-only-bytes'], `sample.${format}`)
      const source = { format, file }
      await mailboxApi.preview(source)
      await mailboxApi.import(source)
      const calls = request.mock.calls.slice(-2)
      for (const [config] of calls) {
        expect(config.headers).toMatchObject({ 'X-Mailbox-Request': '1' })
        const body = config.data as FormData
        const uploaded = body.get('file') as File
        expect(uploaded.name).toBe(file.name)
        expect(await uploaded.arrayBuffer()).toEqual(await file.arrayBuffer())
        expect(body.get('format')).toBe(format)
      }
      expect(importBody(source)).toBeInstanceOf(FormData)
    }
  })
  test('assignment and review carry exact optimistic versions', async () => {
    request = spyOn(api, 'request').mockResolvedValue(success)
    await mailboxApi.assign({ items: [{ id: 3, version: 8 }], operator_id: 0 })
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/assignments',
      method: 'POST',
      headers: { 'X-Mailbox-Request': '1' },
      data: { items: [{ id: 3, version: 8 }], operator_id: 0 },
    })
    await mailboxApi.review(20, {
      version: 6,
      status: 'rejected',
      reason: 'Unclear',
    })
    expect(request.mock.calls[1][0]).toMatchObject({
      url: '/api/mailbox-management/submissions/20/review',
      method: 'POST',
      headers: { 'X-Mailbox-Request': '1' },
      data: { version: 6, status: 'rejected', reason: 'Unclear' },
    })
  })
  test('assignment options do not call operator management; standalone lists remain independent', async () => {
    request = spyOn(api, 'request').mockResolvedValue(success)
    await mailboxApi.operatorOptions()
    await mailboxApi.submissions({
      page: 2,
      page_size: 20,
      status: 'pending',
      search: 'example',
    })
    expect(request.mock.calls[0][0]?.url).toBe(
      '/api/mailbox-management/operator-options'
    )
    expect(request.mock.calls[1][0]).toMatchObject({
      url: '/api/mailbox-management/submissions',
      params: { page: 2, page_size: 20, status: 'pending', search: 'example' },
    })
  })
  test('passwords and OTP are explicit POST requests and private images are authenticated blobs', async () => {
    request = spyOn(api, 'request').mockResolvedValue(success)
    await mailboxApi.credentials(4, 'password')
    await mailboxApi.credentials(4, 'otp')
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/accounts/4/credentials',
      method: 'POST',
      data: { kind: 'password' },
    })
    expect(request.mock.calls[1][0]?.data).toEqual({ kind: 'otp' })
    request.mockResolvedValueOnce({
      data: new Blob(['image'], { type: 'image/png' }),
    })
    await mailboxApi.attachment('random-id')
    expect(request.mock.calls[2][0]).toMatchObject({
      url: '/api/mailbox-management/attachments/random-id',
      responseType: 'blob',
    })
  })
  test('private image rejects HTML instead of creating a renderable URL', async () => {
    request = spyOn(api, 'request').mockResolvedValueOnce({
      data: new Blob(['private diagnostic'], { type: 'text/html' }),
    })
    await expect(mailboxApi.attachment('id')).rejects.toThrow(
      'mailbox_attachment_invalid'
    )
  })
  test('transport errors retain only safe error codes and HTTP status', async () => {
    request = spyOn(api, 'request').mockRejectedValueOnce(
      new AxiosError(
        'private diagnostic',
        'ERR_BAD_REQUEST',
        undefined,
        undefined,
        {
          status: 409,
          statusText: 'Conflict',
          headers: {},
          config: { headers: new AxiosHeaders() },
          data: { success: false, message: 'mailbox_version_conflict' },
        }
      )
    )
    await expect(
      mailboxApi.assign({ items: [{ id: 1, version: 1 }], operator_id: 2 })
    ).rejects.toMatchObject({ code: 'mailbox_version_conflict', status: 409 })
    expect(() =>
      unwrap({
        success: false,
        message: 'private password',
      } as Envelope<unknown>)
    ).toThrow('mailbox_request_failed')
    expect(() =>
      unwrap({
        success: false,
        message: 'mailbox_permission_denied',
      } as Envelope<unknown>)
    ).toThrow(MailboxError)
  })
})
