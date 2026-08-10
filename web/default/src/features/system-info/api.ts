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
import { api } from '@/lib/api'

import type {
  SystemInstanceDeleteResponse,
  SystemInstanceListResponse,
  SystemUpdateCapability,
  SystemUpdateRelease,
  SystemUpdateResponse,
  SystemUpdateState,
} from './types'

export async function listSystemInstances() {
  const res = await api.get<SystemInstanceListResponse>(
    '/api/system-info/instances'
  )
  return res.data
}

export async function deleteStaleSystemInstances() {
  const res = await api.delete<SystemInstanceDeleteResponse>(
    '/api/system-info/stale-instances'
  )
  return res.data
}

export async function deleteStaleSystemInstance(nodeName: string) {
  const res = await api.delete<SystemInstanceDeleteResponse>(
    `/api/system-info/instances/${encodeURIComponent(nodeName)}`
  )
  return res.data
}

export async function getSystemUpdateCapability() {
  const res = await api.get<SystemUpdateResponse<SystemUpdateCapability>>(
    '/api/system-update/capability',
    { skipErrorHandler: true }
  )
  return res.data.data
}

export async function getLatestSystemUpdate() {
  const res = await api.get<SystemUpdateResponse<SystemUpdateRelease>>(
    '/api/system-update/latest',
    { disableDuplicate: true, skipErrorHandler: true }
  )
  return res.data.data
}

export async function getSystemUpdateStatus() {
  const res = await api.get<SystemUpdateResponse<SystemUpdateState>>(
    '/api/system-update/status',
    { disableDuplicate: true, skipErrorHandler: true }
  )
  return res.data.data
}

export async function startSystemUpdate(releaseId: number) {
  const res = await api.post<SystemUpdateResponse<SystemUpdateState>>(
    '/api/system-update',
    { release_id: releaseId },
    { skipErrorHandler: true }
  )
  return res.data.data
}
