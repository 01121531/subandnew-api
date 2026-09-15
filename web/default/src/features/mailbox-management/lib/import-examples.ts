import type { AccountType } from '../types'

export function importExample(type: AccountType, cvv = false): string {
  return [1, 2]
    .map((n) => {
      const fields = [
        `${type}-demo-${n}@example.test`,
        `Demo-Password-${n}!`,
        'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ',
      ]
      if (type === 'opening') {
        fields.push(n === 1 ? '4242424242424242' : '5555555555554444', '12/39')
        if (cvv) fields.push('012')
      }
      return fields.join('----')
    })
    .join('\n')
}
