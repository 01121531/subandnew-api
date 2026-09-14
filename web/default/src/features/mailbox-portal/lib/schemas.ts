import { z } from 'zod'

export const loginSchema = z.object({
  username: z.string().trim().min(1).max(96),
  password: z.string().min(1),
})
export const passwordSchema = z
  .object({
    current_password: z.string().min(1),
    password: z.string().refine((value) => {
      const size = new TextEncoder().encode(value).length
      return value.trim().length > 0 && size >= 8 && size <= 72
    }, 'mailboxPortal.passwordInvalid'),
    confirm: z.string(),
  })
  .refine((data) => data.password === data.confirm, {
    path: ['confirm'],
    message: 'mailboxPortal.passwordMismatch',
  })
