/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useState } from 'react'

import { getManagedAccountSnapshot } from '@/features/managed-instances/api'
import type { ManagedAccountRangeInput } from '@/features/managed-instances/types'

const SNAPSHOT_CONCURRENCY = 6

type SnapshotResponse = Awaited<ReturnType<typeof getManagedAccountSnapshot>>

type SnapshotState = {
  data?: SnapshotResponse
  error?: unknown
  isPending: boolean
  isFetching: boolean
  isError: boolean
}

export type BatchedAccountSnapshotQuery = SnapshotState & {
  refetch: () => Promise<{ isError: boolean }>
}

function idleState(data?: SnapshotResponse): SnapshotState {
  return {
    data,
    isPending: data == null,
    isFetching: false,
    isError: false,
  }
}

export function useBatchedAccountSnapshots(
  instanceIDs: number[],
  rangeKey: string,
  range: ManagedAccountRangeInput
): BatchedAccountSnapshotQuery[] {
  const queryClient = useQueryClient()
  const stableIDs = useMemo(() => instanceIDs.join(','), [instanceIDs])
  const [states, setStates] = useState<SnapshotState[]>([])
  const queryKey = useCallback(
    (instanceID: number) =>
      ['managed-account-snapshot', instanceID, rangeKey] as const,
    [rangeKey]
  )

  const fetchAt = useCallback(
    async (
      index: number,
      instanceID: number,
      signal?: AbortSignal,
      force = false
    ) => {
      setStates((current) =>
        current.map((state, stateIndex) =>
          stateIndex === index ? { ...state, isFetching: true } : state
        )
      )
      try {
        const data = await queryClient.fetchQuery({
          queryKey: queryKey(instanceID),
          queryFn: ({ signal: querySignal }) =>
            getManagedAccountSnapshot(instanceID, range, {
              silent: true,
              signal: signal ?? querySignal,
            }),
          retry: 1,
          staleTime: force ? 0 : Number.POSITIVE_INFINITY,
        })
        setStates((current) =>
          current.map((state, stateIndex) =>
            stateIndex === index
              ? {
                  data,
                  isPending: false,
                  isFetching: false,
                  isError: false,
                }
              : state
          )
        )
        return { isError: false }
      } catch (error) {
        if (signal?.aborted) return { isError: false }
        setStates((current) =>
          current.map((state, stateIndex) =>
            stateIndex === index
              ? {
                  ...state,
                  error,
                  isPending: false,
                  isFetching: false,
                  isError: true,
                }
              : state
          )
        )
        return { isError: true }
      }
    },
    [queryClient, queryKey, range]
  )

  useEffect(() => {
    const ids = stableIDs ? stableIDs.split(',').map(Number) : []
    const controller = new AbortController()
    setStates(
      ids.map((instanceID) =>
        idleState(
          queryClient.getQueryData<SnapshotResponse>(queryKey(instanceID))
        )
      )
    )

    let cursor = 0
    const worker = async () => {
      while (cursor < ids.length && !controller.signal.aborted) {
        const index = cursor++
        await fetchAt(index, ids[index], controller.signal)
      }
    }
    void Promise.all(
      Array.from({ length: Math.min(SNAPSHOT_CONCURRENCY, ids.length) }, worker)
    )
    return () => controller.abort()
  }, [fetchAt, queryClient, queryKey, stableIDs])

  return instanceIDs.map((instanceID, index) => ({
    ...(states[index] ?? idleState()),
    refetch: () => fetchAt(index, instanceID, undefined, true),
  }))
}
