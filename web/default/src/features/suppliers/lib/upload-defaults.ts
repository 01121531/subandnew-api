import type { z } from 'zod'

import type { SupplierTemplateChoice } from '@/stores/supplier-upload-preferences'

import type { UploadOptions } from '../types'
import { policyValues } from './policy'
import type { uploadSchema } from './schemas'

export function uploadDefaults(
  bindingId: number,
  options: UploadOptions,
  remembered?: SupplierTemplateChoice
): z.input<typeof uploadSchema> {
  const policy =
    options.policies.find((item) => item.id === remembered?.policyId) ??
    options.policies[0]
  const template =
    options.templates.find((item) => item.id === remembered?.templateId) ??
    options.templates[0]
  const defaults: z.input<typeof uploadSchema> = {
    binding_id: bindingId,
    name: '',
    name_time_mode: ['date', 'date_time'].includes(
      remembered?.nameTimeMode ?? ''
    )
      ? remembered?.nameTimeMode
      : 'none',
    outbound_proxy_mode: 'manual',
    outbound_proxy_id: options.proxies[0]?.id ?? '',
    group_ids: [],
    policy_template_id: policy?.id ?? '',
    cc_template_id: template?.id ?? '',
    max_rpm: '',
    max_tpm: '',
    max_concurrent: '',
    max_sessions: '',
  }
  if (policy) {
    for (const [key, value] of policyValues(policy)) defaults[key] = value
  }
  return defaults
}
