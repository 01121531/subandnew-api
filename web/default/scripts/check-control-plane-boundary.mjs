/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import fs from 'node:fs/promises'
import path from 'node:path'
import process from 'node:process'

const ROOTS = [
  'src/features/managed-instances',
  'src/features/fleet-dashboard',
  'src/features/usage-records',
  'src/routes/_authenticated/instances',
  'src/routes/_authenticated/dashboard',
  'src/routes/_authenticated/usage-records',
]
const SOURCE_EXTENSIONS = new Set(['.ts', '.tsx'])
const FORBIDDEN_PATTERNS = [
  [/\bfetch\s*\(/, 'fetch'],
  [/\bnew\s+XMLHttpRequest\b/, 'XMLHttpRequest'],
  [/\bnew\s+WebSocket\b/, 'WebSocket'],
  [/\bnew\s+EventSource\b/, 'EventSource'],
  [/\bnavigator\.sendBeacon\b/, 'sendBeacon'],
  [/\bfrom\s+['"]axios['"]/, 'direct axios import'],
  [/href\s*=\s*\{[^}]*base_url[^}]*\}/, 'direct instance navigation'],
  [
    /[`'"]\/api\/(?:data|log|channel|v1\/admin)(?:\/|\?|[`'"])/,
    'target-native API path',
  ],
]
const API_CALL_PATTERN = /\bapi\.(?:get|post|put|patch|delete)\b/g

async function sourceFiles(root) {
  const entries = await fs.readdir(root, { withFileTypes: true })
  const files = []
  for (const entry of entries) {
    const entryPath = path.join(root, entry.name)
    if (entry.isDirectory()) {
      files.push(...(await sourceFiles(entryPath)))
    } else if (SOURCE_EXTENSIONS.has(path.extname(entry.name))) {
      files.push(entryPath)
    }
  }
  return files
}

function firstArgument(source, callEnd) {
  const openParen = source.indexOf('(', callEnd)
  if (openParen < 0 || source.slice(callEnd, openParen).includes(';')) return ''
  const argument = source.slice(openParen + 1).trimStart()
  const quote = argument[0]
  if (!['`', "'", '"'].includes(quote)) return ''
  const close = argument.indexOf(quote, 1)
  return close < 0 ? '' : argument.slice(1, close)
}

const violations = []
for (const root of ROOTS) {
  for (const file of await sourceFiles(root)) {
    const source = await fs.readFile(file, 'utf8')
    for (const [pattern, label] of FORBIDDEN_PATTERNS) {
      if (pattern.test(source)) {
        violations.push(`${file}: forbidden ${label}`)
      }
    }
    for (const match of source.matchAll(API_CALL_PATTERN)) {
      const argument = firstArgument(source, match.index + match[0].length)
      if (argument !== '/api' && !argument.startsWith('/api/')) {
        violations.push(
          `${file}: api call must use a literal /api control-plane path`
        )
      }
    }
  }
}

if (violations.length > 0) {
  console.error(violations.join('\n'))
  process.exitCode = 1
} else {
  console.log('Control-plane boundary check passed.')
}
