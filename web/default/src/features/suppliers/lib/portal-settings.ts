import { z } from 'zod'

export const uploadMethodOrder = ['login', 'setup_token', 'rt', 'sk'] as const
export function validPortalText(value: string, allowEmpty = false): boolean {
  return (
    !/\p{Cc}/u.test(value) &&
    (allowEmpty || value.trim().length > 0) &&
    [...value.trim()].length <= 64
  )
}
export const portalSettingsSchema = z.object({
  title: z
    .string()
    .refine((value) => validPortalText(value))
    .transform((value) => value.trim()),
  revision: z.number().int().positive(),
  upload_methods: z.object({
    login: z.boolean(),
    setup_token: z.boolean(),
    rt: z.boolean(),
    sk: z.boolean(),
  }),
})
