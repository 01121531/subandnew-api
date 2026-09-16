import { describe, expect, test } from 'bun:test'

import { normalSubmissionSchema } from './schemas'

describe('normal submission content', () => {
  test('accepts screenshot-only legacy input, remark-only, and both', () => {
    expect(normalSubmissionSchema.parse({ attachment_ids: ['image'] })).toEqual(
      { attachment_ids: ['image'], remark: '' }
    )
    for (const ids of [[], ['image']]) {
      expect(
        normalSubmissionSchema.parse({
          attachment_ids: ids,
          remark: ' \t\nFirst  line\n\nSecond line\u3000',
        })
      ).toEqual({ attachment_ids: ids, remark: 'First  line\n\nSecond line' })
    }
  })
  test('rejects both empty, including Unicode surrounding whitespace', () => {
    for (const remark of ['', ' \t\n\r\u3000\u00a0', undefined]) {
      expect(
        normalSubmissionSchema.safeParse({ attachment_ids: [], remark }).success
      ).toBe(false)
    }
  })
  test('counts Unicode codepoints after trimming, not UTF-16 units or graphemes', () => {
    for (const character of ['x', '\u4e2d', '\u{1F600}']) {
      const remark = character.repeat(2000)
      expect(
        normalSubmissionSchema.parse({
          attachment_ids: [],
          remark: ` \n${remark}\t `,
        }).remark
      ).toBe(remark)
      expect(
        normalSubmissionSchema.safeParse({
          attachment_ids: ['image'],
          remark: remark + character,
        }).success
      ).toBe(false)
    }
    expect(
      normalSubmissionSchema.safeParse({
        attachment_ids: [],
        remark: 'e\u0301'.repeat(1000),
      }).success
    ).toBe(true)
    expect(
      normalSubmissionSchema.safeParse({
        attachment_ids: [],
        remark: 'e\u0301'.repeat(1001),
      }).success
    ).toBe(false)
  })
  test('remarks do not bypass attachment count, completion or uniqueness validation', () => {
    expect(
      normalSubmissionSchema.safeParse({
        attachment_ids: ['1', '2', '3', '4', '5'],
        remark: '',
      }).success
    ).toBe(true)
    for (const ids of [
      [''],
      ['same', 'same'],
      ['1', '2', '3', '4', '5', '6'],
    ]) {
      expect(
        normalSubmissionSchema.safeParse({
          attachment_ids: ids,
          remark: 'Valid text',
        }).success
      ).toBe(false)
    }
  })
  test('rejects control characters even at the edges but preserves CR, newline and tabs', () => {
    const remark = 'First\tline\r\nSecond line'
    expect(
      normalSubmissionSchema.parse({ attachment_ids: [], remark }).remark
    ).toBe(remark)
    for (const code of [
      ...Array.from({ length: 32 }, (_, i) => i),
      127,
      128,
      159,
    ]) {
      if ([9, 10, 13].includes(code)) continue
      const control = String.fromCharCode(code)
      for (const text of [
        `${control}text`,
        `text${control}`,
        `left${control}right`,
      ]) {
        expect(
          normalSubmissionSchema.safeParse({ attachment_ids: [], remark: text })
            .success
        ).toBe(false)
      }
    }
  })
})
