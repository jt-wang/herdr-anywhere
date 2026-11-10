// What the banner's reason says when the line can't be made, through the app's own Linker and Store. A failure
// within REFUSED_MS reads as "refused" for a socket straight to the computer; through TailPocket the socket goes to the
// phone's own forward, so a fast failure says nothing about the computer: it must not read as refused there.
import { expect, test } from 'nsh/test'
import { Linker, type Tunnel } from './link.ts'
import { LOST_TEXT, Store } from './store.ts'
import { t as tr } from './strings/index.ts'

// The reason the banner's words stand for: the store's code where it has one, else read back from its words.
const why = (s: Store) => (s as any).lostWhy?.get() ?? Object.keys(LOST_TEXT).find((k) => tr(LOST_TEXT[k]) === s.error.get())
const settle = async (n = 10) => { for (let i = 0; i < n; i++) await new Promise((r) => setTimeout(r, 0)) }

function rig(link: { url: string; via?: 'tailpocket' }) {
  const made: any[] = []
  const socket = () => { const p: any = { send() {}, close() {} }; made.push(p); return p }
  const tunnel: Tunnel = {
    pair: async () => '', scan: async () => '', forget() {},
    connect: async () => ({ baseUrl: 'http://127.0.0.1:40001', close() {} }),
  }
  const l = new Linker({ load: async () => null, save() {}, remove() {}, socket: socket as any, tunnel })
  l.set({ ...link, token: 'synthetic-token', device: 'd1', name: 'studio', enc: 'bin' })
  const timers: (() => void)[] = []
  const store = new Store({ pipe: () => l.pipe(), tunnelled: () => l.link.get()?.via === "tailpocket", setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any })
  return { made, store, timers }
}

test('a socket straight to the computer that closes at once reads as refused', async () => {
  const { made, store } = rig({ url: 'http://192.0.2.10:7681' })
  void store.connect().catch(() => {})
  await settle()
  made[0].onClose('network')
  await settle()
  expect(why(store)).toBe('refused')
})

test('through TailPocket, the forward closing at once is no answer from the computer, not refused, first try and retries', async () => {
  const { made, store, timers } = rig({ url: 'tailpocket:', via: 'tailpocket' })
  void store.connect().catch(() => {})
  await settle()
  made[0].onClose('network')
  await settle()
  expect(why(store)).toBe('no-answer')
  for (let i = 1; i <= 2; i++) {
    timers.shift()!()
    await settle()
    made[i].onClose('network')
    await settle()
    expect(why(store)).toBe('no-answer')
  }
})

test('through TailPocket, any failure before the computer answered is no answer, whatever failed (a tunnel that throws)', async () => {
  const timers: (() => void)[] = []
  const store = new Store({ pipe: () => { throw new Error('tunnel gone') }, tunnelled: () => true,
    setInterval: (() => 0) as any, clearInterval: (() => {}) as any, setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any } as any)
  store.connect().catch(() => {})
  store.reconnectNow()
  await settle()
  expect(why(store)).toBe('no-answer')
})
