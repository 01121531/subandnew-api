import { describe, expect, test } from 'bun:test'

import { constrainView, fitScale, pointerPair, zoomView } from './image-view'

describe('private screenshot geometry', () => {
  const image = { width: 2400, height: 4000 }
  const viewport = { width: 390, height: 780 }

  test('fits tall, wide and small images without cropping or upscaling', () => {
    expect(fitScale(image, viewport)).toBe(0.1625)
    expect(fitScale({ width: 4000, height: 1000 }, viewport)).toBe(0.0975)
    expect(fitScale({ width: 100, height: 50 }, viewport)).toBe(1)
    expect(fitScale({ width: 0, height: 0 }, viewport)).toBe(1)
  })

  test('fit includes very long screenshots below 10% scale', () => {
    const long = { width: 2400, height: 20000 }
    const scale = fitScale(long, viewport)
    expect(constrainView({ scale, x: 50, y: 50 }, long, viewport)).toEqual({
      scale: 0.039,
      x: 0,
      y: 0,
    })
  })

  test('100% uses natural dimensions and panning reaches each edge', () => {
    expect(
      constrainView({ scale: 1, x: 99999, y: -99999 }, image, viewport)
    ).toEqual({ scale: 1, x: 1005, y: -1610 })
    expect(
      constrainView({ scale: 0.1, x: 100, y: 100 }, image, viewport)
    ).toEqual({ scale: 0.1, x: 0, y: 0 })
  })

  test('zoom is bounded and keeps the cursor pixel stationary', () => {
    const anchor = { x: 70, y: -100 }
    const initial = { scale: 1, x: 20, y: 40 }
    const result = zoomView(initial, 2, anchor, anchor, image, viewport)
    expect(result).toEqual({ scale: 2, x: -30, y: 180 })
    expect((anchor.x - result.x) / result.scale).toBe(
      (anchor.x - initial.x) / initial.scale
    )
    expect(
      constrainView({ scale: 100, x: 0, y: 0 }, image, viewport).scale
    ).toBe(8)
    expect(constrainView({ scale: 0, x: 0, y: 0 }, image, viewport).scale).toBe(
      0.1
    )
  })

  test('pinch uses distance and moving midpoint, leaving one finger safe to pan', () => {
    const before = pointerPair([
      { x: -50, y: 0 },
      { x: 50, y: 0 },
    ])
    const after = pointerPair([
      { x: -70, y: 30 },
      { x: 130, y: 30 },
    ])
    if (!before || !after) throw new Error('Expected two pointer pairs')
    const result = zoomView(
      { scale: 1, x: 0, y: 0 },
      after.distance / before.distance,
      before.center,
      after.center,
      image,
      viewport
    )
    expect(result).toEqual({ scale: 2, x: 30, y: 30 })
    expect(pointerPair([{ x: 10, y: 20 }])).toBeUndefined()
    expect(pointerPair([])).toBeUndefined()
  })
})
