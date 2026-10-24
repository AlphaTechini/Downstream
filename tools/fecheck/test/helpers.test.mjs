import { test } from 'node:test'
import assert from 'node:assert/strict'
import { rewriteImportMeta, stableName, withCap, kind } from '../helpers.mjs'

test('import.meta is rewritten to the module URL', () => {
  const url = 'http://127.0.0.1:4101/assets/index-AbCd1234.js'
  const out = rewriteImportMeta('const a = import.meta.url; const b = import.meta.resolve("./x.js"); const c = import.meta;', url)
  assert.ok(!out.includes('import.meta'))
  assert.ok(out.includes(`const a = ${JSON.stringify(url)}`))
  assert.ok(out.includes(`((s)=>new URL(s,${JSON.stringify(url)}).href)("./x.js")`))
  assert.ok(out.includes(`const c = ({url:${JSON.stringify(url)}})`))
  // The rewrite must still be valid JavaScript.
  new Function(out.replace(/const /g, 'var '))
})

test('source without import.meta is returned unchanged', () => {
  const src = 'console.log("meta")'
  assert.equal(rewriteImportMeta(src, 'http://x/a.js'), src)
})

test('content hashes are stripped from asset names', () => {
  assert.equal(stableName('http://h/assets/index-BQx1u_j8.js'), '/assets/index.js')
  assert.equal(stableName('http://h/chunk-ABCDEFGH.js'), '/chunk.js')
  assert.equal(stableName('http://h/styles.4f9a8b7c6d.css'), '/styles.css')
  // Short suffixes are not hashes.
  assert.equal(stableName('http://h/app-v2.js'), '/app-v2.js')
})

test('withCap returns true for finished work and false at the cap', async () => {
  assert.equal(await withCap(Promise.resolve(), 1000), true)
  const t0 = Date.now()
  assert.equal(await withCap(new Promise(() => {}), 50), false)
  assert.ok(Date.now() - t0 < 1000)
})

test('asset kinds come from the content type or the extension', () => {
  assert.equal(kind('text/javascript', 'http://h/a'), 'js')
  assert.equal(kind('', 'http://h/a.mjs'), 'js')
  assert.equal(kind('text/css', 'http://h/a'), 'css')
  assert.equal(kind('image/png', 'http://h/a'), 'img')
  assert.equal(kind('', 'http://h/f.woff2'), 'font')
  assert.equal(kind('application/json', 'http://h/api'), 'data')
})
