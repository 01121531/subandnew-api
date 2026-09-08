import { createFileRoute, redirect } from '@tanstack/react-router'

import { QueryState } from '@/features/suppliers/components/common'
import { SupplierPortal } from '@/features/suppliers/portal'
import { sessionOptions, supplierClient } from '@/features/suppliers/session'

export const Route = createFileRoute('/supplier/')({
  beforeLoad: async () => {
    const session = await supplierClient.fetchQuery(sessionOptions)
    if (!session.authenticated) throw redirect({ to: '/supplier/sign-in' })
  },
  component: SupplierPortal,
  errorComponent: (props) => (
    <QueryState pending={false} error={props.error} retry={props.reset} />
  ),
})
