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
  AccountFilterTemplate,
  AccountFilterTemplateInput,
} from './account-filtering'

type ApiResponse<T> = { success: boolean; message: string; data: T }

export async function listAccountFilterTemplates() {
  const response = await api.get<ApiResponse<AccountFilterTemplate[]>>(
    '/api/managed-account-filter-templates',
    { disableDuplicate: true }
  )
  return response.data
}

export async function createAccountFilterTemplate(
  input: AccountFilterTemplateInput
) {
  const response = await api.post<ApiResponse<AccountFilterTemplate>>(
    '/api/managed-account-filter-templates',
    input,
    { disableDuplicate: true, skipErrorHandler: true }
  )
  return response.data
}

export async function updateAccountFilterTemplate(
  id: number,
  input: AccountFilterTemplateInput
) {
  const response = await api.put<ApiResponse<AccountFilterTemplate>>(
    `/api/managed-account-filter-templates/${id}`,
    input,
    { disableDuplicate: true, skipErrorHandler: true }
  )
  return response.data
}

export async function deleteAccountFilterTemplate(id: number) {
  const response = await api.delete<ApiResponse<{ id: number }>>(
    `/api/managed-account-filter-templates/${id}`,
    { disableDuplicate: true, skipErrorHandler: true }
  )
  return response.data
}
