// The agents sidebar's rules (herdr-app defaults/mods/agents/main.js).
import { expect, test } from 'nsh/test'
import { AgentList, duration, type AgentInfo } from './agents.ts'

const a = (address: string, status: AgentInfo['status'], extra: Partial<AgentInfo> = {}): AgentInfo => ({
  address, pane: 'p' + address, space: 's1', space_name: 'api', tab: 'tb1', tab_name: '1', named: false, status, app: null, title: '',
  seq: 0, focused: false, pinned: false, programName: 'Claude Code', programKey: 'claude', ...extra,
})

test('durations read <1m, minutes, hours, days', () => {
  expect([duration(10_000), duration(12 * 60_000), duration(3 * 3600_000), duration(49 * 3600_000)]).toEqual(['<1m', '12m', '3h', '2d'])
})

test('priority view: needs you, done, working, idle; newer change first inside a section', () => {
  const l = new AgentList()
  let now = 1000
  const list = [a('t1', 'idle'), a('t2', 'working'), a('t3', 'blocked'), a('t4', 'done'), a('t5', 'working')]
  l.view(list, [], now)
  now = 2000
  list[1] = a('t2', 'idle')
  list[1] = a('t2', 'working', { seq: 3 })
  const v = l.view([...list.slice(0, 4), a('t5', 'blocked')], [], now)
  expect(v.groups.map((g) => [g.label, g.members.map((m) => m.address)])).toEqual([
    ['Needs you', ['t5', 't3']], ['Done', ['t4']], ['Working', ['t2']], ['Idle', ['t1']],
  ])
})

test('a done agent you open stays under Done until you leave it', () => {
  const l = new AgentList()
  l.view([a('t1', 'done', { focused: true }), a('t2', 'idle')], [], 1)
  const v = l.view([a('t1', 'idle', { focused: true }), a('t2', 'idle')], [], 2)
  expect(v.groups[0].label).toBe('Done')
  const after = l.view([a('t1', 'idle'), a('t2', 'idle', { focused: true })], [], 3)
  expect(after.groups.map((g) => g.label)).toEqual(['Idle'])
})

test('agents pinned on the computer (label herdr.pinned) stay on top in their own group, in the order they were pinned', () => {
  const l = new AgentList()
  const v = l.view([a('t1', 'blocked'), a('t2', 'working', { pinned: true, place: 'r' }), a('t3', 'idle', { pinned: true, place: 'i' })], [], 1)
  expect(v.groups.map((g) => [g.label, g.members.map((m) => m.address)])).toEqual([['Pinned', ['t3', 't2']], ['Needs you', ['t1']]])
  expect(v.order).toEqual(['t3', 't2', 't1'])
  l.sort = 'grouped'
  const g = l.view([a('t1', 'blocked'), a('t2', 'idle', { pinned: true })], [], 2)
  expect(g.groups.map((x) => [x.id, x.members.map((m) => m.address)])).toEqual([['pinned', ['t2']], ['sp-s1', ['t1']]])
})

test('a pinned agent keeps its row when its status changes: only its dot changes, nothing moves', () => {
  // The user: Pinned jumping about with every change was tiring to watch.
  const l = new AgentList()
  const pins = (s1: AgentInfo['status'], s2: AgentInfo['status'], s3: AgentInfo['status']) =>
    [a('t1', s1, { pinned: true, place: 'i' }), a('t2', s2, { pinned: true, place: 'r' }), a('t3', s3, { pinned: true, place: 'v' })]
  const order = (now: number, list: AgentInfo[]) => l.view(list, [], now).groups[0].members.map((m) => m.address)
  expect(order(1, pins('idle', 'working', 'idle'))).toEqual(['t1', 't2', 't3'])
  expect(order(2, pins('idle', 'working', 'blocked'))).toEqual(['t1', 't2', 't3'])
  expect(order(3, pins('done', 'idle', 'blocked'))).toEqual(['t1', 't2', 't3'])
  // In the order agents() lists them (space, tab, pane) when none has a place yet, never by status.
  expect(order(4, [a('t1', 'idle', { pinned: true }), a('t2', 'blocked', { pinned: true })])).toEqual(['t1', 't2'])
})

test('with no pins on the computer there is no Pinned group', () => {
  const v = new AgentList().view([a('t1', 'idle'), a('t2', 'working')], [], 1)
  expect(v.groups.map((g) => g.id)).toEqual(['sec-2', 'sec-1'])
})

test('grouped view puts neighbours of one space together under its name', () => {
  const l = new AgentList()
  l.sort = 'grouped'
  const v = l.view([a('t1', 'idle'), a('t2', 'working'), a('t3', 'idle', { space: 's2', space_name: 'web' })], [], 1)
  expect(v.groups.map((g) => [g.label, g.members.length])).toEqual([['api', 2], ['web', 1]])
})

test('a parent ranks by its most urgent live sub-agent, and sub-agents come as a tree', () => {
  const l = new AgentList()
  const terms = [
    { address: 't1', state: 'live' },
    { address: 't8', state: 'live', parent: 't1', status: { state: 'blocked' } },
    { address: 't9', state: 'live', parent: 't8', status: { state: 'working' } },
    { address: 't2', state: 'live' },
  ]
  const v = l.view([a('t1', 'idle'), a('t2', 'working')], terms, 1)
  expect(v.groups[0].label).toBe('Needs you')
  expect(v.groups[0].members[0].address).toBe('t1')
  expect(v.subs.get('t1')!.map((s) => [s.term.address, s.lines.join(''), s.status])).toEqual([['t8', '└', 'blocked'], ['t9', '└', 'working']])
})

test('a row detail is a named tab, else the agent title, else its program, never an automatic tab number', () => {
  const l = new AgentList()
  const v = l.view([
    a('t1', 'idle', { tab_name: 'fix login', named: true, title: 'Login bug' }),
    a('t2', 'blocked', { tab_name: '2', title: 'Hello.txt file creation' }),
    a('t3', 'idle', { tab_name: '3' }),
    // A tab named after its program (how Herdr names a tab it starts an agent in) shows the agent title, as the desktop does.
    a('t4', 'idle', { tab_name: 'Claude Code', named: true, title: 'Refactor auth' }),
    // A title that only says where the shell is says nothing.
    a('t5', 'idle', { tab_name: '5', title: '~/src/api' }),
  ], [], 1)
  expect(['t1', 't2', 't3', 't4', 't5'].map((t) => v.detail.get(t))).toEqual(['fix login', 'Hello.txt file creation', 'Claude Code', 'Refactor auth', 'Claude Code'])
})
