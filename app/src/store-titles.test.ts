import { expect, test } from 'nsh/test'
import { Store } from './store.ts'
import { startable } from './model/programs.ts'
import { FakeCore, settle } from './testing/fakecore.ts'

function setup(tabs: any[] = [{ id: 'tab', argv: ['codex'], status: 'working' }]) {
  const core = new FakeCore(), doc = { spaces: [] as any[] }
  core.addSpace(doc, 'space', '/synthetic/example', 'Example', tabs)
  core.docs.set('', { value: doc, version: 1 })
  let now = 1000
  let nextTimer = 0
  const timeouts = new Map<number, { run: () => void; ms: number }>()
  let connectedCore = core
  const store = new Store({ pipe: () => connectedCore, now: () => now, setInterval: (() => 0) as any, clearInterval: (() => {}) as any,
    setTimeout: ((run: () => void, ms: number) => { const id = ++nextTimer; timeouts.set(id, { run, ms }); return id }) as any,
    clearTimeout: ((id: number) => timeouts.delete(id)) as any })
  const calls: string[] = [], pending: { resolve: (v: any) => void; reject: (v: any) => void }[] = []
  const signals: (AbortSignal | undefined)[] = []
  store.chat = { title: (a: string, signal?: AbortSignal) => { calls.push(a); signals.push(signal); return new Promise((resolve, reject) => pending.push({ resolve, reject })) } } as any
  const label = (a = 't1') => store.shown().view.detail.get(a)
  return { core, store, calls, pending, signals, label, expire: () => { for (const [id, t] of [...timeouts]) if (t.ms === 5000) { timeouts.delete(id); t.run() } }, advance: (ms: number) => { now += ms; store.sync() }, reconnect: async () => {
    store.disconnect(); await settle()
    connectedCore = new FakeCore(); connectedCore.terms = new Map(core.terms); connectedCore.docs = new Map(core.docs)
    await store.connect(); await settle()
  } }
}

test('saved Codex title appears on home without opening chat and is never activity', async () => {
  const h = setup()
  await h.store.connect(); await settle()
  expect(h.calls).toEqual(['t1'])
  h.pending[0].resolve({ title: 'Index migration', session: 'opaque-one' }); await settle()
  expect(h.label()).toBe('Index migration')
  expect(h.store.activityOf('t1')).toBe('')
  expect(h.store.term('t1')!.title).toBe('')
})

test('manual names and meaningful HC titles win; generic Codex title can use a saved name', async () => {
  const h = setup([
    { id: 'a', name: 'Named by user', argv: ['codex'], status: 'working' },
    { id: 'b', title: 'Live task', argv: ['codex'], status: 'working' },
    { id: 'c', title: 'Codex', argv: ['codex'], status: 'working' },
    { id: 'd', argv: ['kimi'] },
  ])
  await h.store.connect(); await settle()
  expect(h.calls).toEqual(['t3'])
  h.pending[0].resolve({ title: 'Saved task', session: 'opaque-one' }); await settle()
  expect([h.label('t1'), h.label('t2'), h.label('t3')]).toEqual(['Named by user', 'Live task', 'Saved task'])
})

test('late saved titles cannot override a new HC title or manual tab name', async () => {
  const h = setup(); await h.store.connect(); await settle()
  h.core.terms.get('t1')!.title = 'Fresh HC title'; h.core.emit('title', 't1', { title: 'Fresh HC title' }); await settle()
  h.pending[0].resolve({ title: 'Old saved title', session: 'old' }); await settle()
  expect(h.label()).toBe('Fresh HC title')
  const j = setup(); await j.store.connect(); await settle()
  j.store.model.spaces[0].tabs[0].name = 'User chosen'; j.store.sync()
  j.pending[0].resolve({ title: 'Old saved title', session: 'old' }); await settle()
  expect(j.label()).toBe('User chosen')
})

test('process replacement and closed terminals discard old responses', async () => {
  const h = setup(); await h.store.connect(); await settle()
  h.store.term('t1')!.pid = 9002; h.store.sync(); await settle()
  expect(h.calls.length).toBe(2)
  h.pending[0].resolve({ title: 'Previous process', session: 'old' }); await settle()
  expect(h.label()).toBe('Codex')
  h.pending[1].resolve({ title: 'Current process', session: 'new' }); await settle()
  expect(h.label()).toBe('Current process')
  const j = setup(); await j.store.connect(); await settle()
  j.core.finish('t1'); await settle()
  j.pending[0].resolve({ title: 'Gone task', session: 'old' }); await settle()
  expect(j.store.shown().agents.length).toBe(0)
})

test('background and disconnect stop requests; responses after disconnect are discarded', async () => {
  const h = setup(); h.store.appActive = false
  await h.store.connect(); await settle(); expect(h.calls.length).toBe(0)
  h.store.appActive = true; await settle(); expect(h.calls.length).toBe(1)
  h.store.disconnect(); h.pending[0].resolve({ title: 'Late response', session: 'old' }); await settle()
  expect(h.label()).toBe('Codex')
  h.advance(60000); expect(h.calls.length).toBe(1)
})

test('requests are bounded, failures are throttled, and changed or empty sessions replace cached names', async () => {
  const h = setup([1, 2, 3].map(i => ({ id: `tab${i}`, argv: ['codex'], status: 'working' })))
  await h.store.connect(); await settle(); expect(h.calls.length).toBe(2)
  h.pending[0].reject(new Error('offline')); await settle(); expect(h.calls.length).toBe(3)
  h.pending[1].resolve({ title: 'Saved task', session: 'one' }); h.pending[2].resolve({}); await settle()
  h.advance(29000); await settle(); expect(h.calls.length).toBe(3)
  h.advance(1000); await settle(); expect(h.calls.length).toBe(5)
  h.pending[3].resolve({}); h.pending[4].resolve({ title: 'Next session', session: 'two' }); await settle()
  expect(h.label('t2')).toBe('Next session')
  h.pending[5].resolve({}); await settle()
  h.advance(30000); await settle()
  h.pending[6].resolve({}); h.pending[7].resolve({}); await settle()
  expect(h.label('t2')).toBe('Codex')
})

test('explicit terminal names are preserved without reading saved titles', async () => {
  const h = setup(); h.core.terms.get('t1')!.name = 'Named terminal'
  await h.store.connect(); await settle()
  expect(h.calls.length).toBe(0)
  expect(h.label()).toBe('Named terminal')
})

test('a tab named only Codex retains the existing unnamed-tab semantics', async () => {
  const h = setup([{ id: 'tab', name: 'Codex', argv: ['codex'], status: 'working' }])
  await h.store.connect(); await settle()
  expect(h.calls).toEqual(['t1'])
  h.pending[0].resolve({ title: 'Descriptive saved title', session: 'opaque' }); await settle()
  expect(h.label()).toBe('Descriptive saved title')
})

test('an unsupported endpoint is tried once per connection and a reconnect checks again', async () => {
  const h = setup(); await h.store.connect(); await settle()
  h.pending[0].resolve({ unsupported: true }); await settle()
  h.advance(120000); await settle(); expect(h.calls.length).toBe(1)
  await h.reconnect(); expect(h.calls.length).toBe(2)
  h.pending[1].resolve({ title: 'New connection', session: 'new' }); await settle()
  expect(h.label()).toBe('New connection')
})

test('a response from the previous connection cannot overwrite the new connection', async () => {
  const h = setup(); await h.store.connect(); await settle()
  await h.reconnect(); expect(h.calls.length).toBe(2)
  h.pending[1].resolve({ title: 'Current connection', session: 'new' }); await settle()
  h.pending[0].resolve({ title: 'Old connection', session: 'old' }); await settle()
  expect(h.label()).toBe('Current connection')
})

test('two hung requests are aborted on reconnect and cannot consume the new connection slots', async () => {
  const h = setup([1, 2].map(i => ({ id: `tab${i}`, argv: ['codex'], status: 'working' })))
  await h.store.connect(); await settle(); expect(h.calls.length).toBe(2)
  await h.reconnect(); expect(h.calls.length).toBe(4)
  expect(h.signals.slice(0, 2).map(s => s?.aborted)).toEqual([true, true])
  h.pending[0].resolve({ title: 'Stale first', session: 'old' }); h.pending[1].resolve({}); await settle()
  h.pending[2].resolve({ title: 'Fresh first', session: 'new' }); await settle()
  expect(h.label()).toBe('Fresh first')
  expect(h.calls.length).toBe(4)
})

test('a hung title read times out, aborts transport, and permits a later bounded retry', async () => {
  const h = setup(); await h.store.connect(); await settle()
  h.expire(); await settle()
  expect(h.signals[0]?.aborted).toBe(true)
  h.advance(30000); await settle(); expect(h.calls.length).toBe(2)
  h.pending[1].resolve({ title: 'Retry title', session: 'new' }); await settle()
  h.pending[0].resolve({ title: 'Timed out title', session: 'old' }); await settle()
  expect(h.label()).toBe('Retry title')
})


test('Claude uses its saved session title while explicit names and HC titles win', async () => {
  const h = setup([
    { id: 'a', argv: ['claude'], status: 'working' },
    { id: 'b', name: 'User chosen', argv: ['claude'], status: 'working' },
    { id: 'c', title: 'HC supplied task', argv: ['claude'], status: 'working' },
  ])
  await h.store.connect(); await settle()
  expect(h.calls).toEqual(['t1'])
  h.pending[0].resolve({ title: 'Saved Claude task', session: 'opaque-claude' }); await settle()
  expect([h.label('t1'), h.label('t2'), h.label('t3')]).toEqual(['Saved Claude task', 'User chosen', 'HC supplied task'])
  expect(h.store.activityOf('t1')).toBe('')
  expect(h.store.term('t1')!.title).toBe('')
})

test('Claude without a saved session title retains the program name', async () => {
  const h = setup([{ id: 'tab', argv: ['claude'], status: 'working' }])
  await h.store.connect(); await settle(); expect(h.calls).toEqual(['t1'])
  h.pending[0].resolve({}); await settle()
  expect(h.label()).toBe('Claude Code')
  expect(h.store.activityOf('t1')).toBe('')
})

test('Claude titles do not request in background or accept previous-connection responses', async () => {
  const h = setup([{ id: 'tab', argv: ['claude'], status: 'working' }]); h.store.appActive = false
  await h.store.connect(); await settle(); expect(h.calls.length).toBe(0)
  h.store.appActive = true; await settle(); expect(h.calls.length).toBe(1)
  await h.reconnect(); expect(h.calls.length).toBe(2)
  h.pending[1].resolve({ title: 'Current Claude task', session: 'new' }); await settle()
  h.pending[0].resolve({ title: 'Previous Claude task', session: 'old' }); await settle()
  expect(h.label()).toBe('Current Claude task')
})

// Production chatReaders registration (companion chat_readers_test.go), not the launch-menu sort order.
const chatAgents = [
  ['claude', 'claude-code', 'Claude Code'], ['codex', 'codex', 'Codex'], ['pi', 'pi', 'Pi'],
  ['omp', 'omp', 'Oh My Pi'], ['opencode', 'opencode', 'OpenCode'], ['cursor-agent', 'cursor', 'Cursor'], ['agy', 'antigravity', 'Antigravity'],
]
for (const [command, app, name] of chatAgents) {
  const make = (extra = {}) => {
    const h = setup([{ id: 'tab', argv: [command], status: 'working', ...extra }])
    h.core.setStatus('t1', 'working', app)
    return h
  }
  test(`${name}: existing saved title is display-only and an empty reply keeps a stable program name`, async () => {
    const h = make(); await h.store.connect(); await settle()
    expect(h.calls).toEqual(['t1'])
    h.pending[0].resolve({}); await settle()
    expect(h.label()).toBe(name)
    expect(h.store.activityOf('t1')).toBe('')
    h.advance(30000); await settle()
    h.pending[1].resolve({ title: 'Saved sample task', session: 'opaque-sample' }); await settle()
    expect(h.label()).toBe('Saved sample task')
    expect(h.store.activityOf('t1')).toBe('')
    expect(h.store.term('t1')!.title).toBe('')
  })
  test(`${name}: manual names and HC titles take priority`, async () => {
    for (const extra of [{ name: 'Chosen name' }, { title: 'Live task title' }]) {
      const h = make(extra); await h.store.connect(); await settle()
      expect(h.calls.length).toBe(0)
      expect(h.label()).toBe('name' in extra ? extra.name : extra.title)
    }
  })
  test(`${name}: background and old connection replies cannot introduce a title`, async () => {
    const h = make(); h.store.appActive = false
    await h.store.connect(); await settle(); expect(h.calls.length).toBe(0)
    h.store.appActive = true; await settle(); expect(h.calls.length).toBe(1)
    await h.reconnect(); expect(h.calls.length).toBe(2)
    h.pending[1].resolve({ title: 'Current sample task', session: 'new' }); await settle()
    h.pending[0].resolve({ title: 'Stale sample task', session: 'old' }); await settle()
    expect(h.label()).toBe('Current sample task')
  })
}


// The launchable terminal programs beyond the seven registered chat readers still have useful names.
for (const program of startable().filter(p => !['claude', 'codex', 'pi', 'omp', 'opencode', 'cursor', 'antigravity'].includes(p.key))) {
  test(`${program.name}: terminal-only mode retains its name without requesting unsupported saved titles`, async () => {
    const h = setup([{ id: 'tab', argv: program.run, foreground: true }]); await h.store.connect(); await settle()
    expect(h.label()).toBe(program.name)
    expect(h.calls.length).toBe(0)
    expect(h.store.activityOf('t1')).toBe('')
    h.store.renameTab('tab', 'My terminal'); await settle()
    expect(h.label()).toBe('My terminal')
  })
}

test('a changed reported agent invalidates a saved title even when the terminal PID is unchanged', async () => {
  const h = setup(); await h.store.connect(); await settle()
  h.pending[0].resolve({ title: 'Previous agent task', session: 'old' }); await settle()
  h.core.setStatus('t1', 'working', 'pi'); await settle()
  expect(h.label()).toBe('Pi')
  expect(h.calls.length).toBe(2)
  h.pending[1].resolve({ title: 'Current agent task', session: 'new' }); await settle()
  expect(h.label()).toBe('Current agent task')
})

test('an OpenCode session switch at the same PID rejects the previous session title', async () => {
  const h = setup([{ id: 'tab', argv: ['opencode'], status: 'working' }])
  h.core.terms.get('t1')!.records = { 'session-a': { app: 'opencode' } }
  await h.store.connect(); await settle(); expect(h.calls.length).toBe(1)
  const t = h.core.terms.get('t1')!
  t.records = { 'session-b': { app: 'opencode' } }
  h.core.emit('status', 't1', { status: t.status, records: t.records, reported: true }); await settle()
  h.pending[0].resolve({ title: 'Previous session task', session: 'opaque-a' }); await settle()
  expect(h.label()).toBe('OpenCode')
  expect(h.calls.length).toBe(2)
  h.pending[1].resolve({ title: 'Current session task', session: 'opaque-b' }); await settle()
  expect(h.label()).toBe('Current session task')
})

test('reported title sessions use sorted matching-app IDs, ignoring record order and activity fields', async () => {
  const h = setup([{ id: 'tab', argv: ['opencode'], status: 'working' }])
  const t = h.core.terms.get('t1')!
  t.records = { beta: { app: 'opencode', state: 'working' }, alpha: { app: 'opencode' }, foreign: { app: 'pi' } }
  await h.store.connect(); await settle()
  h.pending[0].resolve({ title: 'Retained task', session: 'opaque' }); await settle()
  t.records = { differentForeign: { app: 'claude-code' }, alpha: { app: 'opencode', msg: 'Changed activity' }, beta: { app: 'opencode', state: 'done' } }
  h.core.emit('status', 't1', { status: { ...t.status, state: 'done' }, records: t.records, reported: true }); await settle()
  expect(h.calls.length).toBe(1)
  expect(h.label()).toBe('Retained task')
})
