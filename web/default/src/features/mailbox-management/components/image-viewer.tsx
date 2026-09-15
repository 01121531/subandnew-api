import { Expand, Scan, X, ZoomIn, ZoomOut } from 'lucide-react'
import {
  useEffect,
  useRef,
  useState,
  type PointerEvent,
  type ReactNode,
  type RefObject,
} from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import {
  constrainView,
  fitScale,
  pointerPair,
  zoomView,
  type ImageView,
  type Point,
  type Size,
} from '../lib/image-view'

function ViewerButton(props: {
  label: string
  disabled?: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type='button'
            variant='ghost'
            size='icon'
            className='size-10 shrink-0'
            aria-label={props.label}
            disabled={props.disabled}
            onClick={props.onClick}
          />
        }
      >
        {props.children}
      </TooltipTrigger>
      <TooltipContent side='bottom'>{props.label}</TooltipContent>
    </Tooltip>
  )
}

export function ImageViewer(props: {
  url: string
  label: string
  returnFocus: RefObject<HTMLElement | null>
  onClose: () => void
  onError: () => void
}) {
  const { t } = useTranslation()
  const [area, setArea] = useState<HTMLDivElement | null>(null)
  const geometry = useRef({
    image: { width: 0, height: 0 },
    viewport: { width: 0, height: 0 },
  })
  const current = useRef<ImageView>({ scale: 1, x: 0, y: 0 })
  const pointers = useRef(new Map<number, Point>())
  const [view, setView] = useState(current.current)
  const [ready, setReady] = useState(false)
  const [dragging, setDragging] = useState(false)

  function update(next: ImageView) {
    const { image, viewport } = geometry.current
    current.current = constrainView(next, image, viewport)
    setView(current.current)
  }

  function fit() {
    const { image, viewport } = geometry.current
    update({ scale: fitScale(image, viewport), x: 0, y: 0 })
  }

  function zoom(scale: number, from: Point = { x: 0, y: 0 }, to = from) {
    const { image, viewport } = geometry.current
    update(zoomView(current.current, scale, from, to, image, viewport))
  }

  useEffect(() => {
    const element = area
    if (!element) return
    const observer = new ResizeObserver(([entry]) => {
      geometry.current.viewport = {
        width: entry.contentRect.width,
        height: entry.contentRect.height,
      }
      const { image, viewport } = geometry.current
      current.current = { scale: fitScale(image, viewport), x: 0, y: 0 }
      setView(current.current)
      pointers.current.clear()
      setDragging(false)
    })
    observer.observe(element)
    const wheel = (event: WheelEvent) => {
      event.preventDefault()
      if (!geometry.current.image.width) return
      const rect = element.getBoundingClientRect()
      const anchor = {
        x: event.clientX - rect.left - rect.width / 2,
        y: event.clientY - rect.top - rect.height / 2,
      }
      let delta = event.deltaY
      if (event.deltaMode === 1) delta *= 16
      if (event.deltaMode === 2) delta *= rect.height
      const { image, viewport } = geometry.current
      current.current = zoomView(
        current.current,
        current.current.scale *
          Math.exp(-Math.max(-200, Math.min(200, delta)) * 0.005),
        anchor,
        anchor,
        image,
        viewport
      )
      setView(current.current)
    }
    element.addEventListener('wheel', wheel, { passive: false })
    return () => {
      observer.disconnect()
      element.removeEventListener('wheel', wheel)
    }
  }, [area])

  function point(event: PointerEvent<HTMLDivElement>) {
    const rect = event.currentTarget.getBoundingClientRect()
    return {
      x: event.clientX - rect.left - rect.width / 2,
      y: event.clientY - rect.top - rect.height / 2,
    }
  }

  function move(event: PointerEvent<HTMLDivElement>) {
    const previous = pointers.current.get(event.pointerId)
    if (!previous) return
    const before = pointerPair([...pointers.current.values()])
    const next = point(event)
    pointers.current.set(event.pointerId, next)
    const after = pointerPair([...pointers.current.values()])
    if (before && after && before.distance > 0) {
      zoom(
        (current.current.scale * after.distance) / before.distance,
        before.center,
        after.center
      )
    } else {
      update({
        ...current.current,
        x: current.current.x + next.x - previous.x,
        y: current.current.y + next.y - previous.y,
      })
    }
  }

  function release(event: PointerEvent<HTMLDivElement>) {
    pointers.current.delete(event.pointerId)
    setDragging(pointers.current.size > 0)
  }

  function loaded(image: Size) {
    geometry.current.image = image
    setReady(true)
    fit()
  }

  const minimum = Math.min(
    fitScale(geometry.current.image, geometry.current.viewport),
    0.1
  )
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <DialogContent
        showCloseButton={false}
        finalFocus={props.returnFocus}
        aria-describedby={undefined}
        className='bg-background fixed inset-0 flex h-dvh w-screen max-w-none translate-x-0 translate-y-0 flex-col gap-0 overflow-hidden rounded-none p-0 sm:max-w-none'
        style={{ animation: 'none' }}
        data-mailbox-image-viewer
      >
        <header className='bg-background flex shrink-0 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b px-2 py-2 sm:px-4'>
          <DialogTitle className='min-w-0 break-words'>
            {props.label}
          </DialogTitle>
          <div className='flex items-center gap-0.5'>
            <output
              className='w-14 shrink-0 text-center text-xs tabular-nums'
              aria-label={t('mailbox.viewer.zoom')}
            >
              {(view.scale * 100).toFixed(1)}%
            </output>
            <ViewerButton
              label={t('mailbox.viewer.zoomOut')}
              disabled={!ready || view.scale <= minimum}
              onClick={() => zoom(current.current.scale / 1.25)}
            >
              <ZoomOut />
            </ViewerButton>
            <ViewerButton
              label={t('mailbox.viewer.zoomIn')}
              disabled={!ready || view.scale >= 8}
              onClick={() => zoom(current.current.scale * 1.25)}
            >
              <ZoomIn />
            </ViewerButton>
            <ViewerButton
              label={t('mailbox.viewer.fit')}
              disabled={!ready}
              onClick={fit}
            >
              <Expand />
            </ViewerButton>
            <ViewerButton
              label={t('mailbox.viewer.actual')}
              disabled={!ready}
              onClick={() => update({ scale: 1, x: 0, y: 0 })}
            >
              <Scan />
            </ViewerButton>
            <ViewerButton
              label={t('mailbox.admin.close')}
              onClick={props.onClose}
            >
              <X />
            </ViewerButton>
          </div>
        </header>
        <div
          ref={setArea}
          role='region'
          aria-label={t('mailbox.viewer.image')}
          tabIndex={0}
          className='bg-muted/50 focus-visible:ring-ring relative min-h-0 flex-1 touch-none overflow-hidden outline-none select-none focus-visible:ring-2 focus-visible:ring-inset'
          style={{ cursor: dragging ? 'grabbing' : 'grab' }}
          onPointerDown={(event) => {
            if (!ready || event.button !== 0 || pointers.current.size >= 2) {
              return
            }
            event.preventDefault()
            event.currentTarget.focus({ preventScroll: true })
            event.currentTarget.setPointerCapture(event.pointerId)
            pointers.current.set(event.pointerId, point(event))
            setDragging(true)
          }}
          onPointerMove={move}
          onPointerUp={release}
          onPointerCancel={release}
          onLostPointerCapture={release}
          onKeyDown={(event) => {
            if (!ready) return
            const keys: Record<string, Point> = {
              ArrowLeft: { x: 48, y: 0 },
              ArrowRight: { x: -48, y: 0 },
              ArrowUp: { x: 0, y: 48 },
              ArrowDown: { x: 0, y: -48 },
            }
            const delta = keys[event.key]
            if (delta) {
              event.preventDefault()
              update({
                ...current.current,
                x: current.current.x + delta.x,
                y: current.current.y + delta.y,
              })
            } else if (event.key === '+' || event.key === '=') {
              event.preventDefault()
              zoom(current.current.scale * 1.25)
            } else if (event.key === '-') {
              event.preventDefault()
              zoom(current.current.scale / 1.25)
            }
          }}
        >
          {!ready && (
            <div
              role='status'
              className='absolute inset-0 grid place-items-center'
            >
              {t('mailbox.admin.loading')}
            </div>
          )}
          <img
            src={props.url}
            alt={props.label}
            draggable={false}
            onLoad={(event) =>
              loaded({
                width: event.currentTarget.naturalWidth,
                height: event.currentTarget.naturalHeight,
              })
            }
            onError={props.onError}
            className='pointer-events-none absolute top-1/2 left-1/2 max-w-none'
            style={{
              width: geometry.current.image.width || undefined,
              height: geometry.current.image.height || undefined,
              visibility: ready ? 'visible' : 'hidden',
              transform: `translate(-50%, -50%) translate(${view.x}px, ${view.y}px) scale(${view.scale})`,
            }}
          />
        </div>
      </DialogContent>
    </Dialog>
  )
}
