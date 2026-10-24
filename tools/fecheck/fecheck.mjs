// fecheck: browser-free frontend before/after checks for Downstream.
//
//   node fecheck.mjs collect --url http://127.0.0.1:4101 --journeys journeys.json --out base.json
//   node fecheck.mjs compare --base base.json --head head.json [--base2 base2.json] [--out verdict.json]

import { readFileSync, writeFileSync } from 'node:fs'
import { collect } from './collect.mjs'
import { compareRuns } from './compare.mjs'

const args = Object.fromEntries(
  process.argv.slice(3).reduce((acc, a, i, arr) => (a.startsWith('--') ? [...acc, [a.slice(2), arr[i + 1]]] : acc), []),
)
const readJSON = (p) => JSON.parse(readFileSync(p, 'utf8'))

const cmd = process.argv[2]
if (cmd === 'collect') {
  const origin = args.url.replace(/\/$/, '')
  const out = await collect(origin, readJSON(args.journeys))
  writeFileSync(args.out, JSON.stringify(out, null, 2))
  console.log(`collected ${out.journeys.length} journeys from ${origin} -> ${args.out}`)
} else if (cmd === 'validate') {
  const { validateJourneys } = await import('./validate.mjs')
  const result = validateJourneys(readJSON(args.journeys))
  writeFileSync(args.out, JSON.stringify(result.journeys, null, 2))
  console.log(JSON.stringify({ journeys: result.journeys.length, dropped: result.dropped }))
} else if (cmd === 'compare') {
  const result = compareRuns(readJSON(args.base), readJSON(args.head), args.base2 ? readJSON(args.base2) : null)
  const out = JSON.stringify(result, null, 2)
  if (args.out) writeFileSync(args.out, out)
  console.log(out)
} else {
  console.error('usage: fecheck.mjs collect|compare ...')
  process.exit(2)
}
