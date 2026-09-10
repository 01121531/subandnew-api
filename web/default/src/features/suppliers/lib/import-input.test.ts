import { describe, expect, test } from 'bun:test'

import { parseSessionKeys, uploadFormSchema } from './import-input'

const base = {
  binding_id: 11,
  name: 'Demo',
  outbound_proxy_mode: 'direct',
  outbound_proxy_id: '',
  group_ids: [],
  policy_template_id: '',
  cc_template_id: '',
  max_rpm: 0,
  max_tpm: 0,
  max_concurrent: 0,
  max_sessions: 0,
  refresh_token: '',
  access_token: '',
  session_keys_text: '',
}

describe('supplier import input', () => {
  test('accepts OAuth login and setup-token without imported credentials', () => {
    expect(uploadFormSchema('login').safeParse(base).success).toBe(true)
    expect(uploadFormSchema('setup_token').safeParse(base).success).toBe(true)
  })
  test('requires RT and validates optional AT without imposing a token prefix', () => {
    expect(uploadFormSchema('rt').safeParse(base).success).toBe(false)
    expect(
      uploadFormSchema('rt').safeParse({
        ...base,
        refresh_token: ' synthetic-rt ',
      }).success
    ).toBe(true)
    expect(
      uploadFormSchema('rt').safeParse({
        ...base,
        refresh_token: 'synthetic-rt',
        access_token: 'synthetic-at',
      }).success
    ).toBe(true)
    for (const refresh_token of [
      'bad token',
      'bad\u0000token',
      'x'.repeat(16385),
    ]) {
      expect(
        uploadFormSchema('rt').safeParse({ ...base, refresh_token }).success
      ).toBe(false)
    }
  })
  test('splits newlines and both commas with case-sensitive deduplication', () => {
    expect(parseSessionKeys(' one\r\ntwo,one，Two\n\nlast ')).toEqual([
      'one',
      'two',
      'Two',
      'last',
    ])
  })
  test('requires 1 to 20 valid SKs and rejects oversized batches without truncation', () => {
    expect(uploadFormSchema('sk').safeParse(base).success).toBe(false)
    const twenty = Array.from({ length: 20 }, (_, i) => `synthetic-${i}`).join(
      '\n'
    )
    expect(
      uploadFormSchema('sk').safeParse({ ...base, session_keys_text: twenty })
        .success
    ).toBe(true)
    expect(
      uploadFormSchema('sk').safeParse({
        ...base,
        session_keys_text: `${twenty}\nextra`,
      }).success
    ).toBe(false)
    expect(parseSessionKeys(`${twenty}\nextra`)).toHaveLength(21)
    expect(
      uploadFormSchema('sk').safeParse({
        ...base,
        session_keys_text: 'two tokens',
      }).success
    ).toBe(false)
  })
})
