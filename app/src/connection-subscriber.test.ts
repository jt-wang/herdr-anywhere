import { expect, test } from 'nsh/test'
import { Signal, Store } from './store.ts'
import { setDiag } from './diag.ts'
import { FakeCore, settle } from './testing/fakecore.ts'

test('a connection subscriber renewed during notification does not delay retry scheduling', async () => {
  const events: { what: string; fields: Record<string, string> }[] = []
  const cores = [new FakeCore(), new FakeCore()]
  const timers: (() => void)[] = []
  let opened = 0, visits = 0
  let off: () => unknown = () => {}
  setDiag((what, fields) => { if (what.startsWith('connection-')) events.push({ what, fields }) })
  const store = new Store({ pipe: () => cores[opened++], setInterval: (() => 1) as any,
    clearInterval: (() => {}) as any, setTimeout: ((f: () => void) => { timers.push(f); return 1 }) as any })
  const follow = (conn: string) => {
    if (conn !== 'reconnecting') return
    visits++
    off()
    // A finite replacement sequence exposes repeated delivery without hanging the test process.
    if (visits < 3) off = store.conn.sub(follow)
  }
  try {
    await store.connect()
    off = store.conn.sub(follow)
    events.length = 0
    cores[0].onClose?.('network')
    expect(visits).toBe(1)
    expect(events.some((e) => e.what === 'connection-step' && e.fields.step === 'conn-after')).toBe(true)
    expect(events.some((e) => e.what === 'connection-error')).toBe(false)
    expect(timers.length).toBe(1)
    timers.shift()!()
    await settle(20)
    expect(store.connected).toBe(true)
    expect(events.at(-1)?.fields.step).toBe('ready')
  } finally { off(); store.disconnect(); setDiag(() => {}) }
})

test('a subscriber removed before its turn is not called', () => {
  const signal = new Signal(0)
  const seen: string[] = []
  let off: () => unknown = () => {}
  signal.sub(() => { seen.push('first'); off() })
  off = signal.sub(() => seen.push('removed'))
  signal.set(1)
  expect(seen).toEqual(['first'])
})

test('a subscriber added during notification first receives the next value', () => {
  const signal = new Signal(0)
  const seen: string[] = []
  signal.sub((v) => {
    seen.push(`first:${v}`)
    if (v === 1) signal.sub((next) => seen.push(`new:${next}`))
  })
  signal.set(1)
  expect(seen).toEqual(['first:1'])
  signal.set(2)
  expect(seen).toEqual(['first:1', 'first:2', 'new:2'])
})

test('a subscriber throws its original object synchronously and stops that notification', () => {
  const signal = new Signal(0)
  const failure = new TypeError('synthetic subscriber failure')
  let laterCalled = false, caught: unknown
  signal.sub(() => { throw failure })
  signal.sub(() => { laterCalled = true })
  try { signal.set(1) } catch (e) { caught = e }
  expect(caught).toBe(failure)
  expect(laterCalled).toBe(false)
  expect(signal.get()).toBe(1)
})
