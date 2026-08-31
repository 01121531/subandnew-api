/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import type { QueryMeta } from '@tanstack/react-query'
import { AxiosError } from 'axios'

declare module '@tanstack/react-query' {
  interface Register {
    queryMeta: {
      criticalErrorPage?: boolean
    }
  }
}

export function shouldNavigateToServerError(
  error: unknown,
  meta: QueryMeta | undefined
) {
  return (
    meta?.criticalErrorPage === true &&
    error instanceof AxiosError &&
    error.response?.status === 500
  )
}
