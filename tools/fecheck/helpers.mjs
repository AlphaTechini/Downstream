// Small pure helpers shared by collect and compare.

import { gzipSync, brotliCompressSync, constants as zc } from 'node:zlib'

export const gz = (b) => gzipSync(b, { level: 9 }).length
export const br = (b) => brotliCompressSync(b, { params: { [zc.BROTLI_PARAM_QUALITY]: 11 } }).length

export function kind(type, url) {
  if (/javascript|ecmascript/.test(type) || /\.m?js(\?|$)/.test(url)) return 'js'
  if (/css/.test(type) || /\.css(\?|$)/.test(url)) return 'css'
  if (/html/.test(type)) return 'html'
  if (/json/.test(type)) return 'data'
  if (/^image\//.test(type) || /\.(png|jpe?g|gif|svg|webp|avif|ico)(\?|$)/.test(url)) return 'img'
  if (/font/.test(type) || /\.(woff2?|ttf|otf)(\?|$)/.test(url)) return 'font'
  return 'other'
}

// Strip content hashes so base and head assets can be matched: index-BQx1u_j8.js -> index.js
export const stableName = (url) => {
  const p = new URL(url).pathname
  return p.replace(/[-.]([A-Za-z0-9_]{8,})(?=\.[a-z0-9]+$)/, '')
}

// happy-dom's ES module compiler cannot evaluate import.meta, which production Vite/Angular
// bundles use for preload URLs. Rewrite it to a literal of the module's own URL.
export function rewriteImportMeta(src, url) {
  if (!src.includes('import.meta')) return src
  const u = JSON.stringify(url)
  return src
    .replace(/import\.meta\.url/g, u)
    .replace(/import\.meta\.resolve/g, `((s)=>new URL(s,${u}).href)`)
    .replace(/import\.meta\b/g, `({url:${u}})`)
}

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

// withCap resolves true when work finishes and false when ms pass first. The timer is always
// cleared, or a pending timer would keep Node alive after the last page.
export async function withCap(work, ms) {
  let timer
  const timeout = new Promise((r) => { timer = setTimeout(() => r(false), ms) })
  try {
    return await Promise.race([work.then(() => true), timeout])
  } finally {
    clearTimeout(timer)
  }
}
