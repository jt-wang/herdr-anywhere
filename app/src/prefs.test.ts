import { expect, test } from 'nsh/test'
import { DEFAULTS, PrefStore } from './prefs.ts'

test('Show agent names is on unless it was turned off, and a turned-off one comes back off', async () => {
  expect(DEFAULTS.showAgentNames).toBe(true)
  const saved = new Map<string, string>()
  const io = { load: async (k: string) => saved.get(k) ?? null, save: (k: string, v: string) => void saved.set(k, v) }
  const a = new PrefStore(io)
  await a.load()
  expect(a.prefs.get().showAgentNames).toBe(true)
  a.set({ showAgentNames: false })
  const b = new PrefStore(io)
  await b.load()
  expect(b.prefs.get().showAgentNames).toBe(false)
  // Settings saved before the switch existed keep the name.
  saved.set('prefs', JSON.stringify({ fontSize: 14 }))
  const c = new PrefStore(io)
  await c.load()
  expect(c.prefs.get().showAgentNames).toBe(true)
})

test('the last version a notification told of is kept across starts; settings from before it tell nothing yet', async () => {
  const saved = new Map<string, string>()
  const io = { load: async (k: string) => saved.get(k) ?? null, save: (k: string, v: string) => void saved.set(k, v) }
  const a = new PrefStore(io)
  await a.load()
  expect(a.prefs.get().toldUpdate).toBe('')
  a.set({ toldUpdate: '0.2.1' })
  const b = new PrefStore(io)
  await b.load()
  expect(b.prefs.get().toldUpdate).toBe('0.2.1')
  saved.set('prefs', JSON.stringify({ toldUpdate: 7 }))
  const c = new PrefStore(io)
  await c.load()
  expect(c.prefs.get().toldUpdate).toBe('')
})
