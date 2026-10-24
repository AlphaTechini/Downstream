import { test } from 'node:test'
import assert from 'node:assert/strict'
import { attribute, bytesVerdict, domVerdict, compareRuns } from '../compare.mjs'
import { stableName } from '../helpers.mjs'

const req = (path, gzip) => {
  const url = 'http://h' + path
  return { url, name: stableName(url), gzip }
}

test('byte growth needs both 5% and 1024 bytes', () => {
  assert.equal(bytesVerdict(100000, 106000).verdict, 'regression')
  assert.equal(bytesVerdict(100000, 104000).verdict, 'unchanged') // 4%
  assert.equal(bytesVerdict(10000, 11000).verdict, 'unchanged') // 10% but 1000 bytes
  assert.equal(bytesVerdict(100000, 90000).verdict, 'improved')
  assert.equal(bytesVerdict(0, 5000).verdict, 'regression')
})

test('DOM growth needs both 20% and 100 elements', () => {
  assert.equal(domVerdict(27, 2723), 'regression')
  assert.equal(domVerdict(1000, 1150), 'unchanged') // 15%
  assert.equal(domVerdict(100, 190), 'unchanged') // 90 elements
  assert.equal(domVerdict(2723, 27), 'improved')
})

test('a renamed hashed asset pairs by its stable name', () => {
  const out = attribute([req('/assets/reports-AAAAAAAA.js', 10000)], [req('/assets/reports-BBBBBBBB.js', 35000)])
  assert.deepEqual(out, [{ asset: '/assets/reports-BBBBBBBB.js', change: 'changed', base: 10000, head: 35000, delta: 25000 }])
})

test('repeated chunk names pair by nearest size, largest first', () => {
  const base = [req('/chunk-AAAAAAAA.js', 1000), req('/chunk-BBBBBBBB.js', 20000)]
  const head = [req('/chunk-CCCCCCCC.js', 1100), req('/chunk-DDDDDDDD.js', 45000)]
  const out = attribute(base, head)
  assert.equal(out[0].asset, '/chunk-DDDDDDDD.js')
  assert.equal(out[0].change, 'changed (paired by size)')
  assert.equal(out[0].base, 20000)
  // The 100-byte change is below the 256-byte reporting cut.
  assert.equal(out.length, 1)
})

test('unmatched assets are added or removed', () => {
  const out = attribute([req('/old-AAAAAAAA.js', 5000)], [req('/lodash-BBBBBBBB.js', 25000)])
  assert.deepEqual(out.map((x) => x.change), ['added', 'removed'])
})

const journey = (over = {}) => ({
  id: 'j', requests: [], totals: { js: { count: 1, raw: 0, gzip: 50000, brotli: 0 } }, missing: [],
  dom: { elements: 30, maxDepth: 5, tags: { div: 10 } }, a11y: [], a11yIncomplete: [], markup: [], jsErrors: [], failures: [], renderMs: 100,
  ...over,
})

test('a base run that differs from itself withholds that metric', () => {
  const base = { journeys: [journey()] }
  const base2 = { journeys: [journey({ missing: [{ url: '/flaky.png', status: 404 }] })] }
  const head = { journeys: [journey({ missing: [{ url: '/flaky.png', status: 404 }] })] }
  const r = compareRuns(base, head, base2)
  assert.equal(r.noise.deterministic, false)
  assert.equal(r.journeys[0].findings.find((f) => f.metric === 'missing assets').verdict, 'withheld')
})

test('a journey that fails on base is reported as broken and not compared', () => {
  const broken = journey({ failures: [{ step: { click: '#gone' }, error: 'click target not found: #gone' }] })
  const r = compareRuns({ journeys: [broken] }, { journeys: [journey({ missing: [{ url: '/x.png', status: 404 }] })] }, null)
  assert.deepEqual(r.journeys, [])
  assert.equal(r.brokenOnBase[0].id, 'j')
})

test('identical runs give no findings', () => {
  const r = compareRuns({ journeys: [journey()] }, { journeys: [journey()] }, { journeys: [journey()] })
  assert.equal(r.noise.deterministic, true)
  assert.deepEqual(r.journeys[0].findings, [])
})
