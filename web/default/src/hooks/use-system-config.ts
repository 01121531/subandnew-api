/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useCallback, useEffect } from 'react'

import { DEFAULT_LOGO, DEFAULT_SYSTEM_NAME } from '@/lib/constants'
import { applyFaviconToDom } from '@/lib/dom-utils'
import {
  type SystemConfig,
  useSystemConfigStore,
} from '@/stores/system-config-store'

interface StatusApiResponse {
  success: boolean
  data?: {
    system_name?: string
    logo?: string
    footer_html?: string
  }
}

export function mapStatusDataToConfig(
  data: StatusApiResponse['data']
): Partial<SystemConfig> {
  if (!data) return {}
  return {
    systemName: data.system_name || DEFAULT_SYSTEM_NAME,
    logo: data.logo || DEFAULT_LOGO,
    footerHtml: data.footer_html,
  }
}

async function fetchSystemConfig(): Promise<Partial<SystemConfig>> {
  const response = await fetch('/api/status')
  if (!response.ok) throw new Error('Failed to fetch status')
  const result: StatusApiResponse = await response.json()
  if (!result.success) throw new Error('API returned error')
  return mapStatusDataToConfig(result.data)
}

function preloadImage(
  src: string,
  onLoad: () => void,
  onError: () => void
): () => void {
  const image = new Image()
  image.onload = onLoad
  image.onerror = onError
  image.src = src
  return () => {
    image.onload = null
    image.onerror = null
  }
}

export function useSystemConfig({ autoLoad = false } = {}) {
  const {
    config,
    loading,
    loadedLogoUrl,
    setConfig,
    setLoadedLogoUrl,
    setLoading,
  } = useSystemConfigStore()

  const loadConfig = useCallback(async () => {
    try {
      setLoading(true)
      setConfig(await fetchSystemConfig())
    } catch (error) {
      console.error('Failed to load system config:', error)
    } finally {
      setLoading(false)
    }
  }, [setConfig, setLoading])

  useEffect(() => {
    if (autoLoad) void loadConfig()
  }, [autoLoad, loadConfig])

  useEffect(() => {
    const { logo } = config
    if (!logo || logo === loadedLogoUrl) return
    return preloadImage(
      logo,
      () => {
        setLoadedLogoUrl(logo)
        applyFaviconToDom(logo)
      },
      () => setLoadedLogoUrl(logo)
    )
  }, [config, loadedLogoUrl, setLoadedLogoUrl])

  return {
    ...config,
    loading,
    logoLoaded: config.logo === loadedLogoUrl && Boolean(loadedLogoUrl),
  }
}
