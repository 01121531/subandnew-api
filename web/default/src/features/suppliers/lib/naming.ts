import type { NamingRule, UploadMethod, NameTimeMode } from '../types'

export const emptyNaming: NamingRule = { prefix: '', suffix: '' }
const controlCharacters = /\p{Cc}/u

export function validNamingRule(rule: NamingRule | null) {
  if (!rule) return true
  return (
    !controlCharacters.test(rule.prefix + rule.suffix) &&
    [...(rule.prefix.trim() + rule.suffix.trim())].length <= 63
  )
}

export function uploadName(
  rule: NamingRule,
  name: string,
  mode: NameTimeMode = 'none',
  at = new Date()
) {
  let suffix = ''
  if (mode !== 'none') {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone: 'Asia/Shanghai',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    }).formatToParts(at)
    const part = (type: Intl.DateTimeFormatPartTypes) =>
      parts.find((item) => item.type === type)?.value ?? ''
    suffix = `-${part('month')}${part('day')}`
    if (mode === 'date_time') suffix += `-${part('hour')}${part('minute')}`
  }
  return rule.prefix.trim() + name.trim() + suffix + rule.suffix.trim()
}

export function uploadNameError(
  rule: NamingRule,
  name: string,
  method: UploadMethod,
  mode: NameTimeMode = 'none'
) {
  if (method === 'sk' && rule.suffix.trim()) {
    return 'supplier.namingSkUnavailable'
  }
  if (!name.trim() || controlCharacters.test(name)) {
    return 'supplier.namingInvalidName'
  }
  if ([...uploadName(rule, name, mode)].length > 64) {
    return 'supplier.namingNameTooLong'
  }
  return null
}
