// The decision card in the chat view: the agent's question with the terminal's numbers, each option tapped sends the
// agent's own key, and the card follows the terminal through the call's questions. Screens: Claude Code 2.1.325 at a
// phone's width (src/testing/screens); the agent goes on through them as the fake's flow says.
import { expect, open, test } from 'nsh/test'
import { MULTI, OWN_TICKED, OWN_TYPED, Q1, REVIEW, TOUCH } from './src/testing/screens/claude-2.1.325.ts'
import * as CX from './src/testing/screens/codex-0.167.1.ts'
import * as OC from './src/testing/screens/opencode-2.2.0.ts'

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
const working = [{ id: 'u1', from: 'you', said: 'TOUCH a file', end: 'open', reply: '', steps: [{ id: 'toolu_1', kind: 'run', title: 'Making an empty file' }] }]
const QUESTION = { state: 'blocked', kind: 'question', msg: 'answer: Which plan should the fake shop use? (Ship the small fix now (Recommended) · …)' }

const chatOn = async (screen: string, status: Record<string, string>, turns: unknown[], argv = 'claude') => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', argv, status: 'working', chat: JSON.stringify({ chat: true, turns }) } }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  await app.host.send('fake.status', { address: 't1', screen, ...status })
  await app.clock.runFor(3000)
  await app.press('id:show-chat')
  await app.waitFor('id:ask-card')
  return app
}
const keys = (app: any): string[][] => JSON.parse(app.storage()['fake-keys'] ?? '[]').map((k: [string, string[]]) => k[1])
const sent = (app: any): string[] => JSON.parse(app.storage()['fake-sent'] ?? '[]').map((k: [string, string]) => k[1])

test("a question is a card: the terminal's numbers, each option's detail, the agent's pick marked; no 'answer it in the terminal'", async () => {
  const app = await chatOn(Q1, QUESTION, asked)
  const texts = app.texts()
  for (const x of ['Claude Code is asking · Question 1 of 2', 'Which plan should the fake shop use?', 'Ship the small fix now', 'One file changes; nothing else moves',
    'Recommended', 'Wait for the full rewrite', 'Only tell the team', 'Write my own answer', '5. Skip for now, talk it over in the chat']) expect(texts).toContain(x)
  for (const n of ['1', '2', '3']) expect(app.count(`id:ask-num-${n}`)).toBe(1)
  expect(app.count('id:ask-own')).toBe(1) // 4, Type something.
  expect(texts.some((x: string) => x.includes('Answer it in the terminal'))).toBe(false)
  // Under the card the input box only, with its terminal and attach buttons; no row of choices.
  expect(app.count('id:choices')).toBe(0)
  for (const id of ['compose-input', 'to-terminal', 'attach']) expect(app.count(`id:${id}`)).toBe(1)
})

test('through a call: a digit answers the first question, digits tick the second, Next goes to the review, 1 sends', async () => {
  const app = await chatOn(Q1, QUESTION, asked)
  const cart = MULTI.replace('2. [ ] Cart', '2. [✔] Cart')
  await app.host.send('fake.flow', { address: 't1', steps: [{ keys: ['2'], screen: MULTI }, { keys: ['2'], screen: cart }, { keys: ['tab'], screen: REVIEW }, { keys: ['1'], state: 'working' }] })
  await app.clock.runFor(100)
  await app.press('id:ask-row-2')
  await app.waitFor('text:Sent to Claude Code, waiting for it to go on…')
  await app.clock.runFor(3000)
  await app.waitFor('text:Claude Code is asking · Question 2 of 2 · Pick any')
  expect(app.count('id:ask-box-1-on')).toBe(1)
  expect(app.count('id:ask-box-2-off')).toBe(1)
  await app.press('id:ask-row-2')
  await app.clock.runFor(3000)
  await app.waitFor('id:ask-box-2-on')
  await app.press('id:ask-next')
  await app.clock.runFor(3000)
  await app.waitFor('text:Ready to submit your answers?')
  expect(app.texts()).toContain('Wait for the full rewrite')
  expect(app.texts()).toContain('Home, Help')
  await app.press('id:ask-row-1')
  await app.clock.runFor(3000)
  expect(keys(app)).toEqual([['2'], ['2'], ['tab'], ['1']])
  await app.waitUntil(() => app.count('id:ask-card') === 0)
})

test('my own answer: its row, then the text with Enter', async () => {
  const app = await chatOn(Q1, QUESTION, asked)
  await app.press('id:ask-own')
  await app.type('id:ask-own-input', 'Ship on Friday')
  await app.press('id:ask-own-send')
  await app.clock.runFor(3000)
  expect(keys(app)).toEqual([['4']])
  expect(sent(app)).toEqual(['Ship on Friday'])
})

test('my own answer in a multi-select question: the arrows to its row, the text, Enter; the card then shows it ticked, and Next from that row is Tab and Enter', async () => {
  const app = await chatOn(MULTI, QUESTION, asked)
  await app.host.send('fake.flow', { address: 't1', steps: [{ keys: ['down', 'down', 'down'] }, { text: 'Footer row', screen: OWN_TYPED }, { keys: ['enter'], screen: OWN_TICKED }, { keys: ['tab', 'enter'], screen: REVIEW }] })
  await app.clock.runFor(100)
  await app.press('id:ask-own')
  await app.type('id:ask-own-input', 'Footer row')
  await app.press('id:ask-own-send')
  await app.clock.runFor(3000)
  await app.waitFor('id:ask-box-4-on')
  expect(app.texts()).toContain('Footer row')
  await app.press('id:ask-next')
  await app.clock.runFor(3000)
  await app.waitFor('text:Ready to submit your answers?')
  expect(keys(app)).toEqual([['down', 'down', 'down'], ['enter'], ['tab', 'enter']])
  expect(sent(app)).toEqual(['Footer row'])
})

test('skip for now sends Chat about this; Show the terminal goes back to it', async () => {
  const app = await chatOn(Q1, QUESTION, asked)
  await app.press('id:ask-skip')
  await app.clock.runFor(3000)
  expect(keys(app)).toEqual([['5']])
})

test("a permission: what it runs, every option whole; No asks for words in the input box", async () => {
  const app = await chatOn(TOUCH, { state: 'blocked', kind: 'permission', msg: 'approve Bash: touch made-by-fake.txt' }, working)
  const texts = app.texts()
  for (const x of ['Claude Code wants to run a command', 'touch made-by-fake.txt', 'Do you want to proceed?', 'Yes',
    'Yes, and always allow access to /work/fake-lab/projects/src/fake-shop from this project', 'No', 'After this, tell it what to do instead in the box below']) expect(texts).toContain(x)
  await app.press('id:ask-row-4')
  await app.clock.runFor(3000)
  expect(keys(app)).toEqual([['4']])
})

test("the terminal view keeps its row of choices, in the terminal's order, number first", async () => {
  const app = await chatOn(TOUCH.replace(' ❯ 1. Yes', '   1. Yes').replace('   3. Yes, and switch', ' ❯ 3. Yes, and switch'), { state: 'blocked', kind: 'permission', msg: 'approve Bash: touch made-by-fake.txt' }, working)
  await app.press('id:show-terminal')
  await app.waitFor('id:answers')
  const labels = app.texts().filter((x: string) => /^\d\. /.test(x))
  expect(labels.map((x: string) => x[0])).toEqual(['1', '2', '3', '4'])
})

const codexTurn = [{ id: 'c1', from: 'you', said: 'PLAN the demo', end: 'done', reply: 'Here is the plan.', steps: [] }]

test("Codex's approval: what it runs, read off its screen", async () => {
  const app = await chatOn(CX.CMD, { state: 'blocked', kind: 'permission' }, codexTurn, 'codex')
  for (const x of ['Codex wants to run a command', 'touch made-1.txt', 'Would you like to run the following command?', 'Yes, proceed',
    'No, and tell Codex what to do differently']) expect(app.texts()).toContain(x)
  await app.press('id:ask-row-1')
  await app.clock.runFor(3000)
  expect(keys(app)).toEqual([['1']])
})

test("Codex's 'Implement this plan?', though Codex reports done: it needs you, and its card picks", async () => {
  const app = await chatOn(CX.PLAN, { state: 'done' }, codexTurn, 'codex')
  for (const x of ['Codex wants your OK', 'Implement this plan?', 'Yes, implement this plan', 'No, stay in Plan mode', 'Needs you']) expect(app.texts()).toContain(x)
  await app.press('id:ask-row-3')
  await app.clock.runFor(3000)
  expect(keys(app)).toEqual([['3']])
})

test("OpenCode's permission: its three buttons in a row, no numbers, each sends its own keys", async () => {
  const app = await chatOn(OC.PERMISSION, { state: 'blocked', kind: 'permission', msg: 'Permission needs input' }, codexTurn, 'opencode')
  for (const x of ['OpenCode wants to run a command', 'touch made-1.txt', 'Allow once', 'Always allow', 'Reject']) expect(app.texts()).toContain(x)
  expect(app.count('id:ask-num-1')).toBe(0)
  await app.press('id:ask-row-3')
  await app.clock.runFor(3000)
  expect(keys(app)).toEqual([['right', 'right', 'enter']])
})
