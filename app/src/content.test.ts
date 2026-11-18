import { expect, test } from 'nsh/test'
import { Store } from './store.ts'
import { Linker, type Link } from './link.ts'
import { FakeCore, settle } from './testing/fakecore.ts'

const link: Link = { url: 'http://192.0.2.40:7681', token: 'orchard-token', name: 'Orchard', device: 'phone', enc: 'bin' }
function disk() {
  const saved = new Map<string, string>()
  const io = { load: async (k: string) => saved.get(k) ?? null, save: (k: string, v: string) => { saved.set(k, v) },
    write: async (k: string, v: string) => { saved.set(k, v) }, remove: (k: string) => { saved.delete(k) } }
  const linker = new Linker(io)
  linker.set(link)
  return { saved, io, linker }
}
function core() {
  const c = new FakeCore()
  const doc = { spaces: [] }
  c.addSpace(doc, 'orchard', '/srv/orchard', 'Orchard', [{ id: 'grove', argv: ['claude'], status: 'working' }])
  c.docs.set('', { version: 1, value: doc })
  return c
}
function store(c: FakeCore, linker: Linker) {
  return new Store({ pipe: () => c, content: linker.content(), callTimeoutMs: 100,
    setInterval: (() => 0) as any, clearInterval: (() => {}) as any, setTimeout: (() => 0) as any })
}

test('cold offline home and chat are available before hello; a fresh empty layout replaces rather than uploads the cache', async () => {
  const { linker } = disk()
  const first = store(core(), linker)
  await first.connect()
  first.rememberChat('t1', { chat: true, session: 'grove', turns: [{ id: 'u1', from: 'you', said: 'Count trees', reply: 'Twelve trees.', steps: [], end: 'done' }] })
  await settle(20)
  first.disconnect()
  const remote = new FakeCore()
  remote.mute = true
  const cold = store(remote, linker)
  const connecting = cold.connect().catch(() => {})
  await settle(20)
  expect(cold.model.spaces.map((s) => s.name)).toEqual(['Orchard'])
  expect(cold.agents().map((a) => a.address)).toEqual(['t1'])
  expect(cold.focusTerminal('t1')).toBe(true)
  expect(cold.cachedChat('t1')?.turns?.[0].reply).toBe('Twelve trees.')
  expect(typeof cold.staleFor()).toBe('number')
  expect(remote.calls.length).toBe(0)
  remote.drop()
  await connecting
  cold.disconnect()
  const empty = new FakeCore()
  empty.docs.set('', { version: 4, value: { spaces: [] } })
  const refreshed = store(empty, linker)
  await refreshed.connect()
  expect(refreshed.model.spaces).toEqual([])
  expect(empty.doc().spaces).toEqual([])
  expect(empty.calls.filter((c) => c.method === 'doc.set')).toEqual([])
  refreshed.disconnect()
})

test('pending survives process restart, sends in order once, and a send interrupted after handoff is recovered without replay', async () => {
  const { linker, saved } = disk()
  const first = store(core(), linker)
  await first.connect()
  first.disconnect()
  await first.compose('t1', 'Count trees')
  await first.compose('t1', 'Count pears')
  const next = core()
  next.holding.add('terminal.send')
  const second = store(next, linker)
  await second.connect()
  await settle(20)
  expect(second.pending.get().map((s) => s.text)).toEqual(['Count pears'])
  expect(JSON.parse(JSON.parse(saved.get('content')!).value).sending).toEqual([{ address: 't1', text: 'Count trees' }])
  const interrupted = saved.get('content')!
  second.disconnect()
  await settle(20)
  // A new process sees exactly the last durable state at the instant of interruption, not cleanup from this test.
  saved.set('content', interrupted)
  const after = core()
  const received: string[] = []
  after.onSend = (_a, text) => received.push(text)
  const third = store(after, linker)
  await third.connect()
  await settle(20)
  expect(third.sending.get()).toEqual([{ address: 't1', text: 'Count trees', failed: true }])
  expect(received).toEqual([])
  third.forget('t1') // the composer has taken the uncertain text back
  third.sync()
  await settle(20)
  expect(received).toEqual(['Count pears'])
  expect(third.pending.get()).toEqual([])
  third.sync()
  await settle(20)
  expect(received).toEqual(['Count pears'])
  third.disconnect()
  const fourthCore = core()
  fourthCore.onSend = (_a, text) => received.push(text)
  const fourth = store(fourthCore, linker)
  await fourth.connect()
  await settle(20)
  expect(fourth.sending.get()).toEqual([])
  expect(received).toEqual(['Count pears'])
  fourth.disconnect()
})

test('content is isolated by pairing and cleared on forgetting and server revocation, including late writers', async () => {
  const { linker, saved, io } = disk()
  const old = linker.content()
  await old.save('orchard')
  const restart = new Linker(io)
  await restart.load()
  expect(await restart.content().load()).toBe('orchard')
  restart.set({ ...link, token: 'another-token' })
  expect(await restart.content().load()).toBe(null)
  linker.forget()
  linker.set(link)
  await old.save('late orchard')
  expect(saved.has('content')).toBe(false)
  let pipe: any
  const revoked = new Linker({ ...io, socket: () => (pipe = { send() {}, close() {} }) })
  await revoked.load()
  const cache = revoked.content()
  await cache.save('orchard again')
  revoked.pipe()
  pipe.onClose('unpaired')
  await cache.save('late revoked')
  expect(saved.has('content')).toBe(false)
})

test('offline persisted chat is bounded and authoritative uncertainty replaces it', async () => {
  const { linker, saved } = disk()
  const first = store(core(), linker)
  await first.connect()
  for (let n = 0; n < 14; n++) first.rememberChat(`a${n}`, { chat: true, session: 'grove', turns: Array.from({ length: 70 }, (_, i) =>
    ({ id: `u${i}`, from: 'you' as const, said: `Question ${i}`, reply: 'Pear trees.', steps: [], end: 'done' as const })) })
  first.rememberChat('a13', { chat: true, unknown: 'no-record' })
  first.rememberChat('a12', { chat: true, session: 'grove', turns: [
    { id: 'large', from: 'you', said: 'A large answer', reply: 'x'.repeat(1_100_000), steps: [], end: 'done' },
  ] })
  await settle(30)
  expect(JSON.stringify(JSON.parse(JSON.parse(saved.get('content')!).value).chats).length <= 1_000_000).toBe(true)
  first.disconnect()
  const remote = new FakeCore()
  remote.mute = true
  const next = store(remote, linker)
  const waiting = next.connect().catch(() => {})
  await settle(20)
  expect(next.cachedChat('a0')).toBe(undefined)
  expect(next.cachedChat('a2')?.turns?.length).toBe(40)
  expect(next.cachedChat('a2')?.turns?.[0].id).toBe('u30')
  expect(next.cachedChat('a13')?.turns).toBe(undefined)
  expect(next.cachedChat('a12')?.turns).toEqual([]) // omit an oversized whole turn rather than silently truncate its text
  remote.drop()
  await waiting
  next.disconnect()
})

test('the outbox is durable before handoff, and a storage failure keeps offline text out of the send queue', async () => {
  const { linker, saved, io } = disk()
  const c = core()
  const s = store(c, linker)
  await s.connect()
  await settle(20)
  const received: string[] = []
  c.onSend = (_a, text) => received.push(text)
  let release!: () => void
  io.write = async (k, v) => { await new Promise<void>((r) => { release = r }); saved.set(k, v) }
  const sending = s.compose('t1', 'Count apples')
  await settle(20)
  expect(received).toEqual([])
  release()
  await sending
  expect(received).toEqual(['Count apples'])
  // Let the acknowledgement's write finish too.
  await settle(20)
  release()
  await settle(20)
  s.disconnect()
  io.write = async () => { throw new Error('disk full') }
  const error = await s.compose('t1', 'Count plums').then(() => '', (e) => e.message)
  expect(error).toBe('disk full')
  expect(s.pending.get()).toEqual([])
  expect(received).toEqual(['Count apples'])
})

test('refresh waits for remote layout, applies intervening events, and unavailable layout is never uploaded', async () => {
  const { linker } = disk()
  const first = store(core(), linker)
  await first.connect()
  await settle(20)
  first.disconnect()
  const c = core()
  c.doc().spaces[0].name = 'Pear grove'
  c.holding.add('doc.get')
  const s = store(c, linker)
  const refresh = s.connect()
  await settle(20)
  expect(s.model.spaces[0].name).toBe('Orchard')
  c.setStatus('t1', 'blocked', 'claude-code', { msg: 'Approve pruning' })
  c.holding.clear()
  c.release()
  await refresh
  expect(s.model.spaces[0].name).toBe('Pear grove')
  expect(s.statusOf('t1')).toBe('blocked')
  await settle(20)
  s.disconnect()
  const unavailable = core()
  unavailable.holding.add('doc.get')
  const next = store(unavailable, linker)
  await next.connect()
  next.sync()
  expect(next.model.spaces[0].name).toBe('Pear grove')
  expect(next.slots.get('')?.available).toBe(false)
  expect(unavailable.calls.filter((c) => c.method === 'doc.set')).toEqual([])
  next.disconnect()
})
