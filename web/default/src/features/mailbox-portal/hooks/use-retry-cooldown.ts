import { useEffect, useState } from 'react'

import { MailboxRequestError } from '../api'

export function useRetryCooldown(error: unknown) {
  const until =
    error instanceof MailboxRequestError && error.status === 429
      ? error.retryAt
      : 0
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    setNow(Date.now())
    if (until <= Date.now()) return
    const timer = setInterval(() => {
      setNow(Date.now())
      if (Date.now() >= until) clearInterval(timer)
    }, 1000)
    return () => clearInterval(timer)
  }, [until])
  return Math.max(0, Math.ceil((until - now) / 1000))
}
