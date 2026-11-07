// One pane's terminal on the phone: the web page's messages in, herdr-core's stream out.
import { expect, test } from 'nsh/test'
import { Store } from '../store.ts'
import { FakeCore, settle } from '../testing/fakecore.ts'
import { PAGE_READY_MS, TerminalSession } from './terminal-session.ts'
import { t } from '../strings/index.ts'
import { sinceLabel } from '../model/seen.ts'

async function setup() {
  const cores = [new FakeCore(), new FakeCore()]
  let n = 0
  const timers: (() => void)[] = []
  const store = new Store({ pipe: () => cores[n++], setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any })
  await store.connect()
  const r = await store.newSpace({})
  const posted: any[] = []
  const s = new TerminalSession(store, r.terminal, (m) => posted.push(JSON.parse(m)), (f) => { queueMicrotask(f); return 1 })
  return { core: cores[0], cores, store, r, s, posted, timers }
}

test('the page saying ready attaches at its size; output reaches the page after a reset', async () => {
  const { core, r, s, posted } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 61, rows: 19 }))
  await settle()
  const at = core.calls.find((c) => c.method === 'terminal.attach')!.params
  expect([at.terminal, at.cols, at.rows]).toEqual([r.terminal, 61, 19])
  core.output(r.terminal, 'hello')
  await settle()
  expect(posted.map((m) => m.t)).toEqual(['reset', 'write', 'write'])
  expect(posted.slice(1).map((m) => m.data).join('')).toBe(`\x1b[H\x1b[2J${r.terminal}$ hello`)
  expect(s.state.get()).toBe('live')
})

test('typing goes to the terminal; Ctrl from the key bar applies to the next key only', async () => {
  const { core, s } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  s.page(JSON.stringify({ t: 'input', data: 'ls\r' }))
  s.toggleCtrl()
  expect(s.ctrl.get()).toBe(true)
  s.page(JSON.stringify({ t: 'input', data: 'c' }))
  s.page(JSON.stringify({ t: 'input', data: 'c' }))
  await settle()
  expect(core.written.map((w) => w.data)).toEqual(['ls\r', '\x03', 'c'])
  expect(s.ctrl.get()).toBe(false)
})

test('Shift from the key bar goes with the next key only: a bar key asks the page, a typed letter is capital', async () => {
  const { core, s, posted } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  s.toggleShift()
  expect(s.shift.get()).toBe(true)
  s.key('tab')
  s.key('tab')
  s.toggleShift()
  s.page(JSON.stringify({ t: 'input', data: 'a' }))
  s.page(JSON.stringify({ t: 'input', data: 'a' }))
  await settle()
  expect(posted.filter((m: any) => m.t === 'key')).toEqual([{ t: 'key', name: 'tab', shift: true }, { t: 'key', name: 'tab' }])
  expect(core.written.map((w) => w.data)).toEqual(['A', 'a'])
  expect(s.shift.get()).toBe(false)
})

test('a key from the bar with Ctrl held asks the page for the combination, once', async () => {
  const { s, posted } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  s.toggleCtrl()
  s.key('enter')
  s.key('enter')
  await settle()
  const keys = posted.filter((m: any) => m.t === 'key')
  expect(keys).toEqual([{ t: 'key', name: 'enter', ctrl: true }, { t: 'key', name: 'enter' }])
  expect(s.ctrl.get()).toBe(false)
})

test('a new page size resizes the terminal', async () => {
  const { core, s } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  s.page(JSON.stringify({ t: 'size', cols: 40, rows: 30 }))
  await settle()
  expect(core.resized.map((x) => [x.cols, x.rows])).toEqual([[40, 30]])
})

test('a terminal that ends says why', async () => {
  const { core, r, s } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  core.finish(r.terminal, 'exited')
  await settle()
  expect(s.state.get()).toBe('ended')
  expect(s.why.get()).toBe('The program exited.')
})

test('after the line comes back the terminal attaches again as resumed', async () => {
  const { cores, r, s, store, timers } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  // The second core plays the same computer after the reconnect: it has the terminal.
  cores[1].terms.set(r.terminal, { ...cores[0].terms.get(r.terminal)! })
  cores[1].docs = cores[0].docs
  cores[0].drop('network')
  await settle()
  expect(s.state.get()).toBe('lost')
  timers.shift()!()
  await settle(30)
  expect(store.conn.get()).toBe('ready')
  const again = cores[1].calls.find((c) => c.method === 'terminal.attach')
  expect(again?.params.resumed).toBe(true)
  expect(s.state.get()).toBe('live')
})

// herdr-core, measured 2026-11-06 with two viewers: attaching as resumed leaves the size with the other viewer, and focus
// takes it. A phone in use that kept the Mac's 33 columns after a dropped line wrapped everything at them.
async function backAfterLoss(before: (store: Store) => void) {
  const { cores, r, s, store, timers } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 48, rows: 40 }))
  await settle()
  before(store)
  await settle()
  cores[1].terms.set(r.terminal, { ...cores[0].terms.get(r.terminal)! })
  cores[1].docs = cores[0].docs
  cores[0].drop('network')
  await settle()
  timers.shift()!()
  await settle(30)
  expect(store.conn.get()).toBe('ready')
  return cores[1].focused.length
}

test('a terminal on the phone screen takes the size back when the line returns, touched or not', async () => {
  expect(await backAfterLoss(() => {})).toBe(1)
})

// The phone in a pocket loses its line and gets it back by itself, as when the computer's program restarts after an update.
// Left with the computer's width, everything the agent wrote meanwhile stayed that narrow in the phone's history.
test('a phone with the app in the background keeps its width when the line returns', async () => {
  expect(await backAfterLoss((store) => { store.appActive = false })).toBe(1)
})

test('a terminal only watched takes no width when the line returns', async () => {
  const { cores, r, store, timers } = await setup()
  const posted: any[] = []
  const w = new TerminalSession(store, r.terminal, (m) => posted.push(JSON.parse(m)), (f) => { queueMicrotask(f); return 1 }, { observe: true })
  w.page(JSON.stringify({ t: 'ready', cols: 48, rows: 40 }))
  await settle()
  cores[1].terms.set(r.terminal, { ...cores[0].terms.get(r.terminal)! })
  cores[1].docs = cores[0].docs
  cores[0].drop('network')
  await settle()
  timers.shift()!()
  await settle(30)
  expect(cores[1].focused.length).toBe(0)
})

test('copy asks the page for its text and gives it back; paste goes through the page so bracketed paste holds', async () => {
  const { s, posted } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  const copied = s.copy()
  expect(posted.at(-1)).toEqual({ t: 'copy' })
  s.page(JSON.stringify({ t: 'copied', text: 'on screen' }))
  expect(await copied).toBe('on screen')
  s.paste('echo pasted')
  expect(posted.at(-1)).toEqual({ t: 'paste', data: 'echo pasted' })
})

test('peeking watches without taking the size: observe on attach, no resize after', async () => {
  const { core, store, r } = await setup()
  const posted: any[] = []
  const s = new TerminalSession(store, r.terminal, (m) => posted.push(m), (f) => { queueMicrotask(f); return 1 }, { observe: true })
  s.page(JSON.stringify({ t: 'ready', cols: 50, rows: 20 }))
  await settle()
  expect(core.calls.filter((c) => c.method === 'terminal.attach').at(-1)!.params.observe).toBe(true)
  s.page(JSON.stringify({ t: 'size', cols: 40, rows: 10 }))
  await settle()
  expect(core.resized).toEqual([])
})

test("the page's gestures reach the app: flick, link, selection and tap", async () => {
  const { store, r } = await setup()
  const seen: any[] = []
  const s = new TerminalSession(store, r.terminal, () => {}, (f) => { queueMicrotask(f); return 1 }, { on: {
    swipe: (d) => seen.push(['swipe', d]), link: (u) => seen.push(['link', u]),
    selected: (t) => seen.push(['selected', t]), tap: () => seen.push(['tap']) } })
  for (const m of [{ t: 'swipe', dir: 'prev' }, { t: 'link', url: 'https://example.com' }, { t: 'selected', text: 'abc' }, { t: 'tap' }])
    s.page(JSON.stringify(m))
  expect(seen).toEqual([['swipe', 'prev'], ['link', 'https://example.com'], ['selected', 'abc'], ['tap']])
})

test('scrolled back, the page says so; latest takes it to the bottom', async () => {
  const { s, posted } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  expect(s.away.get()).toBe(false)
  s.page(JSON.stringify({ t: 'away', away: true }))
  expect(s.away.get()).toBe(true)
  s.bottom()
  await settle()
  expect(posted.at(-1)).toEqual({ t: 'bottom' })
})

test('opened again, the line after what you last saw is marked new, with when you looked; leaving keeps what you saw', async () => {
  const { store, r, s, posted } = await setup()
  const at = Date.now() - 60_000
  store.saw(r.terminal, ['$ npm test', 'PASS a'], at)
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  // The page has drawn the replayed history: the session asks for its text, then marks the first new line.
  s.page(JSON.stringify({ t: 'tail', lines: ['$ git status', 'On branch main'] }))
  await settle()
  const asked = () => posted.filter((m) => m.t === 'text' || m.t === 'mark')
  expect(asked()).toEqual([{ t: 'text' }])
  s.page(JSON.stringify({ t: 'text', base: 10, lines: ['$ npm test', 'PASS a', '$ git status', 'On branch main'] }))
  await settle()
  expect(asked()).toEqual([{ t: 'text' }, { t: 'mark', line: 12, label: sinceLabel(at, Date.now()) }])
  // Only once per opening.
  s.page(JSON.stringify({ t: 'tail', lines: ['$ git status', 'On branch main', '$ ls'] }))
  await settle()
  expect(posted.filter((m) => m.t === 'text').length).toBe(1)
  s.dispose()
  expect(store.lastSeen(r.terminal)?.tail).toEqual(['$ git status', 'On branch main', '$ ls'])
})

test('a pane never looked at before gets no mark', async () => {
  const { s, posted } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  s.page(JSON.stringify({ t: 'tail', lines: ['x'] }))
  await settle()
  expect(posted.filter((m) => m.t === 'text' || m.t === 'mark').length).toBe(0)
})

test("attaching tells herdr-core the phone's colours, so a program that asks gets the phone's theme", async () => {
  const { core, s } = await setup()
  s.setTheme(true)
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  const at = core.calls.find((c) => c.method === 'terminal.attach')!.params
  expect(at.display).toEqual({ fg: '#d2d4d9', bg: '#17191d', cursor: '#d2d4d9' })
})

test('switching the theme while attached attaches again with the new colours', async () => {
  const { core, s } = await setup()
  s.setTheme(true)
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  s.setTheme(true)
  await settle()
  expect(core.calls.filter((c) => c.method === 'terminal.attach').length).toBe(1)
  s.setTheme(false)
  await settle()
  const attaches = core.calls.filter((c) => c.method === 'terminal.attach')
  expect(attaches.length).toBe(2)
  expect(attaches[1].params.display).toEqual({ fg: '#25272c', bg: '#ffffff', cursor: '#25272c' })
})

// History width (2026-11-06): a phone that left a terminal gave its width back to the computer's idle 32-column split,
// so whatever Claude Code printed meanwhile stayed 32 columns wide in the history.
test("leaving a terminal keeps the phone's width on it until someone uses another viewer", async () => {
  const { core, r, s } = await setup()
  const computer = core.view(r.terminal, 32, 30)
  s.page(JSON.stringify({ t: 'ready', cols: 54, rows: 37 }))
  await settle()
  expect(core.sizeOf(r.terminal)).toEqual({ cols: 54, rows: 37 })
  s.dispose()
  await settle()
  expect(core.sizeOf(r.terminal)).toEqual({ cols: 54, rows: 37 })
  computer.use()
  expect(core.sizeOf(r.terminal)).toEqual({ cols: 32, rows: 30 })
})

test('opening a terminal again leaves the phone one attachment to it, and a left one shows nothing', async () => {
  const { core, store, r, s, posted } = await setup()
  s.page(JSON.stringify({ t: 'ready', cols: 54, rows: 37 }))
  await settle()
  s.dispose()
  await settle()
  const before = posted.length
  core.output(r.terminal, 'meanwhile')
  await settle()
  expect(posted.length).toBe(before)
  const again = new TerminalSession(store, r.terminal, () => {}, (f) => { queueMicrotask(f); return 1 })
  again.page(JSON.stringify({ t: 'ready', cols: 54, rows: 37 }))
  await settle()
  expect(core.viewers(r.terminal)).toBe(1)
  expect(core.sizeOf(r.terminal)).toEqual({ cols: 54, rows: 37 })
})

test('the phone keeps hold of the last few terminals it left, not all of them', async () => {
  const { core, store } = await setup()
  const left: string[] = []
  for (let i = 0; i < 7; i++) {
    const r = await store.newSpace({})
    const s = new TerminalSession(store, r.terminal, () => {}, (f) => { queueMicrotask(f); return 1 })
    s.page(JSON.stringify({ t: 'ready', cols: 54, rows: 37 }))
    await settle()
    s.dispose()
    await settle()
    left.push(r.terminal)
  }
  expect(left.map((a) => core.viewers(a))).toEqual([0, 1, 1, 1, 1, 1, 1])
})
test('the page having the typing focus is passed on, so the app can ask the system for the keyboard', async () => {
  let asked = 0
  const s = new TerminalSession((await setup()).store, 't1', () => {}, (f) => { queueMicrotask(f); return 1 }, { on: { focused: () => { asked++ } } })
  s.page(JSON.stringify({ t: 'focused' }))
  expect(asked).toBe(1)
})

// A page whose process crashed never says ready (the iOS 26.3 simulator's JavaScriptCore, see PAGE_READY_MS), and the
// WebView says nothing about it: without this the terminal said "Connecting…" for good.
async function pageSetup() {
  const store = new Store({ pipe: () => new FakeCore(), setInterval: (() => 0) as any, clearInterval: (() => {}) as any, setTimeout: (() => 0) as any })
  await store.connect()
  const r = await store.newSpace({})
  const late: { f: () => void; ms: number }[] = []
  let reloads = 0
  const s = new TerminalSession(store, r.terminal, () => {}, (f) => { queueMicrotask(f); return 1 },
    { reload: () => { reloads++ }, pageTimer: (f, ms) => { late.push({ f, ms }) } })
  return { s, late, reloads: () => reloads, fire: () => late.shift()!.f() }
}

test('a page that never says ready is loaded again once, then the terminal says so and offers to try again', async () => {
  const { s, late, reloads, fire } = await pageSetup()
  expect(late.map((l) => l.ms)).toEqual([PAGE_READY_MS])
  fire()
  expect([reloads(), s.state.get()]).toEqual([1, 'waiting'])
  s.pageLost() // the reloaded page starts loading: its wait starts again
  expect(late.length).toBe(2)
  late.shift()
  fire()
  expect([reloads(), s.state.get(), s.why.get()]).toEqual([1, 'failed', t('terminalPageFailed')])
  s.retry()
  expect([reloads(), s.state.get(), s.why.get()]).toEqual([2, 'waiting', ''])
  fire()
  expect([reloads(), s.state.get()]).toEqual([2, 'failed'])
  s.retry()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  fire()
  expect([reloads(), s.state.get()]).toEqual([3, 'live'])
})

test('a page that says ready in time is left alone', async () => {
  const { s, reloads, fire } = await pageSetup()
  s.page(JSON.stringify({ t: 'ready', cols: 80, rows: 24 }))
  await settle()
  fire()
  expect([reloads(), s.state.get()]).toEqual([0, 'live'])
})

test('a left terminal does not load its page again', async () => {
  const { s, reloads, fire } = await pageSetup()
  s.dispose()
  fire()
  expect([reloads(), s.state.get()]).toEqual([0, 'waiting'])
})

test('the keyboard asked for before the page is ready comes up once it is (the switch tapped right as a pane opens)', async () => {
  const { s, posted } = await setup()
  s.focus()
  await new Promise((r) => setTimeout(r, 0))
  expect(posted.filter((m) => m.t === 'focus').length).toBe(0)
  s.page(JSON.stringify({ t: 'ready', cols: 54, rows: 37 }))
  await new Promise((r) => setTimeout(r, 0))
  expect(posted.filter((m) => m.t === 'focus').length).toBe(1)
  s.page(JSON.stringify({ t: 'ready', cols: 54, rows: 37 }))
  await new Promise((r) => setTimeout(r, 0))
  expect(posted.filter((m) => m.t === 'focus').length).toBe(1)
})
