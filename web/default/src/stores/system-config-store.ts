/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { create } from 'zustand'
import { persist } from 'zustand/middleware'

import { DEFAULT_LOGO, DEFAULT_SYSTEM_NAME } from '@/lib/constants'

export interface SystemConfig {
  systemName: string
  logo: string
  footerHtml?: string
}

interface SystemConfigState {
  config: SystemConfig
  loading: boolean
  loadedLogoUrl: string
  setConfig: (config: Partial<SystemConfig>) => void
  setLoadedLogoUrl: (url: string) => void
  setLoading: (loading: boolean) => void
}

export const useSystemConfigStore = create<SystemConfigState>()(
  persist(
    (set) => ({
      config: { systemName: DEFAULT_SYSTEM_NAME, logo: DEFAULT_LOGO },
      loading: true,
      loadedLogoUrl: DEFAULT_LOGO,
      setConfig: (newConfig) =>
        set((state) => ({ config: { ...state.config, ...newConfig } })),
      setLoadedLogoUrl: (url) => set({ loadedLogoUrl: url }),
      setLoading: (loading) => set({ loading }),
    }),
    {
      name: 'system-config-storage',
      partialize: (state) => ({
        config: state.config,
        loadedLogoUrl: state.loadedLogoUrl,
      }),
    }
  )
)
