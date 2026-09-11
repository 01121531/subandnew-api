import { create } from 'zustand'
import { persist } from 'zustand/middleware'

import type { NameTimeMode } from '@/features/suppliers/types'

export interface SupplierTemplateChoice {
  policyId: string
  templateId: string
  nameTimeMode?: NameTimeMode
}

interface SupplierUploadPreferences {
  choices: Record<string, SupplierTemplateChoice>
  remember: (
    supplierId: number,
    bindingId: number,
    choice: SupplierTemplateChoice
  ) => void
}

export const useSupplierUploadPreferences = create<SupplierUploadPreferences>()(
  persist(
    (set) => ({
      choices: {},
      remember: (supplierId, bindingId, choice) =>
        set((state) => ({
          choices: {
            ...state.choices,
            [`${supplierId}:${bindingId}`]: {
              policyId: choice.policyId,
              templateId: choice.templateId,
              nameTimeMode: choice.nameTimeMode ?? 'none',
            },
          },
        })),
    }),
    {
      name: 'supplier-upload-preferences-v1',
      partialize: (state) => ({ choices: state.choices }),
    }
  )
)
