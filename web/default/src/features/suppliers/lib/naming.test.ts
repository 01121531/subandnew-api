import { describe, expect, test } from 'bun:test'

import {
  emptyNaming,
  uploadName,
  uploadNameError,
  validNamingRule,
} from './naming'

describe('supplier upload naming', () => {
  test('time marker uses China time before the fixed suffix and counts toward the limit', () => {
    const rule = { prefix: 'V-', suffix: '-S' }
    const at = new Date('2026-09-11T09:12:00Z')
    expect(uploadName(rule, '账号', 'none', at)).toBe('V-账号-S')
    expect(uploadName(rule, '账号', 'date', at)).toBe('V-账号-0911-S')
    expect(uploadName(rule, '账号', 'date_time', at)).toBe('V-账号-0911-1712-S')
    expect(
      uploadName(rule, '账号', 'date_time', new Date('2026-12-31T16:00:00Z'))
    ).toBe('V-账号-0101-0000-S')
    expect(uploadNameError(rule, '中'.repeat(50), 'rt', 'date_time')).toBeNull()
    expect(uploadNameError(rule, '中'.repeat(51), 'rt', 'date_time')).toBe(
      'supplier.namingNameTooLong'
    )
  })
  test('inheritance, explicit empty, whitespace and Unicode boundaries', () => {
    expect(validNamingRule(null)).toBe(true)
    expect(validNamingRule(emptyNaming)).toBe(true)
    expect(validNamingRule({ prefix: ' 中- ', suffix: ' -S ' })).toBe(true)
    expect(validNamingRule({ prefix: '中'.repeat(63), suffix: '' })).toBe(true)
    expect(validNamingRule({ prefix: '中'.repeat(63), suffix: 'S' })).toBe(
      false
    )
    expect(validNamingRule({ prefix: 'name\n', suffix: '' })).toBe(false)
    expect(uploadName({ prefix: ' P- ', suffix: ' -S ' }, ' P-name-S ')).toBe(
      'P-P-name-S-S'
    )
  })
  test('names are not truncated and SK cannot ignore a fixed suffix', () => {
    const rule = { prefix: '中'.repeat(63), suffix: '' }
    expect(uploadNameError(rule, '文', 'login')).toBeNull()
    expect(uploadNameError(rule, '文字', 'rt')).toBe(
      'supplier.namingNameTooLong'
    )
    expect(uploadNameError(emptyNaming, 'name\n', 'setup_token')).toBe(
      'supplier.namingInvalidName'
    )
    expect(uploadNameError(emptyNaming, '  ', 'login')).toBe(
      'supplier.namingInvalidName'
    )
    expect(uploadNameError({ prefix: 'P-', suffix: '-S' }, 'name', 'sk')).toBe(
      'supplier.namingSkUnavailable'
    )
    expect(
      uploadNameError({ prefix: 'P-', suffix: '' }, 'name', 'sk')
    ).toBeNull()
  })
})
