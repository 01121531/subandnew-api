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
export const FLEET_TIME_PRESETS = [
  { days: 1, label: '1 Day' },
  { days: 7, label: '7 Days' },
  { days: 14, label: '14 Days' },
  { days: 30, label: '30 Days' },
] as const

export type FleetPresetDays = (typeof FLEET_TIME_PRESETS)[number]['days']

export type FleetTimeRange = {
  start: Date
  end: Date
  presetDays: FleetPresetDays | null
}

export function createFleetPresetRange(days: FleetPresetDays): FleetTimeRange {
  const end = new Date()
  return {
    start: new Date(end.getTime() - days * 86_400_000),
    end,
    presetDays: days,
  }
}

export function resolveFleetTimeRange(range: FleetTimeRange) {
  return range.presetDays ? createFleetPresetRange(range.presetDays) : range
}
