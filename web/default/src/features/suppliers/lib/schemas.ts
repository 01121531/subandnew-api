import { z } from 'zod'

import { validPortalText } from './portal-settings'

export const loginSchema = z.object({
  username: z.string().trim().min(1),
  password: z.string().min(1),
})
export const passwordSchema = z
  .object({
    current_password: z.string().min(1),
    password: z.string().min(8),
    confirm: z.string().min(8),
  })
  .refine((v) => v.password === v.confirm, { path: ['confirm'] })
export const supplierSchema = z.object({
  name: z.string().trim().min(1),
  username: z.string().trim().min(1),
  password: z.string(),
  enabled: z.boolean(),
  view_accounts: z.boolean(),
  view_usage: z.boolean(),
  manage_proxies: z.boolean(),
  upload_accounts: z.boolean(),
})
export const supplierDefaults = {
  name: '',
  username: '',
  password: '',
  enabled: true,
  view_accounts: true,
  view_usage: true,
  manage_proxies: false,
  upload_accounts: false,
}
export const bindingSchema = z.object({
  display_name: z
    .string()
    .refine((value) => validPortalText(value, true))
    .optional(),
  instance_id: z.number().int().positive(),
  identifier: z.string().trim(),
  password: z.string(),
  enabled: z.boolean(),
})
function uploadLimit(max: number) {
  return z
    .union([z.number().int().min(0).max(max), z.literal('')])
    .transform((value, ctx) => {
      if (value === '') {
        ctx.addIssue({ code: 'custom', message: 'Required' })
        return z.NEVER
      }
      return value
    })
}
export const uploadSchema = z
  .object({
    binding_id: z.number().int().positive(),
    name: z
      .string()
      .min(1)
      .refine(
        (value) =>
          value.trim().length > 0 &&
          [...value.trim()].length <= 64 &&
          !/\p{Cc}/u.test(value)
      ),
    name_time_mode: z.enum(['none', 'date', 'date_time']).default('none'),
    outbound_proxy_mode: z.enum(['direct', 'manual', 'auto']),
    outbound_proxy_id: z.string(),
    group_ids: z.array(z.string().min(1)).max(100),
    policy_template_id: z.string(),
    cc_template_id: z.string(),
    max_rpm: uploadLimit(1_000_000),
    max_tpm: uploadLimit(1_000_000_000),
    max_concurrent: uploadLimit(100_000),
    max_sessions: uploadLimit(100_000),
  })
  .refine(
    (v) => v.outbound_proxy_mode !== 'manual' || v.outbound_proxy_id.length > 0,
    { path: ['outbound_proxy_id'] }
  )

export function safeOAuthUrl(value: string): string | null {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && !url.username && !url.password
      ? url.href
      : null
  } catch {
    return null
  }
}

export function timestampMs(value: string | number): number {
  if (typeof value === 'number') {
    return value < 100_000_000_000 ? value * 1000 : value
  }
  return Date.parse(value)
}
