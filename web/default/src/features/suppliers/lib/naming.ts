import type { NamingRule, UploadMethod } from '../types'

export const emptyNaming: NamingRule = { prefix: '', suffix: '' }
const controlCharacters = /\p{Cc}/u

export function validNamingRule(rule: NamingRule | null) {
  if (!rule) return true
  return (
    !controlCharacters.test(rule.prefix + rule.suffix) &&
    [...(rule.prefix.trim() + rule.suffix.trim())].length <= 63
  )
}

export function uploadName(rule: NamingRule, name: string) {
  return rule.prefix.trim() + name.trim() + rule.suffix.trim()
}

export function uploadNameError(
  rule: NamingRule,
  name: string,
  method: UploadMethod
) {
  if (method === 'sk' && rule.suffix.trim()) {
    return 'supplier.namingSkUnavailable'
  }
  if (!name.trim() || controlCharacters.test(name)) {
    return 'supplier.namingInvalidName'
  }
  if ([...uploadName(rule, name)].length > 64) {
    return 'supplier.namingNameTooLong'
  }
  return null
}
