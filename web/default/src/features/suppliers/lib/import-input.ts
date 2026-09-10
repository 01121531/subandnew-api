import { z } from 'zod'

import type { UploadMethod } from '../types'
import { uploadSchema } from './schemas'

export function parseSessionKeys(text: string): string[] {
  return [
    ...new Set(
      text
        .split(/[\r\n,，]+/)
        .map((value) => value.trim())
        .filter(Boolean)
    ),
  ]
}

function validSecret(value: string) {
  return (
    value.length > 0 &&
    new TextEncoder().encode(value).length <= 16384 &&
    !/[\s\p{Cc}]/u.test(value)
  )
}

export function uploadFormSchema(method: UploadMethod) {
  return uploadSchema
    .safeExtend({
      refresh_token: z.string(),
      access_token: z.string(),
      session_keys_text: z.string(),
    })
    .superRefine((values, ctx) => {
      if (method === 'rt') {
        if (!validSecret(values.refresh_token.trim())) {
          ctx.addIssue({
            code: 'custom',
            path: ['refresh_token'],
            message: 'Invalid token',
          })
        }
        if (
          values.access_token.trim() &&
          !validSecret(values.access_token.trim())
        ) {
          ctx.addIssue({
            code: 'custom',
            path: ['access_token'],
            message: 'Invalid token',
          })
        }
      }
      if (method === 'sk') {
        const keys = parseSessionKeys(values.session_keys_text)
        if (
          keys.length === 0 ||
          keys.length > 20 ||
          keys.some((key) => !validSecret(key))
        ) {
          ctx.addIssue({
            code: 'custom',
            path: ['session_keys_text'],
            message: 'Invalid session keys',
          })
        }
      }
    })
}
