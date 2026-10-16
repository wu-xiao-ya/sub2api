import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { copyFile, mkdir, readFile, readdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { JSDOM } from 'jsdom'

const root = fileURLToPath(new URL('../..', import.meta.url))
const release = path.join(root, 'frontend-release-dist')
const embedded = path.join(root, 'backend/internal/web/dist')
const artifacts = path.join(root, 'artifacts')
const digest = buffer => createHash('sha256').update(buffer).digest('hex')
async function files(directory, prefix = '') {
  const result = []
  for (const entry of await readdir(path.join(directory, prefix), { withFileTypes: true })) {
    assert(!entry.isSymbolicLink(), 'Build assets must not be symlinks')
    const relative = path.posix.join(prefix, entry.name)
    if (entry.isDirectory()) result.push(...await files(directory, relative))
    else if (entry.isFile()) result.push(relative)
  }
  return result.sort()
}
const oldFiles = await files(embedded)
const oldHashes = {}
for (const name of oldFiles) oldHashes[name] = digest(await readFile(path.join(embedded, name)))
const newFiles = await files(release)
for (const name of newFiles) {
  const source = await readFile(path.join(release, name))
  if (name !== 'index.html' && name in oldHashes) {
    assert.equal(digest(source), oldHashes[name], `Unexpected asset overwrite: ${name}`)
  }
  await mkdir(path.dirname(path.join(embedded, name)), { recursive: true })
  await copyFile(path.join(release, name), path.join(embedded, name))
}
const merged = {}
for (const name of await files(embedded)) merged[name] = digest(await readFile(path.join(embedded, name)))
for (const name of oldFiles.filter(value => value !== 'index.html')) {
  assert.equal(merged[name], oldHashes[name], `Cached browser asset lost: ${name}`)
}
const dom = new JSDOM(await readFile(path.join(release, 'index.html'), 'utf8'))
const entryAssets = [...dom.window.document.querySelectorAll('script[src],link[href]')]
  .map(node => node.getAttribute('src') || node.getAttribute('href'))
  .filter(value => value?.startsWith('/assets/')).sort()
dom.window.close()
assert(entryAssets.length > 0)
await mkdir(artifacts, { recursive: true })
await writeFile(path.join(artifacts, 'ui-manifest.json'), JSON.stringify({
  entry_assets: entryAssets, files: merged, release_files: newFiles,
  preserved_asset_count: oldFiles.length - 1,
}, null, 2))
console.log(JSON.stringify({ releaseFiles: newFiles.length, preservedAssets: oldFiles.length - 1,
  mergedFiles: Object.keys(merged).length, entryAssets }))
