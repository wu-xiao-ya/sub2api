import { readdir, readFile } from 'node:fs/promises'
import { extname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('../src/i18n/locales/zh', import.meta.url))
const suspiciousQuestionRun = /\?{3,}/

async function collectFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  const files = []
  for (const entry of entries) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      files.push(...await collectFiles(path))
    } else if (extname(entry.name) === '.ts') {
      files.push(path)
    }
  }
  return files
}

const failures = []
for (const path of await collectFiles(root)) {
  const content = await readFile(path, 'utf8')
  if (content.includes('\uFFFD') || suspiciousQuestionRun.test(content)) {
    failures.push(relative(root, path))
  }
}

if (failures.length > 0) {
  console.error(`Chinese locale encoding check failed: ${failures.join(', ')}`)
  process.exit(1)
}
