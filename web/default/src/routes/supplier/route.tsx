import { createFileRoute } from '@tanstack/react-router'

import { SupplierProvider } from '@/features/suppliers/provider'

export const Route = createFileRoute('/supplier')({
  component: SupplierProvider,
})
