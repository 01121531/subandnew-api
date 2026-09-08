import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export interface SupplierTemplateChoice {
  policyId: string
  templateId: string
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
