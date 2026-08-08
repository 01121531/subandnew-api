import { useMemo, useState } from 'react'

import { useStatus } from '@/hooks/use-status'
import { useNotificationStore } from '@/stores/notification-store'

function hashString(input: string): string {
  let hash = 0
  for (let index = 0; index < input.length; index += 1) {
    hash = (hash << 5) - hash + input.charCodeAt(index)
    hash |= 0
  }
  return hash.toString(36)
}

function announcementKey(item: Record<string, unknown>): string {
  if (item.id !== undefined && item.id !== null) return `id:${item.id}`
  return `hash:${hashString(JSON.stringify(item))}`
}

export function useNotifications() {
  const [popoverOpen, setPopoverOpen] = useState(false)
  const { status, loading } = useStatus()
  const announcements = useMemo(
    () =>
      status?.announcements_enabled
        ? ((status.announcements || []) as Record<string, unknown>[]).slice(
            0,
            20
          )
        : [],
    [status]
  )
  const { markAnnouncementsRead, isAnnouncementRead } = useNotificationStore()
  const unreadCount = useMemo(
    () =>
      announcements.filter((item) => !isAnnouncementRead(announcementKey(item)))
        .length,
    [announcements, isAnnouncementRead]
  )
  const setOpen = (open: boolean) => {
    if (open) {
      markAnnouncementsRead(announcements.map(announcementKey))
    }
    setPopoverOpen(open)
  }
  return {
    announcements,
    loading,
    unreadCount,
    popoverOpen,
    setPopoverOpen: setOpen,
  }
}
