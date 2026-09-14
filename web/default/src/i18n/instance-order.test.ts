import { expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'

import { createInstance } from 'i18next'
import ts from 'typescript'

import { instanceOrderEn, instanceOrderZh } from './instance-order'

test('every ordering label has English and Chinese runtime resources', async () => {
  expect(Object.keys(instanceOrderZh).sort()).toEqual(
    Object.keys(instanceOrderEn).sort()
  )
  const i18n = createInstance()
  await i18n.init({
    lng: 'zhCN',
    fallbackLng: 'en',
    nsSeparator: false,
    resources: {
      en: { translation: { instanceOrder: instanceOrderEn } },
      zhCN: { translation: { instanceOrder: instanceOrderZh } },
    },
    interpolation: { escapeValue: false },
  })
  for (const key of Object.keys(instanceOrderZh)) {
    expect(i18n.t(`instanceOrder.${key}`)).toMatch(/[\u3400-\u9fff]/)
    expect(i18n.t(`instanceOrder.${key}`, { lng: 'en' })).not.toContain(
      'instanceOrder.'
    )
  }
  expect(i18n.t('instanceOrder.count', { count: 3 })).toBe('3 个实例')
  expect(i18n.t('instanceOrder.count', { lng: 'en', count: 1 })).toBe(
    '1 instance'
  )
  expect(i18n.t('instanceOrder.count', { lng: 'en', count: 3 })).toBe(
    '3 instances'
  )
  expect(i18n.t('instanceOrder.moveUp', { name: '生产 A' })).toBe('上移 生产 A')
  expect(i18n.t('instanceOrder.moveDown', { name: '生产 A' })).toBe(
    '下移 生产 A'
  )
  expect(i18n.t('instanceOrder.moved', { name: '生产 A', position: 2 })).toBe(
    '已将 生产 A 移至第 2 位'
  )
})

test('ordering components reference translated keys, including accessible labels', () => {
  const used = new Set<string>()
  for (const file of [
    'index.tsx',
    'components/instance-order-sheet.tsx',
    'components/instance-order-row.tsx',
  ]) {
    const source = ts.createSourceFile(
      file,
      readFileSync(
        new URL(`../features/managed-instances/${file}`, import.meta.url),
        'utf8'
      ),
      ts.ScriptTarget.Latest,
      true
    )
    const visit = (node: ts.Node) => {
      if (
        ts.isCallExpression(node) &&
        ts.isIdentifier(node.expression) &&
        node.expression.text === 't'
      ) {
        const argument = node.arguments[0]
        if (argument && ts.isStringLiteral(argument)) {
          const key = argument.text
          if (file !== 'index.tsx') {
            expect(key.startsWith('instanceOrder.')).toBe(true)
          }
          if (key.startsWith('instanceOrder.')) {
            const name = key.slice('instanceOrder.'.length)
            expect(instanceOrderEn).toHaveProperty(name)
            expect(instanceOrderZh).toHaveProperty(name)
            used.add(name)
          }
        }
      }
      ts.forEachChild(node, visit)
    }
    visit(source)
  }
  expect([...used].sort()).toEqual(
    Object.keys(instanceOrderEn)
      .filter((key) => !['count_one', 'count_other'].includes(key))
      .sort()
  )
})
