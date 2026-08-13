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
import type { ManagedInstance } from '@/features/managed-instances/types'

export type UsageSystem = 'new_api' | 'sub2api' | 'conductor'

export type UsageRecord = Record<string, unknown> & { id?: number }

export interface UsageRecordPage {
  source_instance_id: number
  kind: ManagedInstance['kind']
  items: UsageRecord[]
  total: number
  page: number
  page_size: number
}

export interface UsageRecordSummary {
  source_instance_id: number
  kind: ManagedInstance['kind']
  total_tokens: number
  amount: number
  currency: 'USD' | 'quota'
}

export type UsageRecordFilterValue = string | string[]
export type UsageRecordFilters = Record<string, UsageRecordFilterValue>

export type UsageRecordFilterOption = {
  value: string
  label: string
}

export interface UsageRecordFilterOptions {
  source_instance_id: number
  kind: ManagedInstance['kind']
  fields: Record<string, UsageRecordFilterOption[]>
}
