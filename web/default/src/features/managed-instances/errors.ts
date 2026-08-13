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
const INSTANCE_CONNECTION_FAILED = 'instance_connection_failed'
const REMOTE_DATA_UNAVAILABLE = 'remote_data_unavailable'

function isConnectionMessage(message: unknown) {
  return (
    message === INSTANCE_CONNECTION_FAILED ||
    message === REMOTE_DATA_UNAVAILABLE
  )
}

export function isInstanceConnectionError(error: unknown) {
  if (error && typeof error === 'object' && 'response' in error) {
    const response = error.response
    if (response && typeof response === 'object' && 'data' in response) {
      const data = response.data
      if (data && typeof data === 'object' && 'message' in data) {
        return isConnectionMessage(data.message)
      }
    }
  }
  return error instanceof Error && isConnectionMessage(error.message)
}
