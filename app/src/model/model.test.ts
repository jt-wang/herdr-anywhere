// Ported from herdr-app's own tests (src/model.rs, src/mirror.rs, src/layout.rs), so the phone moves panes,
// names things and rings alerts exactly as the desktop does.
import { expect, test } from 'nsh/test'
import { Model, folderName, newTab, alert } from './model.ts'
import { captureDoc, replaceDoc, captureView, applyView } from './doc.ts'
import { modBlock, panes, find, parentOf } from './layout.ts'

function modelWithOne() {
  const m = new Model()
  m.addSpace('p1', 'api', '/src/api', newTab('c1', 'fix login', 'x1', 't1'))
  return m
}

test('folders are named as in herdr', () => {
  expect(folderName('/home/can', '/home/can')).toBe('~')
  expect(folderName('/home/can/', '/home/can')).toBe('~')
  expect(folderName('/Users/can/Projects/herdr', '/Users/can')).toBe('herdr')
  expect(folderName('/', '/home/can')).toBe('/')
  expect(folderName('', '/home/can')).toBe('~')
  expect(folderName('C:/Users/can/src/web', 'C:\\Users\\can')).toBe('web')
})

test('unnamed spaces follow their first terminal and tabs count', () => {
  const m = new Model()
  m.addSpace('p1', '', '/home/sam', newTab('c1', '', 'x1', 't1'))
  m.addTab('p1', newTab('c2', 'review', 'x2', 't2'))
  m.addTab('p1', newTab('c3', '', 'x3', 't3'))
  const p = m.space('p1')!
  expect(m.spaceName(p, '/home/sam')).toBe('~')
  m.paneFolders.set('x1', '/src/notes')
  expect(m.spaceName(p, '/home/sam')).toBe('notes')
  m.folderInfo.set('t1', { top: '/src/herdr', branch: 'main' })
  expect(m.spaceName(p, '/home/sam')).toBe('herdr')
  expect(p.tabs.map((c) => Model.tabName(p, c))).toEqual(['1', 'review', '3'])
  p.name = 'api'
  expect(m.spaceName(p, '/home/sam')).toBe('api')
})

test('alerts follow herdr', () => {
  expect(alert('working', 'blocked', true)).toBe('blocked')
  expect(alert('blocked', 'error', false)).toBe(undefined)
  expect(alert('working', 'idle', false)).toBe('done')
  expect(alert('blocked', 'done', false)).toBe('done')
  expect(alert('working', 'idle', true)).toBe(undefined)
  expect(alert(undefined, 'idle', false)).toBe(undefined)
  expect(alert('working', undefined, false)).toBe(undefined)
})

test('only our own agents alert', () => {
  const m = modelWithOne()
  expect(m.alertFor('t1', false, 'working', 'done', false)).toBe('done')
  expect(m.alertFor('vps/t7', false, 'working', 'done', false)).toBe(undefined)
  expect(m.alertFor('t9', false, 'working', 'blocked', false)).toBe(undefined)
  expect(m.alertFor('t1', true, 'working', 'blocked', false)).toBe(undefined)
})

test('seen rules: a finish you did not watch is done until you look', () => {
  const m = modelWithOne()
  m.setWord('t1', 'working', 'claude-code', false)
  expect(m.status('t1')).toBe('working')
  m.setWord('t1', 'idle', undefined, false)
  expect(m.status('t1')).toBe('done')
  m.markSeen(['t1'])
  expect(m.status('t1')).toBe('idle')
  m.setWord('t1', 'blocked', undefined, false)
  expect(m.status('t1')).toBe('blocked')
  m.setWord('t1', 'idle', undefined, true)
  expect(m.status('t1')).toBe('idle')
  expect(m.agents.get('t1')!.app).toBe('claude-code')
  expect(m.status('t5')).toBe(undefined)
})

test('rollup takes the most urgent: blocked > done > working > idle', () => {
  const m = modelWithOne()
  m.setWord('a', 'working', undefined, false)
  m.setWord('b', 'working', undefined, false)
  m.setWord('b', 'idle', undefined, false)
  expect(m.rollup(['a', 'b'])).toBe('done')
  m.setWord('c', 'error', undefined, false)
  expect(m.rollup(['a', 'b', 'c'])).toBe('blocked')
  expect(m.rollup(['zz'])).toBe(undefined)
})

test('settling the shown tab hands over and showing it brings it back', () => {
  const m = modelWithOne()
  m.addTab('p1', newTab('c2', 'docs', 'x2', 't2'))
  m.selectTab('c1')
  expect(m.settle('c1', 5)).toBe(true)
  expect(m.activeTab()?.id).toBe('c2')
  expect(m.tab('c1')!.settled).toBe(5)
  expect(m.settle('c2', 6)).toBe(true)
  expect(m.activeTab()?.id).toBe('c2')
  m.selectTab('c1')
  expect(m.tab('c1')!.settled).toBe(undefined)
})

test('closing the last pane closes tab then space and remembers it', () => {
  const m = modelWithOne()
  m.addTab('p1', newTab('c2', 'docs', 'x2', 't2'))
  m.addSpace('p2', 'web', '/src/web', newTab('c3', 'deploy', 'x3', 't3'))
  expect(m.active).toBe('p2')
  expect(m.removePane('x3')).toEqual([true, true])
  expect(m.active).toBe('p1')
  expect(m.recent).toEqual([{ name: 'web', folder: '/src/web' }])
  expect(m.removePane('x2')).toEqual([true, false])
  expect(m.activeTab()?.id).toBe('c1')
  m.addSpace('p4', 'web', '/src/web', newTab('c4', 'x', 'x4', 't4'))
  expect(m.recent).toEqual([])
})

test('a mod is a pane beside terminals', () => {
  const m = modelWithOne()
  expect(m.splitWith('x1', 'row', modBlock('x2', 'files', { folder: '/src' }))).toBe(true)
  const c = m.tab('c1')!
  expect(panes(c.root).length).toBe(2)
  expect(c.focused).toBe('x2')
  expect(m.addressOf('x2')).toBe(undefined)
  expect(m.removePane('x1')).toEqual([false, false])
  expect(panes(m.tab('c1')!.root).length).toBe(1)
})

test('new tabs go after the selected one', () => {
  const m = modelWithOne()
  m.addTab('p1', newTab('c2', '', 'x2', 't2'))
  m.selectTab('c1')
  m.addTab('p1', newTab('c3', '', 'x3', 't3'))
  expect(m.space('p1')!.tabs.map((c) => c.id)).toEqual(['c1', 'c3', 'c2'])
  expect(m.activeTab()?.id).toBe('c3')
})

test('splits focus the new pane and closing it moves focus back', () => {
  const m = modelWithOne()
  expect(m.split('x1', 'row', 'x9', 't9')).toBe(true)
  expect(m.focusedPane()).toBe('x9')
  expect(m.removePane('x9')).toEqual([false, false])
  expect(m.focusedPane()).toBe('x1')
  expect(m.activeTab()!.root.id).toBe('c1')
})

test('splitting along the same axis halves the target and reuses the row', () => {
  const m = modelWithOne()
  m.split('x1', 'row', 'x2', 't2')
  m.split('x2', 'row', 'x3', 't3')
  const root = m.tab('c1')!.root
  expect(root.type).toBe('row')
  expect(root.children!.map((n) => n.size)).toEqual([{ weight: 0.5 }, { weight: 0.25 }, { weight: 0.25 }])
  m.split('x3', 'column', 'x4', 't4')
  const col = parentOf(root, 'x4')!
  expect(col.type).toBe('column')
  expect(col.size).toEqual({ weight: 0.25 })
  expect(col.children!.map((n) => n.id)).toEqual(['x3', 'x4'])
  // Closing x4 gives its column back to x3 with the column's slot size.
  m.removePane('x4')
  expect(find(root, 'x3')!.size).toEqual({ weight: 0.25 })
  expect(root.children!.map((n) => n.id)).toEqual(['x1', 'x2', 'x3'])
  // Closing x2 gives its share to the neighbor it was split from.
  m.removePane('x2')
  expect(root.children!.map((n) => n.size)).toEqual([{ weight: 0.75 }, { weight: 0.25 }])
})

test('closing the focused pane goes back to the one before', () => {
  const m = modelWithOne()
  m.split('x1', 'row', 'x2', 't2')
  m.split('x2', 'row', 'x3', 't3')
  m.focusPane('x2')
  m.focusPane('x3')
  m.removePane('x3')
  expect(m.focusedPane()).toBe('x2')
})

test('swapping panes keeps sizes with places', () => {
  const m = modelWithOne()
  m.split('x1', 'row', 'x2', 't2')
  m.split('x2', 'row', 'x3', 't3')
  expect(m.movePane('x1', 'x3')).toBe(true)
  const root = m.tab('c1')!.root
  expect(root.children!.map((n) => n.id)).toEqual(['x3', 'x2', 'x1'])
  expect(root.children!.map((n) => n.size)).toEqual([{ weight: 0.5 }, { weight: 0.25 }, { weight: 0.25 }])
  expect(m.addressOf('x1')).toBe('t1')
})

test('spaces move inside their machine block', () => {
  const doc = captureDoc(modelWithOne(), undefined)
  const m = new Model()
  replaceDoc(doc, m, 'z')
  replaceDoc(doc, m, 'a')
  replaceDoc(doc, m, undefined)
  expect(m.spaces.map((s) => s.id)).toEqual(['p1', 'a/p1', 'z/p1'])
  m.addSpace('s2', '', '', newTab('t2', '', 'p2', 't2'))
  m.orderSpaces()
  expect(m.moveSpace('p1', Number.MAX_SAFE_INTEGER)).toBe(true)
  expect(m.spaces.map((s) => s.id)).toEqual(['s2', 'p1', 'a/p1', 'z/p1'])
  expect(m.moveSpace('z/p1', 1)).toBe(true)
  expect(m.spaces.at(-1)!.id).toBe('z/p1')
})

test('focus, zoom and settle are private to a screen', () => {
  const m = modelWithOne()
  const before = JSON.stringify(captureDoc(m, undefined))
  m.tab('c1')!.zoomed = 'x1'
  m.settle('c1', 12)
  expect(JSON.stringify(captureDoc(m, undefined))).toBe(before)
  const view = captureView(m)
  const other = new Model()
  replaceDoc(JSON.parse(before), other, undefined)
  expect(other.tab('c1')!.zoomed).toBe(undefined)
  applyView(view, other)
  expect(other.tab('c1')!.zoomed).toBe('x1')
  expect(other.tab('c1')!.settled).toBe(12)
})

test('a remote round trip qualifies identity but never mod props', () => {
  const m = modelWithOne()
  m.splitWith('x1', 'row', modBlock('mod', 'test', { terminal: 't1' }))
  const doc = captureDoc(m, undefined)
  replaceDoc(doc, m, 'mac')
  expect(m.addressOf('mac/x1')).toBe('mac/t1')
  expect(find(m.tab('mac/c1')!.root, 'mac/mod')!.content).toEqual({ mod: 'test', props: { terminal: 't1' } })
  expect(captureDoc(m, 'mac')).toEqual(doc)
  expect(m.space('p1') !== undefined && m.space('mac/p1') !== undefined).toBe(true)
})

test('shared fields can be omitted, and unknown node fields survive a round trip', () => {
  const m = new Model()
  replaceDoc({ spaces: [{ id: 's', tabs: [{ id: 't', root: { id: 't', type: 'row', gap: 4, children: [{ id: 'p', type: 'block', content: { terminal: 't1' }, title: 'x' }] } }] }] }, m, undefined)
  const s = m.space('s')!
  expect(s.name).toBe('')
  expect(s.folder).toBe('')
  const out = captureDoc(m, undefined)
  expect(out.spaces[0].tabs[0].root).toEqual({ id: 't', type: 'row', gap: 4, children: [{ id: 'p', type: 'block', content: { terminal: 't1' }, title: 'x' }] })
  replaceDoc({}, m, undefined)
  expect(m.spaces).toEqual([])
})

test('a new document keeps this screen\'s valid view and drops what is gone', () => {
  const m = modelWithOne()
  m.split('x1', 'row', 'x2', 't2')
  const doc = captureDoc(m, undefined)
  m.focusPane('x1')
  replaceDoc(doc, m, undefined)
  expect(m.focusedPane()).toBe('x1')
  const other = modelWithOne()
  other.removePane('x1')
  replaceDoc(captureDoc(other, undefined), m, undefined)
  expect(m.activeSpace()).toBe(undefined)
})

test('adopting a labeled terminal makes its space under the label id, or joins its tab', () => {
  const m = new Model()
  m.adopt('t5', 'sp1', 'tb1', '/src/api', () => 'p-new1')
  expect(m.space('sp1')!.tabs[0].id).toBe('tb1')
  expect(m.addressOf('p-new1')).toBe('t5')
  m.adopt('t6', 'sp1', 'tb1', '/src/api', () => 'p-new2')
  expect(panes(m.tab('tb1')!.root).map((n) => n.id)).toEqual(['p-new1', 'p-new2'])
  // Joining an existing tab does not move focus.
  expect(m.tab('tb1')!.focused).toBe('p-new1')
  m.adopt('t7', 'sp1', undefined, '/src/api', (p) => p === 'tb' ? 'tb-new' : 'p-new3')
  expect(m.space('sp1')!.tabs.map((t) => t.id)).toEqual(['tb1', 'tb-new'])
  expect(m.space('sp1')!.active).toBe('tb1')
})

test('removing the pane that is its tab\'s whole root closes the tab, as removing its last pane does', () => {
  const m = modelWithOne()
  m.addTab('p1', newTab('c2', '', 'x2', 't2'))
  m.tab('c2')!.root = { id: 'x2', type: 'block', content: { terminal: 't2' } }
  expect(m.removePane('x2')).toEqual([true, false])
  expect(m.tabs().map((t) => t.id)).toEqual(['c1'])
  m.tab('c1')!.root = { id: 'x1', type: 'block', content: { terminal: 't1' } }
  expect(m.removePane('x1')).toEqual([true, true])
  expect(m.spaces).toEqual([])
})
