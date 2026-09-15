export type Point = { x: number; y: number }
export type Size = { width: number; height: number }
export type ImageView = Point & { scale: number }

export function fitScale(image: Size, viewport: Size) {
  if (!image.width || !image.height || !viewport.width || !viewport.height) {
    return 1
  }
  return Math.min(
    1,
    viewport.width / image.width,
    viewport.height / image.height
  )
}

export function constrainView(
  view: ImageView,
  image: Size,
  viewport: Size
): ImageView {
  const scale = Math.max(
    Math.min(fitScale(image, viewport), 0.1),
    Math.min(8, view.scale)
  )
  const maxX = Math.max(0, (image.width * scale - viewport.width) / 2)
  const maxY = Math.max(0, (image.height * scale - viewport.height) / 2)
  return {
    scale,
    x: Math.max(-maxX, Math.min(maxX, view.x)),
    y: Math.max(-maxY, Math.min(maxY, view.y)),
  }
}

// Anchors are relative to the viewport center, so zoom keeps the inspected pixel in place.
export function zoomView(
  view: ImageView,
  scale: number,
  from: Point,
  to: Point,
  image: Size,
  viewport: Size
): ImageView {
  const next = constrainView({ ...view, scale }, image, viewport)
  const ratio = next.scale / view.scale
  return constrainView(
    {
      scale: next.scale,
      x: to.x - (from.x - view.x) * ratio,
      y: to.y - (from.y - view.y) * ratio,
    },
    image,
    viewport
  )
}

export function pointerPair(points: Point[]) {
  const [first, second] = points
  if (!first || !second) return undefined
  return {
    center: { x: (first.x + second.x) / 2, y: (first.y + second.y) / 2 },
    distance: Math.hypot(second.x - first.x, second.y - first.y),
  }
}
