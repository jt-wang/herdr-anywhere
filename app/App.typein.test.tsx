// Typing in the input box or straight into the terminal: one tap switches the pane you are on; a tap on the toast that
// follows keeps it for every pane, across restarts.
import { expect, open, test } from 'nsh/test'

const prefs = (app: any) => JSON.parse(app.storage()['fake-prefs'] ?? '{}')
const terminalMode = (app: any) => app.count('id:compose-input') === 0 && app.shows('id:key-row') && app.shows('id:to-box')
const openWeb = async (app: any) => { await app.waitFor('text:Needs you'); await app.press('id:space-sp2'); await app.waitFor('id:terminal-t3') }

test('the input box row switches this pane to the terminal; the next pane opens with the input box again', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' } } })
  await openWeb(app)
  expect(app.shows('id:compose-input')).toBe(true)
  await app.press('id:to-terminal')
  await app.waitUntil(() => terminalMode(app))
  // The key row has its esc there, and a keyboard button (the keyboard is down headless).
  expect(app.shows('id:row-esc')).toBe(true)
  expect(app.shows('id:show-keyboard')).toBe(true)
  expect(app.shows('text:Typing in the terminal in this pane')).toBe(true)
  expect(prefs(app).typeIn ?? 'composer').toBe('composer')
  await app.back()
  await openWeb(app)
  expect(app.shows('id:compose-input')).toBe(true)
  expect(prefs(app).typeIn ?? 'composer').toBe('composer')
})

test("the key row's pen brings the input box back", async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' } } })
  await openWeb(app)
  await app.press('id:to-terminal')
  await app.waitUntil(() => terminalMode(app))
  await app.press('id:to-box')
  await app.waitFor('id:compose-input')
  expect(app.count('id:to-box')).toBe(0)
})

test('a tap on the toast keeps the terminal for every pane, and the phone remembers it after a restart', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' } } })
  await openWeb(app)
  await app.press('id:to-terminal')
  await app.waitFor('text:Tap here to keep it for every pane.')
  await app.press('text:Tap here to keep it for every pane.')
  await app.waitFor('text:Every pane now opens typing in the terminal')
  expect(prefs(app).typeIn).toBe('terminal')
  await app.back()
  await openWeb(app)
  await app.waitUntil(() => terminalMode(app))
  const again = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', prefs: '{"typeIn":"terminal"}' } } })
  await openWeb(again)
  await again.waitUntil(() => terminalMode(again))
  // Switching back in a kept-terminal pane is a one-off too: its toast offers to keep the input box.
  await again.press('id:to-box')
  await again.waitFor('id:compose-input')
  expect(again.shows('text:Typing in the input box in this pane')).toBe(true)
})

test('typing in the terminal, Attach is in the ⋯ menu; with the input box it is the + next to it', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' } } })
  await openWeb(app)
  await app.press('id:space-more')
  await app.waitFor('id:menu')
  expect(app.count('id:menu-attach')).toBe(0)
  await app.press('id:menu-cancel')
  await app.waitUntil(() => app.count('id:menu') === 0)
  await app.press('id:to-terminal')
  await app.waitUntil(() => terminalMode(app))
  expect(app.count('id:attach')).toBe(0)
  await app.press('id:space-more')
  await app.waitFor('id:menu')
  expect(app.count('id:menu-attach')).toBe(1)
})

test('held Shift and Ctrl change only their own keys: every other label stays as it was', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' } } })
  await openWeb(app)
  await app.press('id:to-terminal')
  await app.waitUntil(() => terminalMode(app))
  const labels = ['esc', 'tab', '⌃', '⌃C', '←', '↑', '↓', '→', '↵']
  for (const l of labels) expect(app.shows(`text:${l}`)).toBe(true)
  await app.press('id:row-shift')
  await app.press('id:row-ctrl')
  await app.wait(100)
  for (const l of labels) expect(app.shows(`text:${l}`)).toBe(true)
  // No label carries a held key's mark (⇧^tab, ⇧^←): only the ⇧ key itself shows ⇧.
  expect(app.texts().filter((t: string) => /^[⇧^⌃]+./.test(t) && !labels.includes(t))).toEqual([])
})

test('the key row stays as it is while other agents change: a key under the finger is not drawn again, so its press counts', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' } } })
  await openWeb(app)
  await app.press('id:to-terminal')
  await app.waitUntil(() => terminalMode(app))
  await app.wait(300)
  const draws = () => Number(app.storage()['fake-row-draws'] ?? 0)
  const before = draws()
  expect(before).toBeGreaterThan(0)
  // An agent in another space stops waiting: the pane's "+1 waiting" goes, so the phone redrew.
  expect(app.shows('text:+1 waiting ›')).toBe(true)
  await app.host.send('fake.status', { address: 't1', state: 'working' })
  await app.waitUntil(() => !app.shows('text:+1 waiting ›'))
  await app.wait(300)
  expect(draws() - before).toBe(0)
})
