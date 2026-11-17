import { expect, test } from 'nsh/test'
import { connectionEvidence } from './connection-evidence.ts'

test('conn-before records both sides of synchronous diagnostic send at call time', async () => {
  let saved = '', clock = 100, sends = 0
  const trace = connectionEvidence({
    now: () => clock,
    write: async (_key, value) => { saved = value },
    send: () => { sends++; clock += 24000 },
  })
  trace.record('connection-step', { attempt: '1', step: 'conn-before' })
  clock = 90000 // Storage runs later; timestamps must already have been captured.
  await trace.flush()
  expect(sends).toBe(1)
  expect(JSON.parse(saved)).toEqual([
    { seq: 1, time: 100, what: 'connection-step', fields: { attempt: '1', step: 'conn-before' } },
    { seq: 2, time: 100, what: 'connection-hook', fields: { attempt: '1', step: 'conn-before', phase: 'send-before' } },
    { seq: 3, time: 24100, what: 'connection-hook', fields: { attempt: '1', step: 'conn-before', phase: 'send-return' } },
  ])
})

test('a synchronous diagnostic send failure is distinct from a returned promise', async () => {
  let saved = ''
  const trace = connectionEvidence({
    now: () => 1,
    write: async (_key, value) => { saved = value },
    send: () => { throw new TypeError('synthetic private send detail') },
  })
  trace.record('connection-step', { attempt: '1', step: 'conn-before' })
  await trace.flush()
  const rows = JSON.parse(saved)
  expect(rows.filter((e: any) => e.what === 'connection-hook').map((e: any) => e.fields.phase)).toEqual(['send-before', 'send-threw'])
  expect(saved.includes('synthetic private send detail')).toBe(false)
})

test('asynchronous HTTP rejection still records synchronous send return once', async () => {
  let saved = '', sends = 0
  const trace = connectionEvidence({
    now: () => 1,
    write: async (_key, value) => { saved = value },
    send: () => { sends++; return Promise.reject(new Error('synthetic offline')) },
  })
  trace.record('connection-step', { attempt: '1', step: 'conn-before' })
  await trace.flush()
  expect(sends).toBe(1)
  expect(JSON.parse(saved).filter((e: any) => e.what === 'connection-hook').map((e: any) => e.fields.phase)).toEqual(['send-before', 'send-return'])
})
