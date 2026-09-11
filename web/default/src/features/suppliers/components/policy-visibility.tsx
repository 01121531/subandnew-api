import type { ReactNode } from 'react'

import { PolicyContext, useSupplierPolicy } from '../lib/permissions'
import type { EffectivePolicy } from '../types'

export function SupplierPolicyProvider(props: {
  policy: EffectivePolicy | undefined
  children: ReactNode
}) {
  return <PolicyContext value={props.policy}>{props.children}</PolicyContext>
}
export function Visible(props: { field: string; children: ReactNode }) {
  const allowed = useSupplierPolicy()
  return allowed(props.field) ? props.children : null
}
