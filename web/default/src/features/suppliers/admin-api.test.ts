import { afterEach, describe, expect, spyOn, test } from 'bun:test'

import { AxiosError, AxiosHeaders } from 'axios'

import { api } from '@/lib/api'

import { adminApi } from './admin-api'
import { errorKey } from './lib/errors'
import { SupplierRequestError } from './portal-api'

let request: ReturnType<typeof spyOn<typeof api, 'request'>> | undefined
afterEach(() => {
  request?.mockRestore()
})

describe('supplier admin API', () => {
  test('takeover is an explicit POST without stale configuration or credentials', async () => {
    request = spyOn(api, 'request').mockResolvedValueOnce({
      data: { success: true, data: { completed: true } },
    })
    expect(await adminApi.takeOwnership(7)).toEqual({ completed: true })
    expect(request).toHaveBeenCalledWith({
      url: '/api/suppliers/7/takeover',
      method: 'POST',
      data: undefined,
      skipBusinessError: true,
      skipErrorHandler: true,
    })
    request.mockResolvedValueOnce({
      data: { success: false, message: 'private diagnostic' },
    })
    await expect(adminApi.takeOwnership(7)).rejects.toThrow('REQUEST_FAILED')
  })
  test('soft delete uses the supplier endpoint without credential data', async () => {
    request = spyOn(api, 'request').mockResolvedValueOnce({
      data: { success: true, data: { completed: true } },
    })
    await adminApi.delete(7)
    expect(request).toHaveBeenCalledWith({
      url: '/api/suppliers/7',
      method: 'DELETE',
      data: undefined,
      skipBusinessError: true,
      skipErrorHandler: true,
    })
  })
  test('Axios business failures retain safe codes and actionable localized reasons', async () => {
    request = spyOn(api, 'request')
    for (const [code, key] of [
      ['supplier_binding_conflict', 'supplier.bindingConflict'],
      ['supplier_binding_limit', 'supplier.bindingLimit'],
      ['supplier_username_conflict', 'supplier.usernameConflict'],
      ['supplier_encryption_unavailable', 'supplier.encryptionUnavailable'],
    ]) {
      request.mockRejectedValueOnce(
        new AxiosError(
          'private transport diagnostic',
          'ERR_BAD_REQUEST',
          undefined,
          undefined,
          {
            data: { success: false, message: code },
            status: 409,
            statusText: 'Conflict',
            headers: {},
            config: { headers: new AxiosHeaders() },
          }
        )
      )
      try {
        await adminApi.list(1)
        throw new Error('expected rejection')
      } catch (error) {
        expect(error).toBeInstanceOf(SupplierRequestError)
        expect(errorKey(error)).toBe(key)
      }
    }
  })
  test('unsafe Axios payloads never surface transport diagnostics', async () => {
    request = spyOn(api, 'request').mockRejectedValueOnce(
      new AxiosError('private diagnostic', 'ERR_NETWORK')
    )
    await expect(adminApi.list(1)).rejects.toThrow('REQUEST_FAILED')
  })
})
