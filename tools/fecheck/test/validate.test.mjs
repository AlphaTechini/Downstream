import { test } from 'node:test'
import assert from 'node:assert/strict'
import { validateJourneys, LIMITS } from '../validate.mjs'

test('valid journeys pass unchanged', () => {
  const input = [{ id: 'home', steps: [{ visit: '/' }, { expectText: ['h1', 'Hi'] }] }, { id: 'nav', steps: [{ visit: '/a' }, { click: 'nav a:nth-of-type(2)' }, { expectCount: ['li', 3] }] }]
  const r = validateJourneys(input)
  assert.deepEqual(r.journeys, input)
  assert.deepEqual(r.dropped, [])
})

test('bad steps are dropped with a reason, not the whole file', () => {
  const r = validateJourneys([{ id: 'home', steps: [{ visit: '/' }, { click: 'a[[' }, { visit: 'http://evil.example/' }, { visit: '/x/../../etc' }, { hover: 'a' }] }])
  assert.deepEqual(r.journeys, [{ id: 'home', steps: [{ visit: '/' }] }])
  assert.equal(r.dropped.length, 4)
  assert.ok(r.dropped[0].includes('selector does not parse'))
})

test('a visit to "/" is always added', () => {
  const r = validateJourneys([{ id: 'reports', steps: [{ visit: '/reports' }] }])
  assert.deepEqual(r.journeys.map((j) => j.id), ['home', 'reports'])
})

test('journeys and steps are capped', () => {
  const many = Array.from({ length: LIMITS.journeys + 3 }, (_, i) => ({ id: `j${i}`, steps: [{ visit: '/' }] }))
  assert.equal(validateJourneys(many).journeys.length, LIMITS.journeys)
  const long = [{ id: 'long', steps: [{ visit: '/' }, ...Array.from({ length: 30 }, () => ({ expectCount: ['p', 1] }))] }]
  assert.equal(validateJourneys(long).journeys[0].steps.length, LIMITS.steps)
})

test('a journey must start with a visit and ids must be unique', () => {
  const r = validateJourneys([{ id: 'a', steps: [{ click: 'button' }] }, { id: 'b', steps: [{ visit: '/' }] }, { id: 'b', steps: [{ visit: '/' }] }])
  assert.deepEqual(r.journeys.map((j) => j.id), ['b'])
  assert.equal(r.dropped.length, 2)
})
