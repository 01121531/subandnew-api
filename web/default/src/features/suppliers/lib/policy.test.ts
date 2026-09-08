import { describe, expect, test } from 'bun:test'

import type { PolicyTemplate } from '../types'
import { policyValues } from './policy'
import { uploadSchema } from './schemas'

const input = {
  binding_id: 1,
  name: 'account',
  outbound_proxy_mode: 'direct',
  outbound_proxy_id: '',
  group_ids: ['g1'],
  policy_template_id: 'p1',
  cc_template_id: 'cc1',
  max_rpm: 10,
  max_tpm: 20,
  max_concurrent: 30,
  max_sessions: 40,
}
describe('supplier policy limits', () => {
  test('maps only limits, supports numeric strings and preserves real zero', () => {
    const template = {
      id: 'p1',
      name: 'policy',
      policy: {
        max_rpm: '1000',
        max_tpm: 50000000,
        max_concurrent: '400',
        max_sessions: 0,
      },
    } as unknown as PolicyTemplate
    const result = { ...input, ...Object.fromEntries(policyValues(template)) }
    expect(result).toEqual({
      ...input,
      max_rpm: 1000,
      max_tpm: 50000000,
      max_concurrent: 400,
      max_sessions: 0,
    })
    expect(uploadSchema.safeParse(result).success).toBe(true)
  })
  test('missing and invalid limits become blank and cannot be submitted', () => {
    for (const policy of [
      undefined,
      {},
      {
        max_rpm: null,
        max_tpm: '',
        max_concurrent: false,
        max_sessions: 'NaN',
      },
      {
        max_rpm: -1,
        max_tpm: Infinity,
        max_concurrent: 1.5,
        max_sessions: 100001,
      },
    ]) {
      const template = {
        id: 'p1',
        name: 'policy',
        policy,
      } as unknown as PolicyTemplate
      const limits = Object.fromEntries(policyValues(template))
      expect(Object.values(limits)).toEqual(['', '', '', ''])
      expect(uploadSchema.safeParse({ ...input, ...limits }).success).toBe(
        false
      )
    }
  })
  test('each template replaces old limits without replacing unrelated settings', () => {
    const a: PolicyTemplate = {
      id: 'a',
      name: 'A',
      policy: { max_rpm: 1, max_tpm: 2, max_concurrent: 3, max_sessions: 4 },
    }
    const b: PolicyTemplate = {
      id: 'b',
      name: 'B',
      policy: { max_rpm: 0, max_tpm: 5, max_concurrent: null, max_sessions: 6 },
    }
    const state = { ...input, ...Object.fromEntries(policyValues(a)) }
    state.max_rpm = 99
    expect(uploadSchema.safeParse(state).success).toBe(true)
    Object.assign(state, Object.fromEntries(policyValues(b)))
    expect(state.max_rpm).toBe(0)
    expect(state.group_ids).toEqual(['g1'])
    expect(state.cc_template_id).toBe('cc1')
    expect(uploadSchema.safeParse(state).success).toBe(false)
  })
})
