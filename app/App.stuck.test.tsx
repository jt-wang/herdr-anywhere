// An agent that asks one set of choices after another: the terminal view's row of choices and the chat view's
// decision card follow the screen to the next set, however it was answered (a button on the phone, or someone at
// the computer). Screens: Claude Code 2.1.325 at a phone's width (src/testing/screens), read off the real program.
import { expect, open, test } from 'nsh/test'
import { MULTI, Q1, THREE_Q1, THREE_Q1_DOWN, THREE_Q2 } from './src/testing/screens/claude-2.1.325.ts'

const RECORD = {
  text: 'Which plan should the fake shop use?', options: ['Ship the small fix now (Recommended)', 'Wait for the full rewrite', 'Only tell the team'],
  questions: [
    { header: 'Plan', text: 'Which plan should the fake shop use?', options: [
      { label: 'Ship the small fix now', detail: 'One file changes; nothing else moves', recommended: true },
      { label: 'Wait for the full rewrite', detail: 'Two weeks more, every page changes' },
      { label: 'Only tell the team', detail: 'No change to the code' }] },
    { header: 'Pages', text: 'Which pages should get the new banner?', multi: true, options: [
      { label: 'Home', detail: 'The first page people see' }, { label: 'Cart', detail: 'Where people pay' }, { label: 'Help', detail: 'Questions and answers' }] },
  ],
}
const asked = [{ id: 'u1', from: 'you', said: 'QMULTI please', end: 'open', reply: '', steps: [], ask: RECORD }]
const QUESTION = { state: 'blocked', kind: 'question', msg: 'answer: Which plan should the fake shop use? (Ship the small fix now (Recommended) · …)' }

const blockedOn = async (screen: string) => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', argv: 'claude', status: 'working', chat: JSON.stringify({ chat: true, turns: asked }) } }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  await app.host.send('fake.status', { address: 't1', screen, ...QUESTION })
  await app.clock.runFor(3000)
  return app
}
const choices = (app: any): string[] => app.texts().filter((x: string) => /^\d\. /.test(x))

test('the terminal view: a button answers the first question, and the row of choices becomes the second question', async () => {
  const app = await blockedOn(Q1)
  await app.waitFor('id:answers')
  expect(choices(app)).toEqual(['1. Ship the small fix now (Recommended)', '2. Wait for the full rewrite', '3. Only tell the team', '4. Type something.', '5. Chat about this'])
  await app.host.send('fake.flow', { address: 't1', steps: [{ keys: ['2'], screen: MULTI }] })
  await app.press('id:choice-2')
  await app.clock.runFor(3000)
  expect(choices(app).slice(0, 3)).toEqual(['1. Home', '2. Cart', '3. Help'])
})

test('the terminal view: answered at the computer, the row of choices becomes the second question', async () => {
  const app = await blockedOn(Q1)
  await app.waitFor('id:answers')
  await app.host.send('fake.screen', { address: 't1', screen: MULTI })
  await app.clock.runFor(3000)
  expect(choices(app).slice(0, 3)).toEqual(['1. Home', '2. Cart', '3. Help'])
})

test('the chat view: answered at the computer, the card becomes the second question', async () => {
  const app = await blockedOn(Q1)
  await app.press('id:show-chat')
  await app.waitFor('text:Claude Code is asking · Question 1 of 2')
  await app.host.send('fake.screen', { address: 't1', screen: MULTI })
  await app.clock.runFor(3000)
  await app.waitUntil(() => app.texts().some((x: string) => x.startsWith('Claude Code is asking · Question 2 of 2')))
  expect(app.shows('text:Which pages should get the new banner?')).toBe(true)
})

test('the cursor moves on the screen, the question the same, and the agents list is worked out again meanwhile: the buttons stay as they were; the next question makes new ones', async () => {
  const app = await blockedOn(THREE_Q1)
  await app.waitFor('id:answers')
  expect(choices(app)).toEqual(['1. Top shelf', '2. Bottom shelf', '3. Type something.', '4. Chat about this'])
  const draws = () => Number(app.storage()['fake-choice-draws'] ?? 0)
  const views = () => Number(app.storage()['fake-views'] ?? 0)
  expect(draws()).toBe(4)
  const viewsBefore = views()
  await app.host.send('fake.screen', { address: 't1', screen: THREE_Q1_DOWN })
  await app.clock.runFor(5000)
  expect(views()).toBeGreaterThan(viewsBefore)
  expect(draws()).toBe(4)
  await app.host.send('fake.flow', { address: 't1', steps: [{ keys: ['1'], screen: THREE_Q2 }] })
  await app.press('id:choice-1')
  await app.clock.runFor(3000)
  expect(choices(app)).toEqual(['1. Amber', '2. White', '3. Type something.', '4. Chat about this'])
  expect(draws()).toBe(6)
})

test('the home page: a button answers the first question, and its row becomes the second question, with no terminal attached', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', argv: 'claude', status: 'working', chat: JSON.stringify({ chat: true, turns: asked }) } }, clock: {} })
  await app.clock.runFor(100)
  await app.host.send('fake.status', { address: 't1', screen: Q1, ...QUESTION })
  await app.clock.runFor(3000)
  await app.waitFor('id:needs-t1-choice-2')
  await app.host.send('fake.flow', { address: 't1', steps: [{ keys: ['2'], screen: MULTI }] })
  await app.press('id:needs-t1-choice-2')
  await app.clock.runFor(3000)
  await app.waitFor('text:1. Home')
  expect(app.shows('text:1. Ship the small fix now (Recommended)')).toBe(false)
})
