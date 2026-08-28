/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback, useEffect, useMemo, useState } from 'react'

import { consumeControlPlaneEventStream } from '@/lib/control-plane-event-stream'

import type { ManagedInstanceRealtimeState } from './types'

type StreamStatus = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'error'

const MAX_BACKOFF_MS = 30_000
const EMPTY_METRIC = {
  value: null,
  unit: '',
  collection_status: 'unsupported' as const,
}

type ManagedInstanceRealtimePatch = Partial<ManagedInstanceRealtimeState> &
  Pick<ManagedInstanceRealtimeState, 'instance_id'>

function initialRealtimeState(
  instanceID: number
): ManagedInstanceRealtimeState {
  return {
    instance_id: instanceID,
    observed_at: 0,
    stream_status: 'connecting',
    stale: true,
    rpm: EMPTY_METRIC,
    rpm_capacity: EMPTY_METRIC,
    success_rate: EMPTY_METRIC,
  }
}

function parseEvent(
  eventType: string,
  data: string,
  onState: (eventType: string, state: ManagedInstanceRealtimePatch) => void
) {
  if (!['rpm', 'accounts', 'sources', 'status'].includes(eventType)) return
  try {
    onState(eventType, JSON.parse(data) as ManagedInstanceRealtimePatch)
  } catch {
    // A malformed frame is isolated; the next complete state repairs the view.
  }
}

export function useManagedInstanceRealtimeEvents(
  instanceIDs: number[],
  topics: readonly string[]
) {
  const idsKey = useMemo(
    () => [...new Set(instanceIDs)].sort((a, b) => a - b).join(','),
    [instanceIDs]
  )
  const topicsKey = useMemo(
    () => [...new Set(topics)].sort().join(','),
    [topics]
  )
  const [states, setStates] = useState<
    Record<number, ManagedInstanceRealtimeState>
  >({})
  const [status, setStatus] = useState<StreamStatus>('idle')
  const [generation, setGeneration] = useState(0)
  const reconnect = useCallback(() => setGeneration((value) => value + 1), [])
  const activeStates = useMemo(() => {
    const activeIDs = new Set(
      idsKey
        .split(',')
        .filter(Boolean)
        .map((value) => Number(value))
    )
    return Object.fromEntries(
      Object.entries(states).filter(([instanceID]) =>
        activeIDs.has(Number(instanceID))
      )
    ) as Record<number, ManagedInstanceRealtimeState>
  }, [idsKey, states])

  useEffect(() => {
    if (!idsKey) {
      setStatus('idle')
      return
    }
    let disposed = false
    let controller: AbortController | null = null
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let attempts = 0

    const stop = () => {
      if (retryTimer) clearTimeout(retryTimer)
      retryTimer = null
      controller?.abort()
      controller = null
    }
    const connect = () => {
      if (disposed) return
      stop()
      const currentController = new AbortController()
      controller = currentController
      setStatus(attempts ? 'reconnecting' : 'connecting')
      const query = new URLSearchParams({ ids: idsKey, topics: topicsKey })
      void consumeControlPlaneEventStream(
        `/api/managed-instances/realtime-events?${query.toString()}`,
        currentController.signal,
        (eventType, data) =>
          parseEvent(eventType, data, (_topic, state) => {
            attempts = 0
            setStatus('open')
            setStates((current) => {
              const previous =
                current[state.instance_id] ??
                initialRealtimeState(state.instance_id)
              return {
                ...current,
                [state.instance_id]: { ...previous, ...state },
              }
            })
          })
      ).catch((error: unknown) => {
        if (disposed || currentController.signal.aborted) return
        setStatus('error')
        const delay = Math.min(MAX_BACKOFF_MS, 1000 * 2 ** attempts)
        attempts += 1
        retryTimer = setTimeout(connect, delay)
        void error
      })
    }
    connect()
    return () => {
      disposed = true
      stop()
    }
  }, [generation, idsKey, topicsKey])

  return { states: activeStates, status, reconnect }
}
