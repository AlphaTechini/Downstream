// collect renders each journey in happy-dom (no browser binary), executing the app's real
// production JS, and records per journey: every request with raw/gzip/brotli bytes, missing
// assets (4xx/5xx), DOM size, axe-core accessibility violations, html-validate findings,
// uncaught JS errors and failed journey expectations.

import { Browser } from 'happy-dom'
import { HtmlValidate } from 'html-validate'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { gz, br, kind, stableName, rewriteImportMeta, sleep, withCap } from './helpers.mjs'

const require = createRequire(import.meta.url)
const AXE_SRC = readFileSync(require.resolve('axe-core/axe.min.js'), 'utf8')

export async function runJourney(origin, journey) {
  const ledger = new Map() // url -> record (deduped: browsers cache repeat fetches)
  async function record(url, method = 'GET') {
    if (ledger.has(url)) return ledger.get(url)
    const res = await fetch(url, { method, redirect: 'follow' })
    const buf = Buffer.from(await res.arrayBuffer())
    const type = res.headers.get('content-type') || ''
    const rec = { url, name: stableName(url), status: res.status, type, kind: kind(type, url), raw: buf.length, gzip: gz(buf), brotli: br(buf), buf }
    ledger.set(url, rec)
    return rec
  }

  const browser = new Browser({
    settings: {
      enableJavaScriptEvaluation: true,
      suppressInsecureJavaScriptEnvironmentWarning: true,
      enableCSSFileLoading: true,
      navigator: { userAgent: 'Mozilla/5.0 (X11; Linux x86_64) Downstream-fecheck' },
      viewport: { width: 1280, height: 800 },
      fetch: {
        interceptor: {
          beforeAsyncRequest: async ({ request, window }) => {
            if (!request.url.startsWith(origin)) return new window.Response('', { status: 204 })
            const rec = await record(request.url, request.method)
            let body = rec.buf
            if (rec.kind === 'js') body = rewriteImportMeta(rec.buf.toString('utf8'), request.url)
            return new window.Response(body, { status: rec.status, headers: { 'content-type': rec.type } })
          },
        },
      },
    },
  })
  const page = browser.newPage()
  const failures = []
  const t0 = performance.now()
  // Wait for the page's pending work (scripts, fetches, timers) to finish, but never longer than
  // SETTLE_MS: apps that poll with setInterval never go idle.
  const SETTLE_MS = Number(process.env.FECHECK_SETTLE_MS || 10000)
  const settle = async () => {
    const ok = await withCap((async () => {
      await page.waitUntilComplete()
      await sleep(50)
      await page.waitUntilComplete()
    })(), SETTLE_MS)
    if (!ok) failures.push({ step: 'settle', error: `page still busy after ${SETTLE_MS}ms (timers or long-polling); measured as-is` })
  }
  const doc = () => page.mainFrame.document
  const win = () => page.mainFrame.window

  for (const step of journey.steps) {
    try {
      if (step.visit) {
        await page.goto(origin + step.visit)
        await settle()
      } else if (step.click) {
        const el = doc().querySelector(step.click)
        if (!el) throw new Error(`click target not found: ${step.click}`)
        el.click()
        await settle()
      } else if (step.type) {
        const [sel, text] = step.type
        const el = doc().querySelector(sel)
        if (!el) throw new Error(`type target not found: ${sel}`)
        el.value = text
        el.dispatchEvent(new (win().Event)('input', { bubbles: true }))
        el.dispatchEvent(new (win().Event)('change', { bubbles: true }))
        await settle()
      } else if (step.expectText) {
        const [sel, text] = step.expectText
        const el = doc().querySelector(sel)
        const got = el ? el.textContent.trim() : null
        if (!got || !got.includes(text)) throw new Error(`expected "${text}" in ${sel}, got ${JSON.stringify(got)}`)
      } else if (step.expectCount) {
        const [sel, n] = step.expectCount
        const got = doc().querySelectorAll(sel).length
        if (got !== n) throw new Error(`expected ${n} x ${sel}, got ${got}`)
      }
    } catch (e) {
      failures.push({ step, error: e.message })
    }
  }
  const renderMs = performance.now() - t0

  // Resources referenced by the final DOM that the app never fetched (images are not loaded by
  // happy-dom, and a broken reference is a regression even if nothing requested it yet).
  const d = doc()
  const refs = new Set()
  for (const el of d.querySelectorAll('img[src],source[src],video[poster],link[href][rel~="icon"],link[rel="stylesheet"][href],script[src]')) {
    const v = el.getAttribute('src') || el.getAttribute('poster') || el.getAttribute('href')
    try { refs.add(new URL(v, d.location.href).href) } catch {}
  }
  for (const u of refs) if (u.startsWith(origin)) await record(u)

  const all = d.querySelectorAll('*')
  let maxDepth = 0
  for (const el of all) {
    let depth = 0
    for (let n = el; n.parentElement; n = n.parentElement) depth++
    if (depth > maxDepth) maxDepth = depth
  }
  const tags = {}
  for (const el of all) tags[el.tagName.toLowerCase()] = (tags[el.tagName.toLowerCase()] || 0) + 1

  // axe-core inside the happy-dom window. color-contrast needs real layout, so it is disabled
  // and reported as "not checked" rather than passed.
  let a11y = []
  let a11yIncomplete = []
  try {
    const s = d.createElement('script')
    s.textContent = AXE_SRC
    d.head.appendChild(s)
    const r = await win().axe.run(d, { rules: { 'color-contrast': { enabled: false } }, resultTypes: ['violations', 'incomplete'] })
    a11y = r.violations.flatMap((v) => v.nodes.map((n) => ({ rule: v.id, impact: v.impact, target: n.target.join(' '), help: v.help })))
    a11yIncomplete = r.incomplete.map((v) => v.id)
    s.remove()
  } catch (e) {
    failures.push({ step: 'axe', error: e.message })
  }

  const html = '<!doctype html>\n' + d.documentElement.outerHTML
  const hv = new HtmlValidate({
    extends: ['html-validate:recommended'],
    rules: { 'no-inline-style': 'off', 'attribute-boolean-style': 'off', 'void-style': 'off', 'no-trailing-whitespace': 'off', 'attr-case': 'off', 'doctype-style': 'off', 'attribute-empty-style': 'off' },
  })
  const vr = await hv.validateString(html)
  const markup = vr.results.flatMap((r) => r.messages.map((m) => ({ rule: m.ruleId, selector: m.selector || '', message: m.message })))

  // Uncaught errors the app raised (happy-dom prints them to its virtual console)
  const consoleText = page.virtualConsolePrinter.readAsString()
  const jsErrors = [...new Set(consoleText.split('\n').filter((l) => /^(\w*Error|Uncaught)/.test(l.trim())).map((l) => l.trim().slice(0, 300)))]

  await browser.close()

  const requests = [...ledger.values()].map(({ buf, ...r }) => r)
  const totals = {}
  for (const r of requests) {
    if (r.status >= 400) continue
    const t = (totals[r.kind] ||= { count: 0, raw: 0, gzip: 0, brotli: 0 })
    t.count++; t.raw += r.raw; t.gzip += r.gzip; t.brotli += r.brotli
  }
  return {
    id: journey.id,
    requests,
    totals,
    missing: requests.filter((r) => r.status >= 400).map((r) => ({ url: new URL(r.url).pathname, status: r.status })),
    dom: { elements: all.length, maxDepth, tags },
    a11y, a11yIncomplete, markup, jsErrors, failures,
    renderMs: Math.round(renderMs), // advisory only: happy-dom has no layout or paint
  }
}

export async function collect(origin, journeys) {
  const out = { origin, collectedAt: new Date().toISOString(), journeys: [] }
  for (const j of journeys) out.journeys.push(await runJourney(origin, j))
  return out
}
