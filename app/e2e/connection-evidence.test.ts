import { expect, test } from 'nsh/test'
import { connectionEvidence, CONNECTION_EVIDENCE_KEY } from './connection-evidence.ts'

const events: [string, Record<string, string>][] = [
  ['connection-lost', { attempt: '1', wanted: 'true', retrying: 'false' }],
  ['connection-retry-scheduled', { attempt: '1', delay: '1000' }],
  ['connection-retry-fired', { attempt: '1', wanted: 'true', hasClient: 'false' }],
  ['connection-connect', { attempt: '2', step: 'ready' }],
]

test('failed HTTP still records lost, scheduled, fired and ready locally', async () => {
  let saved = ''; let clock = 100; let requests = 0
  const trace = connectionEvidence({
    write: async (key, value) => { expect(key).toBe(CONNECTION_EVIDENCE_KEY); saved = value },
    send: () => { requests++; return Promise.reject(new Error('synthetic offline')) }, now: () => clock++,
  })
  for (const [what, fields] of events) trace.record(what, fields)
  await trace.flush()
  expect(requests).toBe(4)
  expect(saved ? JSON.parse(saved) : []).toEqual(events.map(([what, fields], i) => ({ seq: i + 1, time: 100 + i, what, fields })))
})

test('a pending Storage write prevents a newer write from overtaking it', async () => {
  const writes: string[] = []; let release = () => {}; let saved = ''
  const trace = connectionEvidence({ now: () => 100, send: () => {}, write: async (_key, value) => {
    writes.push(value)
    if (writes.length === 1) await new Promise<void>((resolve) => { release = resolve })
    saved = value
  } })
  trace.record(...events[0]); trace.record(...events[1])
  await Promise.resolve(); await Promise.resolve()
  expect(writes.length).toBe(1)
  release(); await trace.flush()
  expect(writes.length).toBe(2)
  expect(JSON.parse(saved).map((e: any) => e.seq)).toEqual([1, 2])
})

test('records fixed fields only and snapshots fields at the call', async () => {
  let saved = ''
  const trace = connectionEvidence({ now: () => 1, send: () => {}, write: async (_k, v) => { saved = v } })
  const fields = { attempt: '1', step: 'conn-before', name: 'TypeError', message: 'synthetic private text', stack: 'synthetic stack', pair: 'synthetic pairing' }
  trace.record('connection-error', fields)
  fields.step = 'changed'
  trace.record('chat-text', { text: 'synthetic chat' })
  await trace.flush()
  expect(saved ? JSON.parse(saved) : []).toEqual([{ seq: 1, time: 1, what: 'connection-error', fields: { attempt: '1', step: 'conn-before', name: 'TypeError' } }])
})

test('a failed Storage write does not throw from the hook or stop later writes', async () => {
  let calls = 0; let saved = ''
  const trace = connectionEvidence({ now: () => 1, send: () => {}, write: (_k, v) => {
    if (++calls === 1) throw new Error('synthetic storage failure')
    saved = v; return Promise.resolve()
  } })
  trace.record(...events[0]); trace.record(...events[1]); await trace.flush()
  expect(calls).toBe(2)
  expect(saved ? JSON.parse(saved).length : 0).toBe(2)
})

test('pin events share ordered local storage and exclude addresses and labels', async () => {
  let saved = ''
  const trace = connectionEvidence({ now: () => 123, send: () => {}, write: async (_k, v) => { saved = v } })
  trace.record('pin-grant', { gesture: '1', pageY: '240', address: 'synthetic-address', label: 'synthetic-label' })
  trace.record('pin-release', { gesture: '1', moves: '2', firstY: '210', lastY: '120', firstDy: '-30', lastDy: '-120', dy: '-120', lifted: '1', dragging: '1' })
  trace.record('pin-write-success', { gesture: '1', from: '3', target: '1', after: '1', message: 'synthetic-private' })
  await trace.flush()
  const rows = saved ? JSON.parse(saved) : []
  expect(rows.map((e: any) => e.what)).toEqual(['pin-grant', 'pin-release', 'pin-write-success'])
  expect(rows[0].fields).toEqual({ gesture: '1', pageY: '240' })
  expect(rows[2].fields).toEqual({ gesture: '1', from: '3', target: '1', after: '1' })
  expect(rows.map((e: any) => e.seq)).toEqual([1, 2, 3])
})
