/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import fs from 'node:fs/promises'
import path from 'node:path'

const WEB_SOURCE = path.resolve('src')
const GO_SOURCE = path.resolve('../..')
const LOCALES = path.join(WEB_SOURCE, 'i18n', 'locales')
const SOURCE_EXTENSIONS = new Set(['.go', '.ts', '.tsx', '.js', '.jsx'])

async function collectSourceFiles(root, excluded = new Set()) {
  const files = []
  for (const entry of await fs.readdir(root, { withFileTypes: true })) {
    if (excluded.has(entry.name)) continue
    const fullPath = path.join(root, entry.name)
    if (entry.isDirectory()) {
      files.push(...(await collectSourceFiles(fullPath, excluded)))
    } else if (SOURCE_EXTENSIONS.has(path.extname(entry.name))) {
      files.push(fullPath)
    }
  }
  return files
}

async function main() {
  const webFiles = await collectSourceFiles(WEB_SOURCE, new Set(['i18n']))
  const goFiles = (
    await collectSourceFiles(
      GO_SOURCE,
      new Set(['.git', 'bin', 'data', 'node_modules', 'web'])
    )
  ).filter((file) => path.extname(file) === '.go')
  const corpus = (
    await Promise.all(
      [...webFiles, ...goFiles].map((file) => fs.readFile(file, 'utf8'))
    )
  ).join('\n')

  const localeFiles = (await fs.readdir(LOCALES))
    .filter((name) => name.endsWith('.json'))
    .sort()
  let kept = 0
  let removed = 0

  for (const filename of localeFiles) {
    const fullPath = path.join(LOCALES, filename)
    const locale = JSON.parse(await fs.readFile(fullPath, 'utf8'))
    const translations = locale.translation ?? {}
    const filtered = {}

    for (const [key, value] of Object.entries(translations)) {
      if (corpus.includes(key)) {
        filtered[key] = value
        kept += 1
      } else {
        removed += 1
      }
    }

    await fs.writeFile(
      fullPath,
      `${JSON.stringify({ translation: filtered }, null, 2)}\n`,
      'utf8'
    )
  }

  console.log(`i18n prune complete: kept ${kept}, removed ${removed}`)
}

main().catch((error) => {
  console.error(error)
  process.exitCode = 1
})
