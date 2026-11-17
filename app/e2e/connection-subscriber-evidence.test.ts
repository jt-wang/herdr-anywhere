import { expect, test } from 'nsh/test'
import { Signal } from '../src/store.ts'
import { Updates } from '../src/updates.ts'
import { connectionEvidence } from './connection-evidence.ts'

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

test('static registration sites identify callbacks without stack coordinates', async () => {
  const { conn, trace, registrations } = setup()
  const sub = conn.sub as (f: () => void, site: string) => unknown
  sub(() => {}, 'Root.unpaired')
  sub(() => {}, 'kit.useSignal')
  await trace.flush()
  expect(registrations()).toEqual([
    { phase: 'registered', id: '1', site: 'Root.unpaired' },
    { phase: 'registered', id: '2', site: 'kit.useSignal' },
  ])
})

test('the real Updates subscription records its fixed registration site', async () => {
  const { conn, trace, registrations } = setup()
  conn.set('idle')
  const updates = new Updates({ conn: conn as any, baseUrl: async () => 'https://example.com',
    token: () => '', fetcher: (() => { throw new Error('unexpected fetch') }) as any,
    os: 'ios', appVersion: '0.0.0', toast: () => {}, install: async () => 'installing',
  })
  updates.start(); updates.stop()
  await trace.flush()
  expect(registrations()).toEqual([{ phase: 'registered', id: '1', site: 'Updates.follow' }])
})

test('an unlabelled callback records no generated stack location', async () => {
  const { conn, trace, registrations } = setup()
  conn.sub(() => {})
  await trace.flush()
  expect(registrations()).toEqual([{ phase: 'registered', id: '1', site: 'unlabelled' }])
})

test('ordinary Signal ignores registration labels and preserves subscription identity', () => {
  const conn = new Signal('ready'), seen: string[] = []
  const sub = conn.sub.bind(conn) as (f: (v: string) => void, site: string) => () => unknown
  const f = (v: string) => { seen.push(v) }
  const off = sub(f, 'Root.unpaired')
  sub(f, 'kit.useSignal')
  conn.set('reconnecting'); off(); conn.set('ready')
  expect(seen).toEqual(['reconnecting'])
})
