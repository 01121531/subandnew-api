import { describe, expect, test } from 'bun:test'

import { pastedImages, readClipboardImages } from './clipboard-images'
import { validateImages } from './guards'

const png = new File(['test-image'], 'screenshot.png', { type: 'image/png' })

describe('operator screenshot clipboard', () => {
  test('paste reads image files once without parsing HTML or plain text', () => {
    const data = {
      items: [
        { kind: 'string', type: 'text/html', getAsFile: () => null },
        { kind: 'file', type: 'image/png', getAsFile: () => png },
      ],
      files: [png],
    } as unknown as DataTransfer
    expect(pastedImages(data)).toEqual([png])
    expect(
      pastedImages({ items: [], files: [png] } as unknown as DataTransfer)
    ).toEqual([png])
    expect(
      pastedImages({ items: [], files: [] } as unknown as DataTransfer)
    ).toEqual([])
  })
  test('button reads one image representation per item and ignores text', async () => {
    const requested: string[] = []
    const files = await readClipboardImages(
      async () =>
        [
          {
            types: ['text/html', 'image/jpeg', 'image/png'],
            getType: async (type: string) => {
              requested.push(type)
              return png
            },
          },
          {
            types: ['text/plain'],
            getType: async () => {
              throw new Error('Do not read text')
            },
          },
        ] as unknown as ClipboardItem[]
    )
    expect(requested).toEqual(['image/png'])
    expect(files).toHaveLength(1)
    expect(files[0].type).toBe('image/png')
    expect(files[0].size).toBe(png.size)
    validateImages(files, 4)
  })
  test('empty and denied clipboard are distinguishable', async () => {
    expect(await readClipboardImages(async () => [])).toEqual([])
    await expect(
      readClipboardImages(async () => {
        throw new Error('denied')
      })
    ).rejects.toThrow('denied')
  })
  test('pasted images use existing type, byte and combined count limits', async () => {
    expect(() => validateImages([png], 5)).toThrow('mailbox_attachment_count')
    expect(() => validateImages([png, png], 4)).toThrow(
      'mailbox_attachment_count'
    )
    const svg = await readClipboardImages(
      async () =>
        [
          {
            types: ['image/svg+xml'],
            getType: async () => new Blob(['<svg/>']),
          },
        ] as unknown as ClipboardItem[]
    )
    expect(() => validateImages(svg, 0)).toThrow('mailbox_invalid_image')
    expect(() =>
      validateImages(
        [
          new File([new Uint8Array(10 * 1024 * 1024 + 1)], 'large.png', {
            type: 'image/png',
          }),
        ],
        0
      )
    ).toThrow('mailbox_attachment_too_large')
  })
})
