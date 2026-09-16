import { z } from 'zod'

export const normalSubmissionSchema = z
  .object({
    attachment_ids: z
      .array(z.string().min(1, 'mailbox_attachment_count'))
      .max(5, 'mailbox_attachment_count')
      .refine(
        (ids) => new Set(ids).size === ids.length,
        'mailbox_attachment_count'
      ),
    remark: z
      .string()
      .refine(
        (value) => !/\p{Cc}/u.test(value.replaceAll(/[\t\n\r]/g, '')),
        'mailbox_remark_invalid'
      )
      .trim()
      .refine((value) => [...value].length <= 2000, 'mailbox_remark_too_long')
      .default(''),
  })
  .refine(
    (value) => value.attachment_ids.length > 0 || value.remark.length > 0,
    {
      message: 'mailbox_submission_empty',
    }
  )

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
