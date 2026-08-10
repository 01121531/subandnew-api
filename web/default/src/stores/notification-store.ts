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
import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface NotificationState {
  readAnnouncementKeys: string[]
  markAnnouncementsRead: (keys: string[]) => void
  isAnnouncementRead: (key: string) => boolean
}

export const useNotificationStore = create<NotificationState>()(
  persist(
    (set, get) => ({
      readAnnouncementKeys: [],
      markAnnouncementsRead: (keys) => {
        set((state) => ({
          readAnnouncementKeys: [
            ...new Set([...state.readAnnouncementKeys, ...keys]),
          ],
        }))
      },
      isAnnouncementRead: (key) => get().readAnnouncementKeys.includes(key),
    }),
    {
      name: 'notification-storage',
      partialize: (state) => ({
        readAnnouncementKeys: state.readAnnouncementKeys,
      }),
    }
  )
)
