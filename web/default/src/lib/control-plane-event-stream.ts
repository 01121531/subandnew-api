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
import { useAuthStore } from '@/stores/auth-store'

function emitEventBlock(
  block: string,
  onEvent: (eventType: string, data: string) => void
) {
  let eventType = ''
  const data: string[] = []
  for (const line of block.split('\n')) {
    if (line.startsWith('event:')) eventType = line.slice(6).trim()
    if (line.startsWith('data:')) data.push(line.slice(5).trimStart())
  }
  if (eventType && data.length) onEvent(eventType, data.join('\n'))
}

export async function consumeControlPlaneEventStream(
  url: string,
  signal: AbortSignal,
  onEvent: (eventType: string, data: string) => void
) {
  const response = await fetch(url, {
    credentials: 'include',
    headers: { Accept: 'text/event-stream' },
    signal,
  })
  if (response.status === 401) {
    useAuthStore.getState().auth.reset()
  }
  if (!response.ok || !response.body) {
    throw new Error(`HTTP ${response.status}`)
  }
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true }).replaceAll('\r\n', '\n')
    const blocks = buffer.split('\n\n')
    buffer = blocks.pop() ?? ''
    for (const block of blocks) emitEventBlock(block, onEvent)
  }
  buffer += decoder.decode().replaceAll('\r\n', '\n')
  if (buffer.trim()) emitEventBlock(buffer, onEvent)
}
