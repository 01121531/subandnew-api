import { afterEach, describe, expect, spyOn, test } from 'bun:test'

import { AxiosError, AxiosHeaders } from 'axios'

import { api } from '@/lib/api'

import { importBody, mailboxApi, unwrap } from './api'
import { MailboxError } from './lib/errors'
import type { Account, Envelope, ImportSource } from './types'

let request: ReturnType<typeof spyOn<typeof api, 'request'>> | undefined
afterEach(() => request?.mockRestore())
const success = {
  data: {
    success: true,
    data: { items: [], rows: [], issues: [], total: 0, valid: true },
  },
}
describe('mailbox control-plane API', () => {
  test('archive and restore send explicit pool and versions, without credentials', async () => {
    request = spyOn(api, 'request').mockResolvedValue(success)
    for (const restore of [false, true]) {
      await mailboxApi.archive('opening', [{ id: 3, version: 7 }], restore)
      expect(request.mock.calls.at(-1)?.[0]).toMatchObject({
        method: 'POST',
        url: `/api/mailbox-management/accounts/${restore ? 'restore' : 'archive'}`,
        data: { account_type: 'opening', items: [{ id: 3, version: 7 }] },
      })
    }
    await mailboxApi.accounts({
      page: 1,
      page_size: 20,
      archived: true,
      account_type: 'opening',
    })
    expect(request.mock.calls.at(-1)?.[0].params).toMatchObject({
      archived: true,
      account_type: 'opening',
    })
  })
  test('CVV options preserve availability reasons', async () => {
    request = spyOn(api, 'request')
    for (const reason of [
      'not_enabled',
      'node_unsupported',
      'permission_denied',
    ]) {
      request.mockResolvedValueOnce({
        data: {
          success: true,
          data: {
            temporary_cvv_enabled: false,
            temporary_cvv_unavailable_reason: reason,
          },
        },
      })
      expect(await mailboxApi.importOptions()).toEqual({
        temporary_cvv_enabled: false,
        temporary_cvv_unavailable_reason: reason,
      })
    }
  })
  test('temporary CVV handoff is scoped, versioned and never echoed into client cache', async () => {
    request = spyOn(api, 'request').mockResolvedValueOnce({
      data: { success: true, data: { expires_at: 123, cvv: '007' } },
    })
    expect(await mailboxApi.provideTemporaryCvv(4, 6, '007')).toEqual({
      expires_at: 123,
    })
    expect(request.mock.calls[0][0]).toMatchObject({
      url: '/api/mailbox-management/accounts/4/temporary-cvv',
      method: 'POST',
      params: { account_type: 'opening' },
      data: { version: 6, cvv: '007' },
      headers: { 'X-Mailbox-Request': '1' },
    })
  })
  for (const accountType of ['refund', 'opening'] as const) {
    test(`${accountType} scopes lists, detail, credentials, review and private images explicitly`, async () => {
      request = spyOn(api, 'request').mockResolvedValue(success)
      const controller = new AbortController()
      await mailboxApi.accounts({
        page: 2,
        page_size: 20,
        account_type: accountType,
        operator_id: 9,
      })
      await mailboxApi.accountOperators(controller.signal, accountType)
      await mailboxApi.submissions({
        page: 3,
        page_size: 20,
        status: 'approved',
        account_type: accountType,
      })
      await mailboxApi.audits({
        page: 4,
        page_size: 20,
        account_type: accountType,
      })
      request.mockResolvedValueOnce({
        data: { success: true, data: { id: 4, account_type: accountType } },
      })
      await mailboxApi.account(4, controller.signal, accountType)
      for (const kind of ['password', 'otp', 'card'] as const) {
        await mailboxApi.credentials(4, kind, controller.signal, accountType)
        expect(request.mock.calls.at(-1)?.[0]).toMatchObject({
          url: '/api/mailbox-management/accounts/4/credentials',
          method: 'POST',
          signal: controller.signal,
        })
        expect(request.mock.calls.at(-1)?.[0].data).toEqual({ kind })
      }
      await mailboxApi.review(5, {
        account_type: accountType,
        version: 6,
        status: 'approved',
        reason: '',
      })
      expect(request.mock.calls.at(-1)?.[0].data).toEqual({
        version: 6,
        status: 'approved',
        reason: '',
      })
      request.mockResolvedValueOnce({
        data: new Blob(['image'], { type: 'image/png' }),
      })
      await mailboxApi.attachment('private/id', controller.signal, accountType)
      expect(request.mock.calls.at(-1)?.[0].url).toBe(
        '/api/mailbox-management/attachments/private%2Fid'
      )
      for (const [config] of request.mock.calls) {
        expect(config.params).toMatchObject({ account_type: accountType })
      }
    })
    test(`${accountType} import preview and commit preserve exact source and scope`, async () => {
      request = spyOn(api, 'request').mockResolvedValue(success)
      const source: ImportSource = {
        account_type: accountType,
        format: 'text',
        text: `synthetic@example.test\t password \tTESTONLY${accountType === 'opening' ? '\t4111111111111111\t12/30' : ''}`,
      }
      await mailboxApi.preview(source)
      await mailboxApi.import(source)
      for (const [config] of request.mock.calls) {
        expect(config.data).toEqual(source)
      }
      const file = new File(['synthetic only'], 'sample.xlsx')
      const body = importBody({
        account_type: accountType,
        format: 'xlsx',
        file,
      }) as FormData
      expect([...body.keys()].sort()).toEqual(['account_type', 'file'])
      expect(body.get('account_type')).toBe(accountType)
      await mailboxApi.assign({
        account_type: accountType,
        items: [{ id: 3, version: 8 }],
        operator_id: 9,
      })
      expect(request.mock.calls.at(-1)?.[0].data).toEqual({
        account_type: accountType,
        items: [{ id: 3, version: 8 }],
        operator_id: 9,
      })
    })
  }
  test('legacy detail and action calls explicitly select refund', async () => {
    request = spyOn(api, 'request').mockResolvedValue(success)
    request.mockResolvedValueOnce({ data: { success: true, data: { id: 4 } } })
    expect((await mailboxApi.account(4)).account_type).toBe('refund')
    await mailboxApi.credentials(4, 'password')
    await mailboxApi.review(5, { version: 6, status: 'approved', reason: '' })
    for (const [config] of request.mock.calls) {
      expect(config.params).toEqual({ account_type: 'refund' })
    }
  })
  test('metadata strips unexpected card secrets before entering the query cache and rejects wrong-pool detail', async () => {
    const raw = {
      id: 4,
      account_type: 'opening',
      card_last4: '1111',
      card_number: '4111111111111111',
      card_expiry: '12/30',
      cvv: '000',
    }
    request = spyOn(api, 'request').mockResolvedValue({
      data: { success: true, data: raw },
    })
    const result = await mailboxApi.account(4, undefined, 'opening')
    expect(result.card_last4).toBe('1111')
    expect(result).not.toHaveProperty('card_number')
    expect(result).not.toHaveProperty('card_expiry')
    expect(result).not.toHaveProperty('cvv')
    await expect(mailboxApi.account(4)).rejects.toMatchObject({
      code: 'mailbox_not_found',
    })
    request.mockResolvedValueOnce({
      data: { success: true, data: { items: [raw], total: 1 } },
    })
    const page = await mailboxApi.accounts({
      page: 1,
      page_size: 20,
      account_type: 'opening',
    })
    expect(page.items[0]).toEqual(result)
    request.mockResolvedValueOnce({
      data: {
        success: true,
        data: { items: [raw as unknown as Account], total: 1 },
      },
    })
    await expect(
      mailboxApi.accounts({ page: 1, page_size: 20 })
    ).rejects.toMatchObject({ code: 'mailbox_not_found' })
  })
  test('preview exposes only valid last-four metadata, never full PAN or unrecognized fields', async () => {
    request = spyOn(api, 'request').mockResolvedValueOnce({
      data: {
        success: true,
        data: {
          rows: [
            {
              row: 1,
              email: 'synthetic@example.test',
              card_last4: '1111',
              card_number: '4111111111111111',
              cvv: '000',
            },
            {
              row: 2,
              email: 'other@example.test',
              card_last4: '4111111111111111',
            },
          ],
          issues: [],
          total: 2,
          valid: true,
          notices: [
            {
              row: 1,
              code: 'mailbox_import_recovery_email_ignored',
              recovery_email: 'private@example.test',
              password: 'private-password',
            },
          ],
          card_number: '4111111111111111',
        },
      },
    })
    const preview = await mailboxApi.preview({
      format: 'text',
      account_type: 'opening',
      text: 'synthetic',
    })
    expect(preview.rows[0].card_last4).toBe('1111')
    expect(preview.rows[1].card_last4).toBe('')
    expect(JSON.stringify(preview)).not.toContain('4111111111111111')
    expect(preview.rows[0]).not.toHaveProperty('cvv')
    expect(preview).not.toHaveProperty('card_number')
    expect(preview.notices).toEqual([
      { row: 1, code: 'mailbox_import_recovery_email_ignored' },
    ])
    expect(JSON.stringify(preview)).not.toContain('private')
  })
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
        expect([...body.keys()].sort()).toEqual(['account_type', 'file'])
        expect(body.get('account_type')).toBe('refund')
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
