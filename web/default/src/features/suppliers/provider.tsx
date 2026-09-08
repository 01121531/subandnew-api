import { QueryClientProvider } from '@tanstack/react-query'
import { Outlet } from '@tanstack/react-router'

import { supplierClient } from './session'

export function SupplierProvider() {
  return (
    <QueryClientProvider client={supplierClient}>
      <Outlet />
    </QueryClientProvider>
  )
}
