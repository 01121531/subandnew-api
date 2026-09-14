import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

export function usePortalTitle(value?: string) {
  const { t } = useTranslation()
  const title = value || t('supplier.workspaceTitle')
  useEffect(() => {
    const previous = document.title
    const meta = document.querySelector('meta[name="title"]')
    const previousMeta = meta?.getAttribute('content')
    document.title = title
    meta?.setAttribute('content', title)
    return () => {
      document.title = previous
      if (previousMeta !== null && previousMeta !== undefined) {
        meta?.setAttribute('content', previousMeta)
      }
    }
  }, [title])
  return title
}
