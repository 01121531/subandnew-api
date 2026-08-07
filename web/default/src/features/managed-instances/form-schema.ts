import { z } from 'zod'

export const managedInstanceFormSchema = z.object({
  name: z.string().trim().min(1),
  kind: z.enum(['new_api', 'huichuan', 'sub2api', 'generic']),
  base_url: z.url(),
  environment: z.enum(['production', 'staging', 'development']),
  management_mode: z.enum(['observe', 'operate', 'enforce']),
  tls_verify: z.boolean(),
  request_timeout_seconds: z.number().int().min(1).max(120),
  check_interval_seconds: z.number().int().min(10).max(86400),
  labels: z.string(),
  auth_type: z.string(),
  secret: z.string(),
  user_id: z.string(),
})

export type ManagedInstanceFormValues = z.infer<
  typeof managedInstanceFormSchema
>
