import { useCallback, useEffect, useMemo, useState } from 'react'

import { consumeControlPlaneEventStream } from '@/lib/control-plane-event-stream'

import type {
  ManagedDashboardEvent,
  ManagedDashboardSnapshotSection,
} from './types'

type StreamStatus = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'error'

const MAX_BACKOFF_MS = 30_000

export function useManagedDashboardEvents(instanceIDs: number[]) {
  const idsKey = useMemo(
    () => [...new Set(instanceIDs)].sort((a, b) => a - b).join(','),
    [instanceIDs]
  )
  const [snapshots, setSnapshots] = useState<
    Record<string, ManagedDashboardSnapshotSection>
  >({})
  const [status, setStatus] = useState<StreamStatus>('idle')
  const [topologyRevision, setTopologyRevision] = useState(0)
  const [generation, setGeneration] = useState(0)
  const reconnect = useCallback(() => setGeneration((value) => value + 1), [])

  useEffect(() => {
    if (!idsKey) {
      setStatus('idle')
      return
    }
    let disposed = false
    let attempts = 0
    let controller: AbortController | null = null
    let retryTimer: ReturnType<typeof setTimeout> | null = null

    const stop = () => {
      if (retryTimer) clearTimeout(retryTimer)
      retryTimer = null
      controller?.abort()
      controller = null
    }
    const connect = () => {
      if (disposed) return
      stop()
      controller = new AbortController()
      const currentController = controller
      setStatus(attempts ? 'reconnecting' : 'connecting')
      const query = new URLSearchParams({ ids: idsKey })
      void consumeControlPlaneEventStream(
        `/api/managed-instances/dashboard-events?${query.toString()}`,
        currentController.signal,
        (eventType, data) => {
          if (!['summary', 'status', 'topology'].includes(eventType)) return
          try {
            const event = JSON.parse(data) as ManagedDashboardEvent
            attempts = 0
            setStatus('open')
            if (event.instance_id && event.snapshot?.range.range_key) {
              const snapshot = event.snapshot
              const key = `${event.instance_id}:${snapshot.range.range_key}`
              setSnapshots((current) => ({ ...current, [key]: snapshot }))
            }
            if (event.type === 'topology') {
              setTopologyRevision((value) => value + 1)
            }
          } catch {
            // A later complete event repairs a malformed frame.
          }
        }
      ).catch(() => {
        if (disposed || currentController.signal.aborted) return
        setStatus('error')
        const delay = Math.min(MAX_BACKOFF_MS, 1000 * 2 ** attempts)
        attempts += 1
        retryTimer = setTimeout(connect, delay)
      })
    }

    connect()
    return () => {
      disposed = true
      stop()
    }
  }, [generation, idsKey])

  return { snapshots, status, topologyRevision, reconnect }
}
