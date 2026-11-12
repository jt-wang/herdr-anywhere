import { expect, test } from 'nsh/test'
import { Store } from './store.ts'
import { t as tr } from './strings/index.ts'
import { FakeCore, settle } from './testing/fakecore.ts'
import { panes, terminalOf } from './model/layout.ts'
import { spaceActiveTab } from './model/model.ts'

/** Waits for a condition the store reaches on its own timers, up to 5 s: a fixed wait failed while the computer was busy. */
async function until(ok: () => boolean) {
  for (let i = 0; i < 250 && !ok(); i++) await new Promise((f) => setTimeout(f, 20))
}

function setup(core = new FakeCore()) {
  let now = 1_000_000
  const timers: (() => void)[] = []
  const saved = new Map<string, string>()
  const store = new Store({
    pipe: () => core,
    setInterval: (() => 0) as any,
    clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any,
    now: () => now,
    load: async (k) => saved.get(k) ?? null,
    save: (k, v) => saved.set(k, v),
  })
  return { core, store, timers, saved, advance: (ms: number) => { now += ms } }
}

test('connecting reads the environment, the machines and an empty layout', async () => {
  const { core, store } = setup()
  await store.connect()
  expect(store.conn.get()).toBe('ready')
  expect(store.home).toBe('/Users/sam')
  expect(store.shell).toBe('/bin/zsh')
  expect(core.calls.map((c) => c.method).slice(0, 4)).toEqual(['events.subscribe', 'machine.exec', 'machine.list', 'doc.get'])
  expect(store.model.spaces).toEqual([])
})

test('a new space starts a kept, labeled terminal and writes the shared layout', async () => {
  const { core, store } = setup()
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'], task: 'fix the login bug' })
  await settle()
  const run = core.calls.find((c) => c.method === 'terminal.run')!.params
  expect(run.keep).toBe(true)
  expect(run.env).toEqual({ TERM: 'xterm-256color', COLORTERM: 'truecolor', TERM_PROGRAM: 'herdr' })
  expect(run.labels).toEqual({ 'herdr.space': r.space, 'herdr.tab': r.tab })
  expect(run.argv).toEqual(['/bin/zsh', '-ic', '"$@"; exec "$0" -i', '/bin/zsh', 'claude', 'fix the login bug'])
  const doc = core.doc()
  expect(doc.spaces.length).toBe(1)
  expect(doc.spaces[0].id).toBe(r.space)
  expect(doc.spaces[0].folder).toBe('/Users/sam')
  expect(doc.spaces[0].tabs[0].root.children[0].content).toEqual({ terminal: r.terminal })
  expect(r.space.startsWith('sp') && r.tab.startsWith('tb') && r.pane.startsWith('p')).toBe(true)
})

test('a layout written by another screen replaces ours, keeping our focus where it still applies', async () => {
  const { core, store } = setup()
  await store.connect()
  const a = await store.newSpace({})
  const b = await store.splitPane(a.pane, 'right')
  store.focusPane(a.pane)
  await settle()
  const doc = core.doc()
  doc.spaces[0].name = 'renamed elsewhere'
  core.writeDoc(doc)
  await settle(10)
  expect(store.model.spaces[0].name).toBe('renamed elsewhere')
  expect(store.model.focusedPane()).toBe(a.pane)
  expect(panes(store.model.tabs()[0].root).map((n) => n.id)).toEqual([a.pane, b.pane])
})

test('a stale write loses to the other screen and says so', async () => {
  const { core, store } = setup()
  await store.connect()
  await store.newSpace({ name: 'mine' })
  await settle()
  // Another screen changes the document; our next write is based on the old version.
  const other = JSON.parse(JSON.stringify(core.doc()))
  other.spaces[0].name = 'theirs'
  const d = core.docs.get('')!
  core.docs.set('', { value: other, version: d.version + 1 })
  store.renameSpace(store.model.spaces[0].id, 'mine again')
  await settle(10)
  expect(store.model.spaces[0].name).toBe('theirs')
  expect(store.toasts.get().map((t) => t.title)).toEqual(['Layout changed elsewhere'])
})

test('a labeled terminal started elsewhere gets a pane, and is kept', async () => {
  const { core, store } = setup()
  await store.connect()
  const t = core.run({ argv: ['codex'], labels: { 'herdr.space': 'spX', 'herdr.tab': 'tbX' }, cwd: '/src/api' })
  await settle(10)
  const s = store.model.space('spX')!
  expect(s.folder).toBe('/src/api')
  expect(s.tabs[0].id).toBe('tbX')
  expect(store.model.paneOf(t.address) !== undefined).toBe(true)
  expect(core.terms.get(t.address)!.keep).toBe(true)
  await settle(5)
  expect(core.doc().spaces[0].id).toBe('spX')
})

test('needing you rings, finishing unseen rings, the first listing does not', async () => {
  const core = new FakeCore()
  const pre = core.run({ argv: ['claude'], labels: { 'herdr.space': 's0', 'herdr.tab': 'c0' } })
  core.setStatus(pre.address, 'blocked')
  core.docs.set('', { version: 1, value: { spaces: [{ id: 's0', name: '', folder: '/x', tabs: [{ id: 'c0', name: '', root: { id: 'c0', type: 'row', children: [{ id: 'x0', type: 'block', content: { terminal: pre.address } }] } }] }] } })
  const { store } = setup(core)
  const rung: string[] = []
  store.onAlert = (k, t) => rung.push(`${k}:${t.title}`)
  await store.connect()
  expect(rung).toEqual([])
  const r = await store.newSpace({ argv: ['claude'] })
  await settle()
  core.setStatus(r.terminal, 'working')
  await settle()
  core.setStatus(r.terminal, 'blocked')
  await settle()
  core.setStatus(r.terminal, 'working')
  await settle()
  core.setStatus(r.terminal, 'idle')
  await settle()
  expect(rung).toEqual(['blocked:Claude Code needs you', 'done:Claude Code finished'])
  expect(store.statusOf(r.terminal)).toBe('done')
  // Looking at it makes it idle.
  store.viewingTab = true
  store.sync()
  expect(store.statusOf(r.terminal)).toBe('idle')
})

test('closing a pane ends its terminal; a pane whose terminal ended goes away', async () => {
  const { core, store, advance } = setup()
  await store.connect()
  const a = await store.newSpace({})
  const b = await store.splitPane(a.pane, 'down')
  await settle()
  store.closePane(b.pane)
  await settle()
  expect(core.terms.get(b.terminal)!.state).toBe('finished')
  expect(panes(store.model.tabs()[0].root).length).toBe(1)
  const c = await store.splitPane(a.pane, 'right')
  core.finish(c.terminal)
  advance(6000)
  await settle(10)
  store.sync()
  await settle(10)
  expect(panes(store.model.tabs()[0].root).map((n) => terminalOf(n))).toEqual([a.terminal])
})

test('closing the last tab closes its space; the agents list follows status', async () => {
  const { core, store } = setup()
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'] })
  core.setStatus(r.terminal, 'working')
  await settle()
  expect(store.agents().map((a) => [a.address, a.status, a.programName])).toEqual([[r.terminal, 'working', 'Claude Code']])
  expect(store.counts()).toEqual({ agents: 1, needsYou: 0 })
  store.closeTab(r.tab)
  await settle()
  expect(store.model.spaces).toEqual([])
  expect(core.doc()).toEqual({ spaces: [] })
})

test('a new tab asked for while the line is down starts once it is back, and asks it to hurry', async () => {
  const cores = [new FakeCore(), new FakeCore()]
  let n = 0
  const store = new Store({ pipe: () => cores[n++], setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: (() => 0) as any })
  await store.connect()
  await store.newSpace({ name: 'mine' })
  await settle()
  const space = store.model.spaces[0].id
  const before = store.model.spaces[0].tabs.length
  cores[1].docs = cores[0].docs
  cores[0].drop('network')
  await settle()
  expect(store.conn.get()).toBe('reconnecting')
  await store.newTab({ space })
  expect(store.conn.get()).toBe('ready')
  expect(cores[1].calls.some((c) => c.method === 'terminal.run')).toBe(true)
  expect(store.model.spaces[0].tabs.length).toBe(before + 1)
})

test('a new tab that the line never comes back for fails as not connected', async () => {
  const fired: (() => void)[] = []
  const core = new FakeCore()
  let first = true
  const store = new Store({
    pipe: () => { if (!first) throw new Error('no route'); first = false; return core },
    setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { fired.push(f); return fired.length }) as any,
    now: () => 1_000_000,
  })
  await store.connect()
  await store.newSpace({ name: 'mine' })
  await settle()
  const space = store.model.spaces[0].id
  core.drop('network')
  await settle()
  let failed = ''
  let finished = false
  const asked = store.newTab({ space }).catch((e) => { failed = e.kind }).finally(() => { finished = true })
  await settle()
  expect(finished).toBe(false)
  for (const f of fired.splice(0)) f()
  await asked
  expect(failed).toBe('disconnected')
})

test('losing the line reconnects and resubscribes', async () => {
  const cores = [new FakeCore(), new FakeCore()]
  let n = 0
  const timers: (() => void)[] = []
  const store = new Store({ pipe: () => cores[n++], setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any })
  await store.connect()
  cores[0].drop('network')
  await settle()
  expect(store.conn.get()).toBe('reconnecting')
  timers.shift()!()
  await settle(20)
  expect(store.conn.get()).toBe('ready')
  expect(cores[1].calls.some((c) => c.method === 'events.subscribe')).toBe(true)
})

test("a computer that refuses at once has Herdr's companion stopped; one that never answers is asleep or away", async () => {
  let now = 1_000_000
  let mode: 'refuse' | 'silent' = 'refuse'
  const pipe = () => {
    const c = new FakeCore()
    // Refused: the socket closes at once. Silent: it closes after the connect times out.
    c.send = () => { if (mode === 'silent') now += 20_000; c.drop('network') }
    return c
  }
  const timers: (() => void)[] = []
  const store = new Store({ pipe, now: () => now, setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any })
  await store.connect().catch(() => {})
  await settle()
  expect(store.conn.get()).toBe('reconnecting')
  expect(store.lostWhy.get()).toBe('refused')
  // The banner puts what to run under it (restart.ts), as for a tunnel left unanswered.
  expect(store.error.get()).toBe("Can't reach Companion on your computer. Run this on it:")
  mode = 'silent'
  timers.shift()!()
  await settle(20)
  expect(store.error.get()).toBe("The computer doesn't answer: it may be asleep, off, or on another network. Trying again…")
})

test("a retry that the tunnel leaves unanswered keeps saying so: the failed retry doesn't turn it into 'asleep or away'", async () => {
  // As measured over a real tunnel to a stopped Companion: each retry's socket ends unanswered after the tunnel's 10 s.
  let now = 1_000_000
  let mode: 'up' | 'silent' = 'up'
  const core = new FakeCore()
  const pipe = () => {
    if (mode === 'up') return core
    const c = new FakeCore()
    c.send = () => { now += 10_000; c.drop('no-answer') }
    return c
  }
  const timers: (() => void)[] = []
  const store = new Store({ pipe, now: () => now, setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any })
  await store.connect()
  mode = 'silent'
  core.drop('network')
  await settle()
  for (let i = 0; i < 3; i++) {
    timers.shift()!()
    await settle(20)
    expect(store.lostWhy.get()).toBe('no-answer')
    expect(store.error.get()).toBe("Your computer doesn't answer. Run this on it:")
  }
})

test('while the line is down, the state shown says how old it is', async () => {
  let now = 1_000_000
  const cores = [new FakeCore(), new FakeCore()]
  let n = 0
  const store = new Store({ pipe: () => cores[n++], now: () => now, setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: (() => 0) as any })
  await store.connect()
  expect(store.staleFor()).toBe(undefined)
  cores[0].drop('network')
  await settle()
  now += 4 * 60_000
  expect(store.staleFor()).toBe(4 * 60_000)
})

test('a new space or tab is placed once, though herdr-core reports its terminal before terminal.run answers', async () => {
  const { core, store } = setup()
  await store.connect()
  core.holding.add('terminal.run')
  const made = store.newSpace({ folder: '/Users/sam/src/api' })
  await settle(20)
  core.release()
  const r = await made
  await settle(20)
  expect(store.model.spaces.filter((p) => p.id === r.space).length).toBe(1)
  const tab = store.newTab({ space: r.space })
  await settle(20)
  core.release()
  const t = await tab
  await settle(20)
  expect(store.model.space(r.space)!.tabs.filter((c) => c.id === t.tab).length).toBe(1)
  expect(store.model.space(r.space)!.tabs.length).toBe(2)
})

test('a sub-agent kept from peek becomes a tab of its own beside its parent', async () => {
  const { core, store } = setup()
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'] })
  const kid = core.run({ argv: ['codex'], parent: r.terminal })
  await settle()
  await store.keepTerminal(kid.address)
  await settle()
  const s = store.model.space(r.space)!
  expect(s.tabs.length).toBe(2)
  expect(core.terms.get(kid.address)!.labels['herdr.space']).toBe(r.space)
  expect(store.model.paneOf(kid.address) !== undefined).toBe(true)
})

test('a computer that forgot this phone stops the retries and asks to pair again, connected or not', async () => {
  const { core, store, timers } = setup()
  await store.connect()
  core.drop('unpaired')
  await settle()
  expect(store.conn.get()).toBe('unpaired')
  expect(store.error.get()).toBe('This computer no longer knows this phone. Pair it again.')
  expect(timers.length).toBe(0)
  // Refused while saying hello: the same.
  const refusing = new FakeCore()
  refusing.send = () => refusing.drop('unpaired')
  const s2 = setup(refusing)
  await s2.store.connect().catch(() => {})
  await settle()
  expect(s2.store.conn.get()).toBe('unpaired')
  expect(s2.timers.length).toBe(0)
})

test('losing the line says what went wrong in words, and keeps retrying', async () => {
  const words: string[] = []
  for (const reason of ['no-core', 'network', 'binary-unsupported']) {
    const { core, store, timers } = setup()
    await store.connect()
    core.drop(reason)
    await settle()
    words.push(store.error.get())
    expect(timers.length).toBe(1)
  }
  expect(words).toEqual([
    "herdr-core isn't installed on the computer. Install it there; this reconnects by itself.",
    'Lost the computer. Reconnecting…',
    'Switching to a slower connection…',
  ])
})

test('alerts the person turned off neither ring nor show', async () => {
  const { core, store } = setup()
  const rung: string[] = []
  store.onAlert = (k) => rung.push(k)
  store.wants = (k) => k !== 'blocked'
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'] })
  await settle()
  for (const st of ['working', 'blocked', 'working', 'idle']) { core.setStatus(r.terminal, st); await settle() }
  expect(rung).toEqual(['done'])
  expect(store.toasts.get().map((t) => t.title)).toEqual(['Claude Code finished'])
})

test("a pane's folder shows its git branch and how far it is ahead and behind", async () => {
  const core = new FakeCore()
  core.git.set('/src/api', '# branch.oid 1\n# branch.head feature/login\n# branch.ab +2 -1\n')
  const { store } = setup(core)
  await store.connect()
  const r = await store.newSpace({ folder: '/src/api' })
  store.sync()
  await settle(10)
  expect(store.model.folderInfo.get(r.terminal)).toEqual({ top: '/src/api', branch: 'feature/login', ahead: 2, behind: 1 })
})

test('a second terminal in a folder looked up a moment ago gets the branch too, without asking git again', async () => {
  const core = new FakeCore()
  core.git.set('/src/api', '# branch.oid 1\n# branch.head feature/login\n')
  const { store } = setup(core)
  await store.connect()
  const a = await store.newSpace({ folder: '/src/api' })
  store.sync()
  await settle(10)
  const b = await store.newSpace({ folder: '/src/api' })
  store.sync()
  await settle(10)
  expect(store.model.folderInfo.get(a.terminal)?.branch).toBe('feature/login')
  expect(store.model.folderInfo.get(b.terminal)?.branch).toBe('feature/login')
  expect(core.calls.filter((c) => c.method === 'machine.exec' && c.params.line.startsWith('git ')).length).toBe(1)
})

test("a blocked agent's toast and its agents row say what it asks", async () => {
  const { core, store } = setup()
  const toasts: string[] = []
  store.onAlert = (_k, t) => toasts.push(`${t.title} | ${t.body}`)
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'], folder: '/Users/sam/src/demo-app' })
  await settle()
  core.emit('title', r.terminal, { title: '✳ Hello.txt file creation' })
  core.setStatus(r.terminal, 'working')
  await settle()
  core.setStatus(r.terminal, 'blocked', 'claude-code', { kind: 'permission', msg: 'approve Write: /Users/sam/src/demo-app/hello.txt' })
  await settle()
  // Named by its title, as its row is, in quotes.
  expect(toasts).toEqual(['“Hello.txt file creation” needs you | Write: /Users/sam/src/demo-app/hello.txt\ndemo-app › 1'])
  const a = store.agents()[0]
  expect([a.kind, a.msg, a.named]).toEqual(['permission', 'approve Write: /Users/sam/src/demo-app/hello.txt', false])
  expect(store.agentsView().detail.get(r.terminal)).toBe('Hello.txt file creation')
})

test("a finished agent's alert says what it did, then where it is", async () => {
  const { core, store } = setup()
  const toasts: string[] = []
  store.onAlert = (_k, t) => toasts.push(`${t.title} | ${t.body}`)
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'], folder: '/Users/sam/src/demo-app' })
  await settle()
  core.emit('title', r.terminal, { title: '✳ Hello.txt file creation' })
  core.setStatus(r.terminal, 'working')
  await settle()
  core.setStatus(r.terminal, 'done')
  await settle()
  core.setStatus(r.terminal, 'working')
  await settle()
  core.setStatus(r.terminal, 'done', 'claude-code', { msg: 'Wrote hello.txt' })
  await settle()
  // Named by its title; the body doesn't say the title again.
  expect(toasts).toEqual(['“Hello.txt file creation” finished | demo-app › 1', '“Hello.txt file creation” finished | Wrote hello.txt\ndemo-app › 1'])
})

// Claude Code 2.1.319, measured in a scratch herdr-core session (hc events): status done with
// no msg, then the turn, then done again with msg "replied 'the sky is blue.' with no tools, as asked".
test('a finished alert is told again, quietly, when the agent says what it did a moment later', async () => {
  const { core, store } = setup()
  const rung: string[] = []
  store.onAlert = (k, t, again) => rung.push(`${again ? 'again ' : ''}${k}: ${t.body}`)
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'], folder: '/Users/sam/src/demo-app' })
  await settle()
  core.emit('title', r.terminal, { title: '✳ Sky color reply' })
  core.setStatus(r.terminal, 'working')
  await settle()
  core.setStatus(r.terminal, 'done')
  await settle()
  core.setStatus(r.terminal, 'done', 'claude-code', { msg: "replied 'the sky is blue.'" })
  await settle()
  core.setStatus(r.terminal, 'done', 'claude-code', { msg: "replied 'the sky is blue.'" })
  await settle()
  expect(rung).toEqual(['done: demo-app › 1', "again done: replied 'the sky is blue.'\ndemo-app › 1"])
})

// herdr-core 0.1.20, measured with hc events: a script that reports done with words and then exits is followed by
// status null 0 to 440 ms later, as its shell takes the terminal back. When both lines come in one read (a slow phone,
// a busy line), the words were never looked at: the alert kept only where it is.
test('words reported just before the status clears still reach the alert', async () => {
  const { core, store } = setup()
  const rung: string[] = []
  store.onAlert = (k, t, again) => rung.push(`${again ? 'again ' : ''}${k}: ${t.body}`)
  await store.connect()
  const r = await store.newSpace({ argv: ['sh', 'done.sh'], folder: '/Users/sam/src/demo-app' })
  await settle()
  core.setStatus(r.terminal, 'working', null)
  await settle()
  core.setStatus(r.terminal, 'done', null)
  await settle()
  core.setStatus(r.terminal, 'done', null, { msg: 'Ran the tests' })
  core.emit('status', r.terminal, { status: null, records: {}, reported: true })
  await settle()
  expect(rung).toEqual(['done: demo-app › 1', 'again done: Ran the tests\ndemo-app › 1'])
})

test('a finish reported with its words just before the status clears still rings, with the words', async () => {
  const { core, store } = setup()
  const rung: string[] = []
  store.onAlert = (k, t, again) => rung.push(`${again ? 'again ' : ''}${k}: ${t.body}`)
  await store.connect()
  const r = await store.newSpace({ argv: ['sh', 'done.sh'], folder: '/Users/sam/src/demo-app' })
  await settle()
  core.setStatus(r.terminal, 'working', null)
  await settle()
  core.setStatus(r.terminal, 'done', null, { msg: 'Ran the tests' })
  core.emit('status', r.terminal, { status: null, records: {}, reported: true })
  await settle()
  expect(rung).toEqual(['done: Ran the tests\ndemo-app › 1'])
})

test('words that come after the agent moved on are not put on the old alert', async () => {
  const { core, store } = setup()
  const rung: string[] = []
  store.onAlert = (k, t, again) => rung.push(`${again ? 'again ' : ''}${k}: ${t.body}`)
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'], folder: '/Users/sam/src/demo-app' })
  await settle()
  core.setStatus(r.terminal, 'working')
  await settle()
  core.setStatus(r.terminal, 'done')
  await settle()
  core.setStatus(r.terminal, 'working')
  await settle()
  core.setStatus(r.terminal, 'working', 'claude-code', { msg: 'reading files' })
  await settle()
  expect(rung.filter((x) => x.startsWith('again'))).toEqual([])
})

test("any program that reports its state gets its words in the alert; a shell's title stays out", async () => {
  const { core, store } = setup()
  const toasts: string[] = []
  store.onAlert = (_k, t) => toasts.push(`${t.title} | ${t.body}`)
  await store.connect()
  const r = await store.newSpace({ argv: ['sh', 'ask.sh'], folder: '/Users/sam/src/demo-app' })
  await settle()
  core.emit('title', r.terminal, { title: 'sam@laptop:~/src/demo-app' })
  core.setStatus(r.terminal, 'working', null)
  await settle()
  core.setStatus(r.terminal, 'blocked', null, { kind: 'question', msg: 'Which port?' })
  await settle()
  core.setStatus(r.terminal, 'working', null)
  await settle()
  core.setStatus(r.terminal, 'done', null)
  await settle()
  core.setStatus(r.terminal, 'working', null)
  await settle()
  core.setStatus(r.terminal, 'done', null, { msg: 'Ran the tests' })
  await settle()
  expect(toasts.map((t) => t.split(' | ')[1])).toEqual(['Which port?\ndemo-app › 1', 'demo-app › 1', 'Ran the tests\ndemo-app › 1'])
})

test("answering a blocked agent presses its prompt's keys in its terminal; a reply then types the text", async () => {
  const { core, store } = setup()
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'] })
  await settle()
  await store.answer(r.terminal, { keys: ['1'] })
  expect(core.calls.filter((c) => c.method === 'terminal.keys').map((c) => c.params)).toEqual([{ terminal: r.terminal, keys: ['1'] }])
  await store.answer(r.terminal, { keys: ['3'] }, 'green')
  expect(core.calls.filter((c) => c.method.startsWith('terminal.keys') || c.method === 'terminal.send').slice(1).map((c) => [c.method, c.params])).toEqual([
    ['terminal.keys', { terminal: r.terminal, keys: ['3'] }],
    ['terminal.send', { terminal: r.terminal, text: 'green', force: true }],
  ])
})

test("a pane's header names a shell or an agent by its program, as the desktop's does, whatever its title", async () => {
  const { core, store } = setup()
  await store.connect()
  const a = await store.newSpace({})
  const b = await store.newSpace({ argv: ['claude'] })
  core.emit('title', a.terminal, { title: 'sam@studio:~/src/api' })
  core.setStatus(b.terminal, 'working')
  await settle()
  expect(store.paneName(a.terminal)).toBe('zsh')
  expect(store.paneName(b.terminal)).toBe('Claude Code')
})

test("a flick across the terminal goes to the space's next or previous tab, and stops at either end", async () => {
  const { store } = setup()
  await store.connect()
  const a = await store.newSpace({})
  const b = await store.newTab({ space: a.space })
  const c = await store.newTab({ space: a.space })
  const active = () => spaceActiveTab(store.model.space(a.space)!)?.id
  store.selectTab(b.tab)
  store.stepTab(a.space, 'next')
  expect(active()).toBe(c.tab)
  store.stepTab(a.space, 'next')
  expect(active()).toBe(c.tab)
  store.stepTab(a.space, 'prev')
  store.stepTab(a.space, 'prev')
  expect(active()).toBe(a.tab)
})

// herdr-core (0.1.20) answers terminal.send once a person sends or clears what they typed in that terminal, up to 60 s
// after their last key, and has no way to ask how long. The phone waits for it as long as the line is up: a send that
// timed out on the phone at 10 s still arrived, and sending it again put it there twice.
test('text from the composer waits for herdr-core as long as it takes, and goes once', async () => {
  const core = new FakeCore()
  const store = new Store({ pipe: () => core, setInterval: (() => 0) as any, clearInterval: (() => {}) as any, callTimeoutMs: 20 })
  await store.connect()
  const r = await store.newSpace({})
  core.holding.add('terminal.send')
  let done = 'waiting'
  const sent = store.compose(r.terminal, 'ls').then(() => { done = 'sent' }, (e) => { done = String(e) })
  await new Promise((f) => setTimeout(f, 80))
  expect(done).toBe('waiting')
  core.release()
  await sent
  expect(done).toBe('sent')
  expect(core.calls.filter((c) => c.method === 'terminal.send').map((c) => c.params.text)).toEqual(['ls'])
})

test('text typed while the line was down, sent once it is back, waits for herdr-core as long as it takes too', async () => {
  const cores = [new FakeCore(), new FakeCore()]
  let n = 0
  const timers: (() => void)[] = []
  const store = new Store({ pipe: () => cores[n++], setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any, callTimeoutMs: 20 })
  await store.connect()
  const r = await store.newSpace({})
  cores[1].terms.set(r.terminal, { ...cores[0].terms.get(r.terminal)! })
  cores[1].docs = cores[0].docs
  cores[1].holding.add('terminal.send')
  cores[0].drop('network')
  await settle()
  await store.compose(r.terminal, 'pwd')
  timers.shift()!()
  await settle(30)
  store.sync()
  await new Promise((f) => setTimeout(f, 80))
  expect(store.toasts.get().map((t) => t.title)).toEqual([])
  cores[1].release()
  await settle()
  expect(cores[1].calls.filter((c) => c.method === 'terminal.send').map((c) => c.params.text)).toEqual(['pwd'])
  expect(store.toasts.get().map((t) => t.title)).toEqual([])
})

test('a send the line drops while herdr-core holds it fails as lost and is not sent again', async () => {
  const cores = [new FakeCore(), new FakeCore()]
  let n = 0
  const timers: (() => void)[] = []
  const store = new Store({ pipe: () => cores[n++], setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any })
  await store.connect()
  const r = await store.newSpace({})
  cores[0].holding.add('terminal.send')
  let failed: unknown = null
  const sent = store.compose(r.terminal, 'ls').catch((e) => { failed = e })
  await settle()
  cores[1].terms.set(r.terminal, { ...cores[0].terms.get(r.terminal)! })
  cores[1].docs = cores[0].docs
  cores[0].drop('network')
  await sent
  expect((failed as any)?.kind).toBe('disconnected')
  timers.shift()!()
  await settle(30)
  expect(store.conn.get()).toBe('ready')
  expect(store.pending.get()).toEqual([])
  expect(cores[1].calls.filter((c) => c.method === 'terminal.send')).toEqual([])
})

// How long herdr-core takes says why it holds a send (0.1.20): a program printing all the time held it about 3.2 s, one
// still starting was refused after 5 s, a half-typed line held it 59.2 s; other calls on the same line answered in
// under 2 ms meanwhile. Past a few seconds the store asks herdr-core something else to tell its hold from a slow line.
test('a send herdr-core holds past a few seconds shows as held by the terminal when herdr-core answers the rest at once', async () => {
  const core = new FakeCore()
  const store = new Store({ pipe: () => core, setInterval: (() => 0) as any, clearInterval: (() => {}) as any, slowSendMs: 30 })
  await store.connect()
  const r = await store.newSpace({})
  core.holding.add('terminal.send')
  const sent = store.compose(r.terminal, 'ls')
  await settle()
  expect(store.sending.get().map((s) => [s.address, s.text, s.failed])).toEqual([[r.terminal, 'ls', undefined]])
  await until(() => !!store.sending.get()[0]?.held)
  expect(store.sending.get().map((s) => s.held)).toEqual(['draft'])
  core.release()
  await sent
  expect(store.sending.get()).toEqual([])
})

test('a held send is slow on the line when herdr-core answers nothing else either', async () => {
  const core = new FakeCore()
  const store = new Store({ pipe: () => core, setInterval: (() => 0) as any, clearInterval: (() => {}) as any, slowSendMs: 30 })
  await store.connect()
  const r = await store.newSpace({})
  core.holding.add('terminal.send'); core.holding.add('server.status')
  void store.compose(r.terminal, 'ls')
  await until(() => !!store.sending.get()[0]?.held)
  expect(store.sending.get().map((s) => s.held)).toEqual(['line'])
})

test('a terminal sends one thing at a time: a second send while the first is held is not sent', async () => {
  const core = new FakeCore()
  const store = new Store({ pipe: () => core, setInterval: (() => 0) as any, clearInterval: (() => {}) as any })
  await store.connect()
  const r = await store.newSpace({})
  core.holding.add('terminal.send')
  const first = store.compose(r.terminal, 'ls')
  await settle()
  const again = [store.compose(r.terminal, 'ls'), store.compose(r.terminal, 'pwd')]
  await settle()
  core.release()
  await Promise.all([first, ...again])
  expect(core.calls.filter((c) => c.method === 'terminal.send').map((c) => c.params.text)).toEqual(['ls'])
})

test('a send that fails stays with its text, marked failed, until the input box takes it back', async () => {
  const core = new FakeCore()
  const store = new Store({ pipe: () => core, setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: (() => 0) as any })
  await store.connect()
  const r = await store.newSpace({})
  core.holding.add('terminal.send')
  const sent = store.compose(r.terminal, 'ls').catch(() => {})
  await settle()
  core.drop('network')
  await sent
  expect(store.sending.get().map((s) => [s.text, s.failed])).toEqual([['ls', true]])
  store.forget(r.terminal)
  expect(store.sending.get()).toEqual([])
})

// The app closed (killed, or swiped away) while herdr-core held a send: herdr-core dropped it with its sender (the kill on
// the emulator: nothing arrived when the line was cleared), and the box came back empty, as if it had gone.
test('a send the app closed on comes back failed on the next launch, with its text, and is not sent again', async () => {
  const saved = new Map<string, string>()
  const io = { setInterval: (() => 0) as any, clearInterval: (() => {}) as any, setTimeout: (() => 0) as any,
    load: async (k: string) => saved.get(k) ?? null, save: (k: string, v: string) => { saved.set(k, v) } }
  const core = new FakeCore()
  const first = new Store({ pipe: () => core, ...io })
  await first.connect()
  const r = await first.newSpace({})
  core.holding.add('terminal.send')
  void first.compose(r.terminal, 'ls')
  await settle()
  const again = new FakeCore()
  again.terms.set(r.terminal, { ...core.terms.get(r.terminal)! })
  again.docs = core.docs
  const next = new Store({ pipe: () => again, ...io })
  await next.connect()
  await settle()
  expect(next.sending.get().map((s) => [s.address, s.text, s.failed])).toEqual([[r.terminal, 'ls', true]])
  // The toast's title takes one line, its body three: the title says what happened, the body what to do.
  expect(next.toasts.get().map((t) => [t.title, t.body, t.action?.terminal])).toEqual([[tr('maybeNotSent'), tr('sendClosed'), r.terminal]])
  expect(again.calls.filter((c) => c.method === 'terminal.send')).toEqual([])
  next.forget(r.terminal)
  const last = new FakeCore()
  last.terms.set(r.terminal, { ...core.terms.get(r.terminal)! })
  last.docs = core.docs
  const third = new Store({ pipe: () => last, ...io })
  await third.connect()
  expect(third.sending.get()).toEqual([])
})

test('a send that arrived is not brought back on the next launch', async () => {
  const saved = new Map<string, string>()
  const io = { setInterval: (() => 0) as any, clearInterval: (() => {}) as any, setTimeout: (() => 0) as any,
    load: async (k: string) => saved.get(k) ?? null, save: (k: string, v: string) => { saved.set(k, v) } }
  const core = new FakeCore()
  const first = new Store({ pipe: () => core, ...io })
  await first.connect()
  const r = await first.newSpace({})
  await first.compose(r.terminal, 'ls')
  const again = new FakeCore()
  again.terms.set(r.terminal, { ...core.terms.get(r.terminal)! })
  again.docs = core.docs
  const next = new Store({ pipe: () => again, ...io })
  await next.connect()
  expect(next.sending.get()).toEqual([])
  expect(next.toasts.get()).toEqual([])
})

test('text from the composer goes out with terminal.send; typed while the line is down, it waits as pending and goes once it is back', async () => {
  const cores = [new FakeCore(), new FakeCore()]
  let n = 0
  const timers: (() => void)[] = []
  const store = new Store({ pipe: () => cores[n++], setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any })
  await store.connect()
  const r = await store.newSpace({})
  await store.compose(r.terminal, 'ls')
  expect(cores[0].calls.filter((c) => c.method === 'terminal.send').map((c) => [c.params.text, c.params.force])).toEqual([['ls', true]])
  cores[1].terms.set(r.terminal, { ...cores[0].terms.get(r.terminal)! })
  cores[1].docs = cores[0].docs
  cores[0].drop('network')
  await settle()
  await store.compose(r.terminal, 'pwd')
  expect(store.pending.get()).toEqual([{ address: r.terminal, text: 'pwd' }])
  timers.shift()!()
  await settle(30)
  expect(store.conn.get()).toBe('ready')
  store.sync()
  await settle()
  expect(cores[1].calls.filter((c) => c.method === 'terminal.send').map((c) => c.params.text)).toEqual(['pwd'])
  expect(store.pending.get()).toEqual([])
})

test('a waiting line whose answer is lost is not sent again, and the person is told', async () => {
  const cores = [new FakeCore(), new FakeCore(), new FakeCore()]
  let n = 0
  const timers: (() => void)[] = []
  const store = new Store({ pipe: () => cores[n++], setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any })
  const toasts: string[] = []
  const toast = store.toast.bind(store)
  store.toast = (t) => { toasts.push(t.title); return toast(t) }
  await store.connect()
  const r = await store.newSpace({})
  for (const c of cores.slice(1)) { c.terms.set(r.terminal, { ...cores[0].terms.get(r.terminal)! }); c.docs = cores[0].docs }
  cores[0].drop('network')
  await settle()
  await store.compose(r.terminal, 'pwd')
  // It reaches the computer, and the line goes before the answer comes back.
  cores[1].onSend = () => cores[1].drop('network')
  timers.shift()!()
  await settle(30)
  store.sync()
  await settle()
  expect(cores[1].calls.filter((c) => c.method === 'terminal.send').map((c) => c.params.text)).toEqual(['pwd'])
  expect(store.pending.get()).toEqual([])
  expect(toasts.some((t) => t.includes('pwd'))).toBe(true)
  while (timers.length) timers.shift()!()
  await settle(30)
  store.sync()
  await settle()
  expect(cores[2].calls.filter((c) => c.method === 'terminal.send')).toEqual([])
})

test("a blocked agent's menu is read off its screen once; a screen without one is read again, then left to the keys", async () => {
  const { core, store, timers } = setup()
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'] })
  await settle()
  core.screens.set(r.terminal, ' Do you want to proceed?\n ❯ 1. Yes\n   2. No\n')
  core.setStatus(r.terminal, 'blocked', 'claude-code', { kind: 'permission', msg: 'approve Bash: ls' })
  await settle()
  expect(store.menuFor(r.terminal)?.options.map((o) => o.label)).toEqual(['Yes', 'No'])
  expect(core.calls.filter((c) => c.method === 'terminal.read').length).toBe(1)
  core.setStatus(r.terminal, 'working')
  await settle()
  expect(store.menuFor(r.terminal)).toBe(undefined)
  // Blocked again, the menu not drawn yet: read again a moment later, three times in all.
  core.screens.set(r.terminal, '❯ \n')
  core.setStatus(r.terminal, 'blocked', 'claude-code', { kind: 'permission', msg: 'approve Bash: ls' })
  await settle()
  for (let i = 0; i < 3; i++) { for (const f of timers.splice(0)) f(); await settle() }
  expect(core.calls.filter((c) => c.method === 'terminal.read').length).toBe(4)
  expect(store.menuFor(r.terminal)).toBe(undefined)
  expect(store.menuRead(r.terminal)).toBe(true)
})

// A line that went quiet without closing: the computer gave up on the phone, and the close never reached it.
function quietSetup() {
  const core = new FakeCore()
  let now = 1_000_000
  const ticks: (() => void)[] = []
  const timers: (() => void)[] = []
  const store = new Store({
    pipe: () => core,
    setInterval: ((f: () => void) => { ticks.push(f); return ticks.length }) as any,
    clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void) => { timers.push(f); return 0 }) as any,
    now: () => now,
  })
  /** Time passes with the app running: every second, its timers fire. */
  const run = async (seconds: number) => {
    for (let i = 0; i < seconds; i++) { now += 1000; for (const t of [...ticks]) t(); await settle() }
  }
  return { core, store, timers, run, sleep: (ms: number) => { now += ms } }
}

test('a line that goes quiet without closing is dropped and connected again', async () => {
  const { core, store, run, timers } = quietSetup()
  await store.connect()
  await run(10)
  expect(store.conn.get()).toBe('ready')
  core.mute = true
  await run(10)
  expect(store.conn.get()).toBe('ready')
  await run(10)
  expect(store.conn.get()).toBe('reconnecting')
  expect(timers.length > 0).toBe(true)
})

test('the app being suspended is not a quiet line: it gets the full time again when it wakes', async () => {
  const { store, run, sleep, core } = quietSetup()
  await store.connect()
  await run(2)
  sleep(600_000)
  core.mute = false
  await run(2)
  expect(store.conn.get()).toBe('ready')
})

test("an agent's activity follows its reports: its title while it works, what it says it did when done; a shell has none", async () => {
  const core = new FakeCore()
  const store = new Store({ pipe: () => core })
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'] })
  expect(store.activityOf(r.terminal)).toBe('')
  core.setStatus(r.terminal, 'done', 'claude-code', { msg: 'ran the tests' })
  await settle()
  expect(store.activityOf(r.terminal)).toBe('ran the tests')
  const sh = await store.newSpace({})
  expect(store.activityOf(sh.terminal)).toBe('')
  store.disconnect()
})

test("a program started from the phone answers colour queries with the phone's theme from its start", async () => {
  const { core, store } = setup()
  await store.connect()
  store.display = { fg: '#d2d4d9', bg: '#17191d', cursor: '#d2d4d9' }
  await store.newSpace({ argv: ['opencode'] })
  await settle()
  const run = core.calls.find((c) => c.method === 'terminal.run')!.params
  expect(run.display).toEqual({ fg: '#d2d4d9', bg: '#17191d', cursor: '#d2d4d9' })
})

test('an agent that never reports its state (OpenCode) still shows among the agents, with no state; a shell does not', async () => {
  const { core, store } = setup()
  const doc = { spaces: [] as any[] }
  // What herdr-core answers (hc get): OpenCode in the foreground, no status, never reported.
  core.addSpace(doc, 'sp1', `${core.home}/src/api`, '', [{ id: 'tb1', argv: ['opencode'], foreground: true }, { id: 'tb2', argv: ['zsh'] }])
  core.docs.set('', { value: doc, version: 1 })
  await store.connect()
  await settle()
  expect(store.agents().map((a) => [a.address, a.status, a.programName])).toEqual([['t1', undefined, 'OpenCode']])
  expect(store.counts()).toEqual({ agents: 1, needsYou: 0 })
  core.setStatus('t1', 'working', 'opencode')
  await settle()
  expect(store.agents().map((a) => [a.address, a.status])).toEqual([['t1', 'working']])
})

test('pins are the computer\'s: a terminal labeled herdr.pinned shows pinned, and a pin or unpin elsewhere shows at once', async () => {
  const core = new FakeCore()
  const pre = core.run({ argv: ['claude'], labels: { 'herdr.space': 's0', 'herdr.tab': 'c0', 'herdr.pinned': '1' } })
  core.setStatus(pre.address, 'idle')
  core.docs.set('', { version: 1, value: { spaces: [{ id: 's0', name: '', folder: '/x', tabs: [{ id: 'c0', name: '', root: { id: 'c0', type: 'row', children: [{ id: 'x0', type: 'block', content: { terminal: pre.address } }] } }] }] } })
  const { store } = setup(core)
  await store.connect()
  const pinned = () => store.agents().find((a) => a.address === pre.address)?.pinned
  expect(pinned()).toBe(true)
  expect(store.agentsView().groups[0].id).toBe('pinned')
  core.label(pre.address, { 'herdr.pinned': '' })
  await settle()
  expect(pinned()).toBe(false)
  expect(store.agentsView().groups.map((g) => g.id)).not.toContain('pinned')
  core.label(pre.address, { 'herdr.pinned': '1' })
  await settle()
  expect(pinned()).toBe(true)
})

test('pinning on the phone writes the label the desktop reads, and unpinning removes it, keeping the other labels', async () => {
  const { core, store } = setup()
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'] })
  await settle()
  core.setStatus(r.terminal, 'idle')
  await settle()
  await store.pin(r.terminal, true)
  await settle()
  expect(core.calls.filter((c) => c.method === 'terminal.label').map((c) => c.params)).toEqual([{ terminal: r.terminal, labels: { 'herdr.pinned': '1' } }])
  expect(core.terms.get(r.terminal)!.labels).toEqual({ 'herdr.space': r.space, 'herdr.tab': r.tab, 'herdr.pinned': '1' })
  expect(store.agents()[0].pinned).toBe(true)
  await store.pin(r.terminal, false)
  await settle()
  expect(core.terms.get(r.terminal)!.labels).toEqual({ 'herdr.space': r.space, 'herdr.tab': r.tab })
  expect(store.agents()[0].pinned).toBe(false)
})

test('pins the phone kept by itself before are not shown: only the computer\'s count', async () => {
  const { core, store, saved } = setup()
  const t = core.run({ argv: ['claude'], labels: { 'herdr.space': 's0', 'herdr.tab': 'c0' } })
  core.setStatus(t.address, 'idle')
  core.docs.set('', { version: 1, value: { spaces: [{ id: 's0', name: '', folder: '/x', tabs: [{ id: 'c0', name: '', root: { id: 'c0', type: 'row', children: [{ id: 'x0', type: 'block', content: { terminal: t.address } }] } }] }] } })
  saved.set('agents.pins', JSON.stringify([t.address]))
  await store.connect()
  expect(store.agents()[0].pinned).toBe(false)
  expect(store.agentsView().groups.map((g) => g.id)).not.toContain('pinned')
})

// A tab whose root is the pane block itself (not a row holding it). Herdr writes row roots, but the shared document
// takes any tree. terminal.get answers are held so a loop shows as a growing count instead of a hung test.
const rootBlockDoc = (live: string, gone: string) => ({ spaces: [{ id: 's0', name: '', folder: '/x', tabs: [
  { id: 'c0', name: '', root: { id: 'c0', type: 'row', children: [{ id: 'x0', type: 'block', content: { terminal: live } }] } },
  { id: 'c1', name: '', root: { id: 'x1', type: 'block', content: { terminal: gone } } },
] }] })

test('a tab whose root is a pane block with an ended terminal closes, asking terminal.get once', async () => {
  const core = new FakeCore()
  const live = core.run({ argv: ['zsh'], labels: { 'herdr.space': 's0', 'herdr.tab': 'c0' } })
  const gone = core.run({ argv: ['zsh'], labels: { 'herdr.space': 's0', 'herdr.tab': 'c1' } })
  core.finish(gone.address)
  core.docs.set('', { version: 1, value: rootBlockDoc(live.address, gone.address) })
  core.holding.add('terminal.get')
  const { store, advance } = setup(core)
  await store.connect()
  advance(6000)
  store.sync()
  for (let i = 0; i < 20; i++) { core.release(); await settle(1) }
  const asked = core.calls.filter((c) => c.method === 'terminal.get' && c.params.terminal === gone.address).length
  expect(asked).toBe(1)
  expect(store.model.tabs().map((t) => t.id)).toEqual(['c0'])
  expect(core.doc().spaces[0].tabs.map((t: any) => t.id)).toEqual(['c0'])
})

test('a tab whose root is a pane block with a terminal herdr-core no longer knows closes, asking terminal.get once', async () => {
  const core = new FakeCore()
  const live = core.run({ argv: ['zsh'], labels: { 'herdr.space': 's0', 'herdr.tab': 'c0' } })
  // No such terminal at all: terminal.get answers not_found, as after the core forgot it.
  core.docs.set('', { version: 1, value: rootBlockDoc(live.address, 't-gone') })
  core.holding.add('terminal.get')
  const { store, advance } = setup(core)
  await store.connect()
  advance(6000)
  store.sync()
  for (let i = 0; i < 20; i++) { core.release(); await settle(1) }
  const asked = core.calls.filter((c) => c.method === 'terminal.get' && c.params.terminal === 't-gone').length
  expect(asked).toBe(1)
  expect(store.model.tabs().map((t) => t.id)).toEqual(['c0'])
  expect(core.doc().spaces[0].tabs.map((t: any) => t.id)).toEqual(['c0'])
})

test('reconcile asks again only on the next loop when closing a pane or placing a terminal changes nothing', async () => {
  const core = new FakeCore()
  const live = core.run({ argv: ['zsh'], labels: { 'herdr.space': 's0', 'herdr.tab': 'c0' } })
  const gone = core.run({ argv: ['zsh'], labels: { 'herdr.space': 's0', 'herdr.tab': 'c1' } })
  core.finish(gone.address)
  core.docs.set('', { version: 1, value: rootBlockDoc(live.address, gone.address) })
  const stray = core.run({ argv: ['zsh'], keep: true, labels: { 'herdr.space': 's0', 'herdr.tab': 'c2' } })
  core.holding.add('terminal.get')
  const { store, advance } = setup(core)
  await store.connect()
  // A model that can't do either: any re-check before the next loop is the store scheduling itself without a change.
  store.model.removePane = () => [false, false]
  store.model.adopt = () => {}
  advance(6000)
  store.sync()
  for (let i = 0; i < 20; i++) { core.release(); await settle(1) }
  const asked = (a: string) => core.calls.filter((c) => c.method === 'terminal.get' && c.params.terminal === a).length
  expect([asked(gone.address), asked(stray.address)]).toEqual([1, 1])
  store.sync()
  for (let i = 0; i < 20; i++) { core.release(); await settle(1) }
  expect([asked(gone.address), asked(stray.address)]).toEqual([2, 2])
})

test("an agent's spinner frames in its title cost no pass over the terminals; new words in it show at once", async () => {
  // Claude Code and others put a spinner in front of the title and turn it several times a second. With 28 agents that
  // was 250 events a second, each running the whole status pass on the phone's one JS thread (60% of it, measured on
  // the Android emulator), and taps waited behind it.
  const { core, store } = setup()
  await store.connect()
  const r = await store.newSpace({ argv: ['claude'] })
  core.setStatus(r.terminal, 'working', 'claude-code')
  core.emit('title', r.terminal, { title: '⠋ Fix the login bug' })
  await settle()
  let passes = 0
  const pass = store.sync.bind(store)
  store.sync = () => { passes++; pass() }
  const rev = store.rev.get()
  // Each frame comes in a message of its own, as over the line.
  for (const s of '⠙⠹⠸⠼⠴⠦⠧⠇⠏⠋') { core.emit('title', r.terminal, { title: `${s} Fix the login bug` }); await settle() }
  expect(passes).toBe(0)
  expect(store.rev.get()).toBe(rev)
  core.emit('title', r.terminal, { title: '⠙ Run the tests' })
  await settle()
  expect(store.activityOf(r.terminal)).toBe('Run the tests')
  expect(store.rev.get()).toBeGreaterThan(rev)
})

test('an agent that rang costs nothing on later passes until what it says changes; then its alert is told again', async () => {
  // Every pass rebuilt the whole agents list again for each agent that had rung, to see whether its words changed:
  // with many finished agents each pass took 15 ms instead of 2 on the Android emulator.
  const { core, store } = setup()
  const told: string[] = []
  store.onAlert = (k, t, again) => { if (again) told.push(t.body ?? '') }
  await store.connect()
  const agents = []
  for (let i = 0; i < 5; i++) agents.push((await store.newSpace({ argv: ['claude'] })).terminal)
  await settle()
  for (const a of agents) { core.setStatus(a, 'working', 'claude-code'); await settle(); core.setStatus(a, 'idle', 'claude-code', { msg: 'ran the tests' }); await settle() }
  let built = 0
  const view = store.agentList.view.bind(store.agentList)
  store.agentList.view = (...a) => { built++; return view(...a) }
  for (let i = 0; i < 10; i++) store.sync()
  expect(built).toBe(0)
  core.setStatus(agents[0], 'idle', 'claude-code', { msg: 'ran the tests and fixed one' })
  await settle()
  expect(told.length).toBe(1)
  expect(told[0].startsWith('ran the tests and fixed one')).toBe(true)
})

test('a Companion restarted on the computer: the phone retries at 1, 2, 4, 8, then every 15 s, and is back at the first retry after it is up', async () => {
  // Down for 12 s (as chat-behind's shots stop one Companion and start another on the same port and state).
  let now = 1_000_000
  let upAt = 0
  const core = new FakeCore()
  let first = true
  const pipe = () => {
    if (first) { first = false; return core }
    if (now >= upAt) return new FakeCore()
    const c = new FakeCore()
    c.send = () => { c.drop('network') } // refused: nothing listens on the port
    return c
  }
  const timers: { f: () => void; ms: number }[] = []
  const store = new Store({ pipe, now: () => now, setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((f: () => void, ms: number) => { timers.push({ f, ms }); return 0 }) as any })
  await store.connect()
  core.drop('closed')
  await settle()
  upAt = now + 12_000
  const waits: number[] = []
  while (store.conn.get() !== 'ready' && waits.length < 20) {
    const t = timers.shift()!
    waits.push(t.ms); now += t.ms
    t.f()
    await settle(20)
  }
  expect(store.conn.get()).toBe('ready')
  expect(waits).toEqual([1000, 2000, 4000, 8000])
  // In 90 s down it would try 9 times: 1 + 2 + 4 + 8 + 15 × 5 = 90.
  const schedule = [1, 2, 4, 8, 16, 32, 64, 128, 256].map((s) => Math.min(s * 1000, 15_000))
  expect(schedule.reduce((a, b) => a + b, 0)).toBe(90_000)
})
