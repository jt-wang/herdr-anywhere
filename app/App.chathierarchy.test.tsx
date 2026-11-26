import { expect, open, test } from 'nsh/test'
import { hierarchyTurn } from './fixtures/chat-hierarchy.ts'

const pane = async () => {
  const app = await open('./fixtures/fake.tsx', { width: 375, height: 812, host: { params: { scene: 'single', argv: 'codex', status: 'working', chat: JSON.stringify({ chat: true, turns: [hierarchyTurn] }) } }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  await app.press('id:show-chat')
  await app.waitFor(`text:${hierarchyTurn.reply}`)
  return app
}

test('working commands fold into one summary with progress, failures and an independent reply', async () => {
  const app = await pane()
  expect(app.count('id:step-command-0')).toBe(0)
  expect(app.count('id:fold-review')).toBe(1)
  expect(app.texts()).toContain('Working… · 15 steps')
  expect(app.texts()).toContain('Failed · 1')
  expect(app.texts()).toContain(hierarchyTurn.steps[13].title)
  expect(app.texts()).toContain('make test-catalog')
  expect(app.shows('id:reply-review')).toBe(true)
  expect(app.texts()).not.toContain(hierarchyTurn.steps[0].title)
})

test('expanded steps and details survive streaming updates and completion, and can fold again', async () => {
  const app = await pane()
  await app.press('id:fold-review')
  await app.press('id:step-command-7')
  await app.waitFor('text:output of command-7')
  const updated = { ...hierarchyTurn, steps: [...hierarchyTurn.steps, { id: 'check-next', kind: 'run', title: 'make test-empty' }] }
  await app.host.send('fake.chat', { chat: true, turns: [updated] })
  await app.clock.runFor(3200)
  expect(app.count('id:step-check-next')).toBe(1)
  expect(app.texts()).toContain('output of command-7')
  const asks = JSON.parse(app.storage()['fake-chat-asks'] ?? '[]') as string[]
  expect(asks.filter((x) => x.endsWith('/steps/command-7')).length).toBe(1)
  await app.host.send('fake.chat', { chat: true, turns: [{ ...updated, end: 'done', reply: 'The catalog checks pass.' }] })
  await app.clock.runFor(3200)
  expect(app.count('id:step-command-7')).toBe(1)
  expect(app.texts()).toContain('output of command-7')
  await app.press('id:fold-review')
  expect(app.count('id:step-command-7')).toBe(0)
  expect(app.texts()).toContain('Failed · 1')
  expect(app.texts()).toContain('The catalog checks pass.')
  await app.press('id:fold-review')
  await app.waitFor('text:output of command-7')
})

test('a collapsed update keeps messages added mid-turn visible and the reply outside the fold', async () => {
  const app = await pane()
  await app.host.send('fake.chat', { chat: true, turns: [{ ...hierarchyTurn, steps: [...hierarchyTurn.steps,
    { id: 'extra', kind: 'you', title: 'Check the keyboard too.' }, { id: 'last', kind: 'run', title: 'make test-keyboard' }] }] })
  await app.clock.runFor(3200)
  expect(app.count('id:step-command-0')).toBe(0)
  expect(app.count('id:you-extra')).toBe(1)
  expect(app.texts()).toContain('make test-keyboard')
  expect(app.texts()).toContain('Working… · 16 steps')
  await app.press('id:fold-review')
  expect(app.count('id:you-extra')).toBe(1)
  expect(app.count('id:reply-review')).toBe(1)
})

test('opening work keeps the summary in view instead of jumping to the bottom', async () => {
  const app = await pane()
  await app.press('id:fold-review')
  await app.clock.runFor(100)
  expect(app.shows('id:fold-review')).toBe(true)
  expect(app.shows('id:step-command-0')).toBe(true)
})

test('reading older work stays put through a poll; scrolling to the end follows new replies', async () => {
  const app = await pane()
  await app.press('id:fold-review')
  await app.scroll('id:chat-scroll', 10000)
  await app.clock.runFor(100)
  await app.scroll('id:chat-scroll', -200)
  await app.host.send('fake.chat', { chat: true, turns: [{ ...hierarchyTurn, reply: hierarchyTurn.reply + '\n\nA new check is running.' }] })
  await app.clock.runFor(3101)
  expect(app.texts()).toContain('A new check is running.')
  await app.scroll('id:chat-scroll', 10000)
  await app.clock.runFor(100)
  await app.host.send('fake.chat', { chat: true, turns: [{ ...hierarchyTurn, reply: hierarchyTurn.reply + '\n\nThe last check has passed.\n\nThe keyboard navigation also works.\n\nThe report is ready.' }] })
  await app.clock.runFor(3202)
  expect(app.texts()).toContain('The last check has passed.')
})

// Pi's icon is 800 points of its own, drawn at 15. NativeShell 0.3.0 counted that in the scroll view's
// content: a screen of nothing below the last turn, and a chat that scrolled sideways. e2e/chat-hierarchy-trace.test.py
// reads the trace of this.
test('a long Pi chat scrolled to its end ends at its last reply', async () => {
  const turns = Array.from({ length: 12 }, (_, n) => ({ id: `pi-${n}`, from: 'you', said: `Check orchard ${n}`,
    reply: `Orchard ${n}: ${'The apple trees are healthy. '.repeat(6)}`, end: 'done', steps: [] }))
  const app = await open('./fixtures/fake.tsx', { width: 375, height: 812, host: { params: { scene: 'single', argv: 'pi', status: 'done', chat: JSON.stringify({ chat: true, turns }) } }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  await app.press('id:show-chat')
  await app.waitFor('id:reply-pi-11')
  // Opened at its end by the app; then to the top and back by hand, as far as the scroll view goes.
  await app.scroll('id:chat-scroll', -100000)
  await app.clock.runFor(300)
  await app.scroll('id:chat-scroll', 100000)
  await app.clock.runFor(300)
})
