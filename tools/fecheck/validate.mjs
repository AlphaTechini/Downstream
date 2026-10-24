// validate checks frontend journeys before any run: the step format, the limits, paths that
// stay on the app's origin, and selectors that parse. Invalid steps are dropped with a reason
// rather than failing the whole file, and a visit to "/" is always present.

import { Window } from 'happy-dom'

export const LIMITS = { journeys: 15, steps: 20 }
const ID = /^[a-z0-9_-]{1,40}$/
const STEP_KINDS = ['visit', 'click', 'type', 'expectText', 'expectCount']

function selectorError(doc, sel) {
  if (typeof sel !== 'string' || sel.length === 0 || sel.length > 300) return 'selector must be a non-empty string of at most 300 characters'
  try {
    doc.querySelector(sel)
    return null
  } catch (e) {
    return `selector does not parse: ${e.message}`
  }
}

function stepError(doc, step) {
  if (!step || typeof step !== 'object' || Array.isArray(step)) return 'step must be an object'
  const keys = Object.keys(step)
  if (keys.length !== 1 || !STEP_KINDS.includes(keys[0])) return `step must have exactly one of ${STEP_KINDS.join(', ')}`
  const [k] = keys
  const v = step[k]
  switch (k) {
    case 'visit':
      if (typeof v !== 'string' || !v.startsWith('/') || v.startsWith('//') || v.includes('..') || v.length > 300) return 'visit must be a path starting with "/" on the app origin'
      return null
    case 'click':
      return selectorError(doc, v)
    case 'type':
    case 'expectText':
      if (!Array.isArray(v) || v.length !== 2 || typeof v[1] !== 'string' || v[1].length > 500) return `${k} must be [selector, text]`
      return selectorError(doc, v[0])
    case 'expectCount':
      if (!Array.isArray(v) || v.length !== 2 || !Number.isInteger(v[1]) || v[1] < 0) return 'expectCount must be [selector, whole number]'
      return selectorError(doc, v[0])
  }
  return 'unknown step'
}

// validateJourneys returns the journeys to run and every reason something was dropped.
export function validateJourneys(input) {
  const window = new Window()
  const doc = window.document
  const dropped = []
  const out = []
  if (!Array.isArray(input)) return { journeys: [{ id: 'home', steps: [{ visit: '/' }] }], dropped: ['journeys must be a JSON array; only the home page is checked'] }
  const seen = new Set()
  for (const [i, j] of input.entries()) {
    if (out.length >= LIMITS.journeys) {
      dropped.push(`journey #${i + 1}: more than ${LIMITS.journeys} journeys`)
      continue
    }
    if (!j || !ID.test(j.id) || seen.has(j.id)) {
      dropped.push(`journey #${i + 1}: id must be unique and match [a-z0-9_-]{1,40}`)
      continue
    }
    if (!Array.isArray(j.steps)) {
      dropped.push(`journey ${j.id}: steps must be an array`)
      continue
    }
    const steps = []
    for (const [si, s] of j.steps.entries()) {
      if (steps.length >= LIMITS.steps) {
        dropped.push(`journey ${j.id} step ${si + 1}: more than ${LIMITS.steps} steps`)
        break
      }
      const err = stepError(doc, s)
      if (err) dropped.push(`journey ${j.id} step ${si + 1}: ${err}`)
      else steps.push(s)
    }
    if (!steps.length || !steps[0].visit) {
      dropped.push(`journey ${j.id}: must start with a valid visit step`)
      continue
    }
    seen.add(j.id)
    out.push({ id: j.id, steps })
  }
  if (!out.some((j) => j.steps.some((s) => s.visit === '/'))) {
    out.unshift({ id: seen.has('home') ? 'home-page' : 'home', steps: [{ visit: '/' }] })
    if (out.length > LIMITS.journeys) out.pop()
  }
  window.close()
  return { journeys: out, dropped }
}
