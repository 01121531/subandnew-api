import { afterEach, describe, expect, spyOn, test } from 'bun:test'

import { createInstance } from 'i18next'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'

import { takeOwnershipOfAccountDataAPI } from './api'
import { canTakeOwnership, ownershipText } from './ownership'

let put: ReturnType<typeof spyOn<typeof api, 'put'>> | undefined
afterEach(() => put?.mockRestore())

describe('account API ownership', () => {
  test('only a live root can take a different or legacy owner', () => {
    const root = { id: 7, role: ROLE.SUPER_ADMIN, status: 1 }
    expect(canTakeOwnership(root, 2)).toBe(true)
    expect(canTakeOwnership(root, 0)).toBe(true)
    expect(canTakeOwnership(root, 7)).toBe(false)
    expect(canTakeOwnership({ ...root, status: 2 }, 2)).toBe(false)
    expect(canTakeOwnership({ ...root, role: 10 }, 2)).toBe(false)
    expect(canTakeOwnership({ ...root, role: 1 }, 2)).toBe(false)
    expect(canTakeOwnership(null, 2)).toBe(false)
  })

  test('sends takeover intent without stale config or secrets', async () => {
    const saved = { id: 9, created_by: 7, name: 'Partner' }
    put = spyOn(api, 'put').mockResolvedValueOnce({
      data: { success: true, data: saved },
    })
    expect(await takeOwnershipOfAccountDataAPI(9)).toMatchObject(saved)
    expect(put).toHaveBeenCalledWith(
      '/api/account-data-apis/9',
      { takeover: true },
      { skipErrorHandler: true }
    )
    put.mockResolvedValueOnce({
      data: { success: false, message: 'private diagnostic' },
    })
    await expect(takeOwnershipOfAccountDataAPI(9)).rejects.toThrow(
      'account_data_api_takeover_failed'
    )
  })

  test('confirmation names the resource and explains audit and revocation in both locales', async () => {
    const instance = createInstance()
    await instance.init({ lng: 'en', fallbackLng: 'en', resources: {} })
    const t = ownershipText(instance)
    expect(t('title', { name: 'Partner' })).toBe('Take ownership of Partner?')
    expect(t('confirm')).toContain('API keys and portal sessions')
    expect(t('confirm')).toContain('audited')
    expect(t('confirm')).toContain('unsaved edits')
    await instance.changeLanguage('zhCN')
    expect(ownershipText(instance)('action')).toBe('接管')
    expect(ownershipText(instance)('confirm')).toContain('审计')
    expect(ownershipText(instance)('owner', { id: 7 })).toContain('#7')
  })
})
