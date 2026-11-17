import { expect, test } from 'nsh/test'
import { Store } from './store.ts'
import { setDiag } from './diag.ts'
import { FakeCore, settle } from './testing/fakecore.ts'

type Event = { what: string; fields: Record<string, string> }

function setup() {
  const events: Event[] = []
  setDiag((what, fields) => events.push({ what, fields }))
  const cores = [new FakeCore(), new FakeCore()]
  const timers: (() => void)[] = []
  let opened = 0
  const store = new Store({ pipe: () => cores[opened++], setInterval: (() => 1) as any,
    clearInterval: (() => {}) as any, setTimeout: ((f: () => void) => { timers.push(f); return 1 }) as any })
  return { store, cores, timers, events }
}

test('connection trace separates normal loss, scheduled retry, fired retry and ready', async () => {
  const { store, cores, timers, events } = setup()
  try {
    await store.connect()
    expect(events.filter((e) => e.what === 'connection-connect').map((e) => e.fields.step)).toEqual([
      'start', 'hello-before', 'hello-after', 'subscribe-before', 'subscribe-after', 'ready',
    ])
    events.length = 0
    cores[0].drop('network')
    await settle()
    expect(events[0]).toEqual({ what: 'connection-lost', fields: { wanted: 'true', retrying: 'false', attempt: '1' } })
    expect(events.filter((e) => e.what === 'connection-step').map((e) => e.fields.step)).toEqual([
      'stop-loops-before', 'stop-loops-after', 'clear-client-before', 'clear-client-after',
      'conn-before', 'conn-after', 'lost-why-before', 'lost-why-after', 'error-before', 'error-after',
    ])
    expect(events.at(-1)).toEqual({ what: 'connection-retry-scheduled', fields: { attempt: '1', delay: '1000' } })
    expect(timers.length).toBe(1)
    timers.shift()!()
    await settle(20)
    expect(events.some((e) => e.what === 'connection-retry-fired' && e.fields.wanted === 'true' && e.fields.hasClient === 'false')).toBe(true)
    expect(events.at(-1)?.fields.step).toBe('ready')
    expect(store.connected).toBe(true)
  } finally { store.disconnect(); setDiag(() => {}) }
})

for (const [field, step] of [['conn', 'conn-before'], ['lostWhy', 'lost-why-before'], ['error', 'error-before']] as const) {
  test(`connection trace identifies a throwing ${field} subscriber without swallowing it`, async () => {
    const { store, cores, timers, events } = setup()
    let off = () => {}
    try {
      await store.connect()
      events.length = 0
      const failure = new TypeError('synthetic subscriber failure: do not put this text in diagnostics')
      off = store[field].sub(() => { throw failure })
      let caught: unknown
      // Invoke the pipe close callback synchronously to observe the exact thrown object.
      try { cores[0].onClose?.('network') } catch (e) { caught = e }
      expect(caught).toBe(failure)
      expect(events.at(-1)).toEqual({ what: 'connection-error', fields: { attempt: '1', step, name: 'TypeError' } })
      expect(events.some((e) => e.what === 'connection-retry-scheduled')).toBe(false)
      expect(timers.length).toBe(0)
      expect(JSON.stringify(events).includes(failure.message)).toBe(false)
    } finally { off(); store.disconnect(); setDiag(() => {}) }
  })
}

test('connection trace reports an existing retry rather than claiming a second timer', async () => {
  const { store, cores, timers, events } = setup()
  try {
    await store.connect()
    cores[0].drop('network')
    await settle()
    events.length = 0
    ;(store as any).lost('network')
    expect(events[0].fields.retrying).toBe('true')
    expect(events.at(-1)).toEqual({ what: 'connection-return', fields: { attempt: '1', reason: 'retry-pending' } })
    expect(timers.length).toBe(1)
  } finally { store.disconnect(); setDiag(() => {}) }
})

test('connection trace records connect catch without copying an error message', async () => {
  const events: Event[] = []
  setDiag((what, fields) => events.push({ what, fields }))
  const failure = new TypeError('synthetic connection detail excluded from diagnostics')
  const store = new Store({ pipe: () => { throw failure } })
  try {
    let caught: unknown
    try { await store.connect() } catch (e) { caught = e }
    expect(caught).toBe(failure)
    expect(events.at(-1)).toEqual({ what: 'connection-connect-catch', fields: { attempt: '1', name: 'TypeError' } })
    expect(JSON.stringify(events).includes(failure.message)).toBe(false)
  } finally { store.disconnect(); setDiag(() => {}) }
})
