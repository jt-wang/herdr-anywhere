import { expect, test } from 'nsh/test'
import { Signal } from '../src/store.ts'
import { connectionEvidence, subscriberSite } from './connection-evidence.ts'

function setup() {
  let saved = '', sends = 0
  const conn = new Signal('ready')
  const trace = connectionEvidence({ now: () => 1,
    write: async (_key, value) => { saved = value }, send: () => { sends++ },
  })
  trace.traceSubscribers(conn)
  return { conn, trace, rows: () => JSON.parse(saved || '[]').filter((e: any) => e.fields.phase !== 'registered').map((e: any) => e.fields), registrations: () => JSON.parse(saved || '[]').filter((e: any) => e.fields.phase === 'registered').map((e: any) => e.fields), sends: () => sends }
}

test('real connection subscribers record entry and return locally in delivery order', async () => {
  const { conn, trace, rows, sends } = setup()
  const seen: string[] = []
  conn.sub((value) => { seen.push(`first:${value}`) })
  conn.sub((value) => { seen.push(`second:${value}`) })
  conn.set('reconnecting')
  await trace.flush()
  expect(seen).toEqual(['first:reconnecting', 'second:reconnecting'])
  expect(rows()).toEqual([
    { phase: 'notify-enter', id: '0' },
    { phase: 'enter', id: '1' }, { phase: 'return', id: '1' },
    { phase: 'enter', id: '2' }, { phase: 'return', id: '2' },
    { phase: 'notify-return', id: '0' },
  ])
  expect(sends()).toBe(0)
})

test('a subscriber that throws keeps the original object and leaves its entry unmatched', async () => {
  const { conn, trace, rows } = setup()
  const failure = new Error('synthetic callback failure')
  let later = false, caught: unknown
  conn.sub(() => { throw failure })
  conn.sub(() => { later = true })
  try { conn.set('reconnecting') } catch (e) { caught = e }
  expect(caught).toBe(failure)
  expect(later).toBe(false)
  await trace.flush()
  expect(rows()).toEqual([{ phase: 'notify-enter', id: '0' }, { phase: 'enter', id: '1' }])
})

test('subscription identities still deduplicate and unsubscribe during delivery', async () => {
  const { conn, trace, rows } = setup()
  let calls = 0
  const same = () => { calls++ }
  conn.sub(same); conn.sub(same)
  let off = () => {}
  conn.sub(() => off())
  off = conn.sub(() => { throw new Error('removed subscriber ran') }) as () => void
  conn.set('reconnecting')
  await trace.flush()
  expect(calls).toBe(1)
  expect(rows().filter((e: any) => e.phase === 'enter').map((e: any) => e.id)).toEqual(['1', '2'])
})

test('two real registration sites differ after excluding the collector frame', async () => {
  const { conn, trace, registrations } = setup()
  const first = () => conn.sub(() => {})
  const second = () => conn.sub(() => {})
  first(); second()
  conn.set('reconnecting')
  await trace.flush()
  const rows = registrations()
  expect(rows.length).toBe(2)
  expect(rows.map((r: any) => r.id)).toEqual(['1', '2'])
  for (const row of rows) expect(/^bundle\.js:\d+:\d+$/.test(row.site)).toBe(true)
  expect(rows[0].site === rows[1].site).toBe(false)
})

test('actual bundle coordinates skip the collector and retain the next two frames', () => {
  expect(subscriberSite('    at <anonymous> (bundle.js:1311:64)\n    at register (bundle.js:1458:30)\n    at <anonymous> (bundle.js:1459:2)\n')).toEqual({ site: 'bundle.js:1458:30', caller: 'bundle.js:1459:2' })
})

test('missing coordinates and private paths do not become a source location', () => {
  expect(subscriberSite('Error: synthetic message\n    at native')).toEqual({ site: 'unmapped', caller: '' })
  expect(subscriberSite('    at private (/private/synthetic/bundle.js:4:5)\n    at capture (bundle.js:10:2)\n    at register (bundle.js:20:3)')).toEqual({ site: 'bundle.js:20:3', caller: '' })
})
