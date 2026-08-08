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
