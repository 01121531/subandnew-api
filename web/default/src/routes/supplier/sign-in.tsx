import { createFileRoute } from '@tanstack/react-router'

import { SupplierSignIn } from '@/features/suppliers/sign-in'

export const Route = createFileRoute('/supplier/sign-in')({
  component: SupplierSignIn,
})
