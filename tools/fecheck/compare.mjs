// compare turns two collected runs into deterministic verdicts (regression / improved /
// unchanged), and, given a second base run, withholds any metric that moved between the two
// base runs.

// Deterministic metrics need no noise band; the optional base2 run proves they are stable.
export const RULES = {
  bytesPct: 0.05, // gzip JS/CSS growth that counts as a regression...
  bytesMin: 1024, // ...but only if it is also at least this many bytes
  domPct: 0.2,
  domMin: 100,
}
const keyA = (v) => `${v.rule}|${v.target}`
const keyM = (v) => `${v.rule}|${v.selector}`

export function diffSets(a, b, key) {
  const ka = new Set(a.map(key))
  const kb = new Set(b.map(key))
  return { added: b.filter((x) => !ka.has(key(x))), removed: a.filter((x) => !kb.has(key(x))) }
}

// Explain a byte change asset by asset. Identical URLs are unchanged files. Otherwise match by
// hash-stripped name when that name is unique on both sides; when the same name repeats
// equally on both sides (Angular emits many "chunk-HASH.js"), pair largest-first by nearest
// gzip size; everything else is reported as added/removed.
export function attribute(bReqs, hReqs) {
  const bUrls = new Set(bReqs.map((r) => new URL(r.url).pathname))
  const hUrls = new Set(hReqs.map((r) => new URL(r.url).pathname))
  const bOnly = bReqs.filter((r) => !hUrls.has(new URL(r.url).pathname))
  const hOnly = hReqs.filter((r) => !bUrls.has(new URL(r.url).pathname))
  const count = (list) => list.reduce((m, r) => m.set(r.name, (m.get(r.name) || 0) + 1), new Map())
  const bc = count(bOnly), hc = count(hOnly)
  const out = []
  const used = new Set()
  for (const h of [...hOnly].sort((x, y) => y.gzip - x.gzip)) {
    let b = null
    let how = 'changed'
    if (bc.get(h.name) === 1 && hc.get(h.name) === 1) b = bOnly.find((x) => x.name === h.name)
    else if (bc.get(h.name) === hc.get(h.name)) {
      b = bOnly.filter((x) => x.name === h.name && !used.has(x)).sort((x, y) => Math.abs(x.gzip - h.gzip) - Math.abs(y.gzip - h.gzip))[0] || null
      how = 'changed (paired by size)'
    }
    if (b) used.add(b)
    out.push({ asset: new URL(h.url).pathname, change: b ? how : 'added', base: b?.gzip || 0, head: h.gzip, delta: h.gzip - (b?.gzip || 0) })
  }
  for (const b of bOnly) if (!used.has(b)) out.push({ asset: new URL(b.url).pathname, change: 'removed', base: b.gzip, head: 0, delta: -b.gzip })
  return out.filter((x) => Math.abs(x.delta) > 256).sort((x, y) => Math.abs(y.delta) - Math.abs(x.delta)).slice(0, 5)
}

// bytesVerdict applies the byte rule: growth must pass both the percentage and the minimum.
export function bytesVerdict(base, head) {
  const delta = head - base
  const pct = base ? delta / base : delta ? 1 : 0
  if (delta > RULES.bytesMin && pct > RULES.bytesPct) return { verdict: 'regression', delta, pct }
  if (-delta > RULES.bytesMin && -pct > RULES.bytesPct) return { verdict: 'improved', delta, pct }
  return { verdict: 'unchanged', delta, pct }
}

// domVerdict applies the DOM rule: growth must pass both the percentage and the minimum.
export function domVerdict(base, head) {
  const de = head - base
  if (de > RULES.domMin && de / Math.max(base, 1) > RULES.domPct) return 'regression'
  if (-de > RULES.domMin && -de / Math.max(base, 1) > RULES.domPct) return 'improved'
  return 'unchanged'
}

export function compareJourney(b, h) {
  const findings = []
  const push = (metric, verdict, detail) => findings.push({ metric, verdict, detail })

  for (const k of new Set([...Object.keys(b.totals), ...Object.keys(h.totals)])) {
    if (!['js', 'css', 'img', 'font', 'html'].includes(k)) continue
    const bb = b.totals[k]?.gzip || 0
    const hh = h.totals[k]?.gzip || 0
    const { verdict, delta, pct } = bytesVerdict(bb, hh)
    if (verdict !== 'unchanged') {
      const culprits = attribute(b.requests.filter((r) => r.kind === k), h.requests.filter((r) => r.kind === k))
      push(`${k} bytes (gzip)`, verdict, { base: bb, head: hh, delta, pct: +(pct * 100).toFixed(1), culprits })
    }
  }
  const bc = Object.values(b.totals).reduce((s, t) => s + t.count, 0)
  const hc = Object.values(h.totals).reduce((s, t) => s + t.count, 0)
  if (hc !== bc) push('request count', hc > bc ? 'regression' : 'improved', { base: bc, head: hc })

  const miss = diffSets(b.missing, h.missing, (m) => m.url)
  if (miss.added.length) push('missing assets', 'regression', miss.added)
  if (miss.removed.length) push('missing assets', 'improved', miss.removed)

  const dv = domVerdict(b.dom.elements, h.dom.elements)
  if (dv === 'regression') {
    const growth = Object.entries(h.dom.tags)
      .map(([t, n]) => [t, n - (b.dom.tags[t] || 0)])
      .filter(([, n]) => n > 0)
      .sort((x, y) => y[1] - x[1])
      .slice(0, 5)
    push('DOM size', 'regression', { base: b.dom.elements, head: h.dom.elements, depth: [b.dom.maxDepth, h.dom.maxDepth], growth })
  } else if (dv === 'improved') {
    push('DOM size', 'improved', { base: b.dom.elements, head: h.dom.elements })
  }

  const ax = diffSets(b.a11y, h.a11y, keyA)
  if (ax.added.length) push('accessibility', 'regression', ax.added)
  if (ax.removed.length) push('accessibility', 'improved', ax.removed)

  const mk = diffSets(b.markup, h.markup, keyM)
  if (mk.added.length) push('markup validity', 'regression', mk.added)
  if (mk.removed.length) push('markup validity', 'improved', mk.removed)

  const je = diffSets(b.jsErrors.map((e) => ({ e })), h.jsErrors.map((e) => ({ e })), (x) => x.e)
  if (je.added.length) push('JS errors', 'regression', je.added.map((x) => x.e))
  const jf = diffSets(b.failures, h.failures, (f) => JSON.stringify(f.step) + f.error)
  if (jf.added.length) push('journey steps', 'regression', jf.added)
  if (b.a11yIncomplete.length || h.a11yIncomplete.length) push('not checked', 'withheld', [...new Set([...b.a11yIncomplete, ...h.a11yIncomplete, 'color-contrast'])])
  return findings
}

export function compareRuns(base, head, base2) {
  const result = { journeys: [], noise: null }
  if (base2) {
    // A base-vs-base comparison must produce nothing for deterministic metrics.
    const unstable = []
    for (const j of base.journeys) {
      const j2 = base2.journeys.find((x) => x.id === j.id)
      if (!j2) continue
      const f = compareJourney(j, j2).filter((x) => x.verdict !== 'withheld')
      if (f.length) unstable.push({ id: j.id, findings: f })
    }
    result.noise = { deterministic: unstable.length === 0, unstable }
  }
  for (const b of base.journeys) {
    const h = head.journeys.find((x) => x.id === b.id)
    if (!h) continue
    const findings = compareJourney(b, h)
    // If base was unstable on a metric, withhold that metric for this journey.
    const unstable = result.noise?.unstable.find((u) => u.id === b.id)
    for (const f of findings) if (unstable?.findings.some((u) => u.metric === f.metric) && f.verdict !== 'withheld') f.verdict = 'withheld'
    result.journeys.push({ id: b.id, findings, renderMs: { base: b.renderMs, head: h.renderMs, note: 'advisory, no layout or paint' } })
  }
  return result
}
