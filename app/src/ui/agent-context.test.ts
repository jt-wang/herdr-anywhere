import { expect, test } from 'nsh/test'
import { Store } from '../store.ts'
import { FakeCore, settle } from '../testing/fakecore.ts'
import { startable } from '../model/programs.ts'
import { agentWords } from './agent-row.tsx'

async function setup(state = 'working', msg = '') {
  const core = new FakeCore(), doc = { spaces: [] as any[] }
  core.addSpace(doc, 'space', '/synthetic/sample', 'Sample', [{ id: 'tab', argv: ['claude'], foreground: true, status: state, title: 'Review sorting', msg }])
  core.docs.set('', { value: doc, version: 1 })
  let now = 1000
  const store = new Store({ now: () => now, pipe: () => core, setInterval: (() => 0) as any, clearInterval: (() => {}) as any })
  await store.connect(); await settle()
  const words = agentWords(store), agent = () => store.shown().agents[0]
  return { core, store, words, agent, advance: async () => { now += 61000; store.sync(); await settle() } }
}

test('a task title with no independent activity still shows its actual working folder', async () => {
  const h = await setup()
  expect(h.words.main(h.agent())).toBe('Review sorting')
  expect(h.words.line(h.agent())).toBe('')
  expect(h.words.context(h.agent())).toBe('/synthetic/sample')
  h.store.disconnect()
})


test('independent activity takes precedence, but a duplicate task message falls back to context', async () => {
  const h = await setup('working', 'Checking empty input')
  expect(h.words.line(h.agent())).toBe('Checking empty input')
  expect(h.words.context(h.agent())).toBe('')
  h.core.setStatus('t1', 'working', 'claude-code', { msg: 'Review sorting' }); await settle()
  expect(h.words.line(h.agent())).toBe('')
  expect(h.words.context(h.agent())).toBe('/synthetic/sample')
  expect(h.store.activityOf('t1')).toBe('Review sorting')
  h.store.disconnect()
})

test('blocked questions remain in Needs You without a duplicate activity or location line', async () => {
  const h = await setup('blocked', 'approve Write: sample.ts')
  expect(h.store.asksOf('t1')).toBe('Write: sample.ts')
  expect(h.words.line(h.agent())).toBe('')
  expect(h.words.context(h.agent())).toBe('')
  h.store.disconnect()
})

test('context follows foreground folder and its branch, never a previous folder branch', async () => {
  const h = await setup()
  h.core.git.set('/synthetic/sample', '# branch.head feature-sort\n'); await h.advance()
  expect(h.words.context(h.agent())).toBe('/synthetic/sample · feature-sort')
  const t = h.core.terms.get('t1')!
  t.foreground = { ...t.foreground, cwd: '/synthetic/other' }
  h.core.emit('foreground', 't1', t.foreground); await settle()
  expect(h.words.context(h.agent())).toBe('/synthetic/other')
  h.core.git.set('/synthetic/other', '# branch.head feature-preview\n'); await h.advance()
  expect(h.words.context(h.agent())).toBe('/synthetic/other · feature-preview')
  h.core.git.set('/synthetic/other', '# branch.head feature-final\n'); await h.advance()
  expect(h.words.context(h.agent())).toBe('/synthetic/other · feature-final')
  h.store.disconnect()
})

test('context uses the existing home abbreviation and does not repeat the main title', async () => {
  const h = await setup()
  h.store.home = '/synthetic'
  expect(h.words.context(h.agent())).toBe('~/sample')
  h.store.renameTab('tab', '~/sample'); await settle()
  expect(h.words.context(h.agent())).toBe('')
  h.store.disconnect()
})

for (const program of startable()) test(`${program.name} uses the shared location subtitle without overriding name preferences`, async () => {
  const h = await setup()
  h.core.setStatus('t1', 'working', program.key === 'claude' ? 'claude-code' : program.key); await settle()
  expect(h.words.context(h.agent())).toBe('/synthetic/sample')
  expect(h.words.main(h.agent())).toBe('Review sorting')
  h.store.showNames = () => false
  expect(h.words.named(h.agent())).toBe(false)
  expect(h.words.context(h.agent())).toBe('/synthetic/sample')
  h.store.showNames = () => true
  expect(h.words.named(h.agent())).toBe(true)
  h.store.disconnect()
})


test('missing folder data creates no invented context', async () => {
  const h = await setup(), t = h.store.term('t1')!
  t.cwd = ''; t.foreground.cwd = ''
  expect(h.words.context(h.agent())).toBe('')
  h.store.disconnect()
})

test('a combined folder and branch already used as the main title is not repeated', async () => {
  const h = await setup()
  h.core.git.set('/synthetic/sample', '# branch.head feature-sort\n'); await h.advance()
  h.core.emit('title', 't1', { title: '' }); await settle()
  h.store.renameTab('tab', '/synthetic/sample · feature-sort'); await settle()
  expect(h.words.context(h.agent())).toBe('')
  h.store.disconnect()
})


test('a long location keeps the useful final folders and branch instead of a shared prefix', async () => {
  const h = await setup()
  const cwd = '/media/sample/archived-workspaces/seasonal-research/snapshots/client/catalog'
  const t = h.core.terms.get('t1')!
  t.foreground = { ...t.foreground, cwd }
  h.core.git.set(cwd, '# branch.head image-preview\n')
  h.core.emit('foreground', 't1', t.foreground); await settle()
  await h.advance()
  expect(h.words.context(h.agent())).toBe('…/client/catalog · image-preview')
  h.store.disconnect()
})
