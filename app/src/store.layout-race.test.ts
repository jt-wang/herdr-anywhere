import { expect, test } from 'nsh/test'
import { Store } from './store.ts'
import { FakeCore, settle } from './testing/fakecore.ts'
import { panes, terminalOf } from './model/layout.ts'

function setup() {
  const core = new FakeCore()
  const store = new Store({
    pipe: () => core,
    setInterval: (() => 0) as any,
    clearInterval: (() => {}) as any,
    setTimeout: (() => 0) as any,
    now: () => 1_000_000,
  })
  return { core, store }
}

test('a delayed layout read cannot undo an acknowledged new terminal', async () => {
  const { core, store } = setup()
  await store.connect()
  await store.newSpace({ name: 'orchard' })
  await settle(10)
  core.holding.add('doc.get')
  const oldRead = store.readDoc(undefined)
  await settle()
  const added = await store.newTab({ name: 'seedling' })
  await settle(10)
  const version = core.docs.get('')!.version
  expect(core.doc().spaces.flatMap((s: any) => s.tabs).flatMap((t: any) => panes(t.root)).some((p: any) => p.id === added.pane && terminalOf(p) === added.terminal)).toBe(true)
  core.holding.delete('doc.get')
  core.release()
  await oldRead
  expect(store.slots.get('')!.version).toBe(version)
  expect(store.model.paneOf(added.terminal)).toBe(added.pane)
  expect(store.model.focusedPane()).toBe(added.pane)
})

for (const kind of ['space', 'split'] as const) test(`a concurrent remote edit preserves a newly created ${kind}`, async () => {
  const { core, store } = setup()
  await store.connect()
  const first = await store.newSpace({ name: 'orchard' })
  await settle(10)
  const remote = JSON.parse(JSON.stringify(core.doc()))
  remote.spaces[0].name = 'grove'
  const previous = core.docs.get('')!
  core.docs.set('', { value: remote, version: previous.version + 1 })
  const added = kind === 'space'
    ? await store.newSpace({ name: 'meadow' })
    : await store.splitPane(first.pane, 'down')
  await settle(20)
  expect(store.model.space(first.space)!.name).toBe('grove')
  expect(store.model.paneOf(added.terminal)).toBe(added.pane)
  expect(store.model.focusedPane()).toBe(added.pane)
  if (kind === 'space') expect(store.model.activeSpace()!.name).toBe('meadow')
  else expect(store.model.tabOf(added.pane)!.root.children![0].type).toBe('column')
  store.focusPane(added.pane)
  await settle(10)
  const matches = core.doc().spaces.flatMap((s: any) => s.tabs).flatMap((t: any) => panes(t.root)).filter((p: any) => terminalOf(p) === added.terminal)
  expect(matches.map((p: any) => p.id)).toEqual([added.pane])
  expect(core.calls.filter(c => c.method === 'terminal.run').length).toBe(2)
  let received = ''
  core.onSend = (terminal, text) => { if (terminal === added.terminal) received = text }
  await store.call('terminal.send', { terminal: added.terminal, text: 'synthetic input' })
  expect(received).toBe('synthetic input')
})

test('a concurrent remote rename preserves the new terminal and its focus', async () => {
  const { core, store } = setup()
  await store.connect()
  const first = await store.newSpace({ name: 'orchard' })
  await settle(10)
  const remote = JSON.parse(JSON.stringify(core.doc()))
  remote.spaces[0].name = 'grove'
  const previous = core.docs.get('')!
  // The other writer commits before our CAS; its event has not reached this screen yet.
  core.docs.set('', { value: remote, version: previous.version + 1 })
  const added = await store.newTab({ name: 'seedling' })
  await settle(20)
  expect(store.model.space(first.space)!.name).toBe('grove')
  expect(store.model.paneOf(added.terminal)).toBe(added.pane)
  expect(store.model.focusedPane()).toBe(added.pane)
  // Normal sync persists both writers' changes without starting another terminal.
  store.focusPane(added.pane)
  await settle(10)
  const doc = core.doc()
  expect(doc.spaces[0].name).toBe('grove')
  expect(doc.spaces[0].tabs.flatMap((t: any) => panes(t.root)).filter((p: any) => terminalOf(p) === added.terminal).length).toBe(1)
  expect(core.calls.filter(c => c.method === 'terminal.run').length).toBe(2)
  let received = ''
  core.onSend = (terminal, text) => { if (terminal === added.terminal) received = text }
  await store.call('terminal.send', { terminal: added.terminal, text: 'synthetic input' })
  expect(received).toBe('synthetic input')
})

test('a concurrent rename keeps a new split beside the first of three panes', async () => {
  const { core, store } = setup()
  await store.connect()
  const first = await store.newSpace({ name: 'orchard' })
  const second = await store.splitPane(first.pane, 'right')
  const third = await store.splitPane(second.pane, 'right')
  await settle(10)
  const remote = JSON.parse(JSON.stringify(core.doc()))
  remote.spaces[0].name = 'grove'
  const previous = core.docs.get('')!
  core.docs.set('', { value: remote, version: previous.version + 1 })
  const added = await store.splitPane(first.pane, 'right')
  await settle(20)
  const expected = [first, added, second, third].map(p => [p.pane, p.terminal])
  expect(panes(store.model.tabOf(first.pane)!.root).map(p => [p.id, terminalOf(p)])).toEqual(expected)
  expect(panes(core.doc().spaces[0].tabs[0].root).map(p => [p.id, terminalOf(p)])).toEqual(expected)
  expect(store.model.space(first.space)!.name).toBe('grove')
  expect(store.model.focusedPane()).toBe(added.pane)
  expect(core.calls.filter(c => c.method === 'terminal.run').length).toBe(4)
})
