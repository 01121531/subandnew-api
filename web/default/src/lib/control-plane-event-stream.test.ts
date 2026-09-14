import { afterEach, expect, spyOn, test } from 'bun:test'

import { useAuthStore } from '@/stores/auth-store'

import { consumeControlPlaneEventStream } from './control-plane-event-stream'

let request: ReturnType<typeof spyOn<typeof globalThis, 'fetch'>> | undefined
afterEach(() => {
  request?.mockRestore()
  useAuthStore.getState().auth.reset()
})

test('SSE revocation signs out and suppresses subsequent private frames', async () => {
  useAuthStore.getState().auth.setUser({ id: 2, username: 'admin', role: 10 })
  const encoder = new TextEncoder()
  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode('event: authorization_revoked\r'))
      controller.enqueue(
        encoder.encode(
          '\ndata: {}\r\n\r\nevent: rpm\r\ndata: {"amount":999}\r\n\r\n'
        )
      )
      controller.close()
    },
  })
  request = spyOn(globalThis, 'fetch').mockResolvedValue(new Response(stream))
  const events: string[] = []
  await consumeControlPlaneEventStream(
    '/api/events',
    new AbortController().signal,
    (type) => events.push(type)
  )
  expect(useAuthStore.getState().auth.user).toBeNull()
  expect(events).toEqual([])
})

test('normal SSE disconnect reports a reconnectable failure', async () => {
  request = spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response('event: rpm\ndata: {}\n\n')
  )
  const events: string[] = []
  await expect(
    consumeControlPlaneEventStream(
      '/api/events',
      new AbortController().signal,
      (type) => events.push(type)
    )
  ).rejects.toThrow('Event stream disconnected')
  expect(events).toEqual(['rpm'])
})
