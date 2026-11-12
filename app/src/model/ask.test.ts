// The decision card: what the agent asked (its record, from the computer) laid over where its terminal is now (the
// screen, model/menu.ts), and the keys each tap sends. Keys as measured on Claude Code 2.1.325 (model/ask.ts).
import { expect, test } from 'nsh/test'
import { askCard, ownSteps } from './ask.ts'
import { readMenu } from './menu.ts'
import { LONG, MULTI, ON_SUBMIT, OWN_TYPED, PLAN, Q1, QONE, REVIEW, TOUCH } from '../testing/screens/claude-2.1.325.ts'
import * as CX from '../testing/screens/codex-0.167.1.ts'
import * as OC from '../testing/screens/opencode-2.2.0.ts'
import type { ChatAsk } from './chat.ts'

// The computer's answer for the call on screen in Q1, MULTI and REVIEW (companion/internal/chat/claude/testdata).
const RECORD: ChatAsk = {
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
const asking = { state: 'blocked', app: 'claude-code', kind: 'question', msg: 'answer: Which plan should the fake shop use? (…)' }
const card = (screen: string, ask?: ChatAsk, status: any = asking) => askCard(status, readMenu('claude-code', screen), ask)
const rows = (c: any) => c.rows.map((r: any) => [r.n, r.label, r.detail ?? '', !!r.recommended])

test("the first of two questions: the record's words, the terminal's numbers, a digit picks", () => {
  const c = card(Q1, RECORD)!
  expect([c.kind, c.index, c.count, c.multi, c.question]).toEqual(['question', 1, 2, false, 'Which plan should the fake shop use?'])
  expect(rows(c)).toEqual([
    [1, 'Ship the small fix now', 'One file changes; nothing else moves', true],
    [2, 'Wait for the full rewrite', 'Two weeks more, every page changes', false],
    [3, 'Only tell the team', 'No change to the code', false],
  ])
  expect(c.rows.map((r) => r.steps)).toEqual([[{ keys: ['1'] }], [{ keys: ['2'] }], [{ keys: ['3'] }]])
  expect(c.own?.n).toBe(4)
  expect(ownSteps(c, 'Ship on Friday')).toEqual([{ keys: ['4'] }, { text: 'Ship on Friday' }])
  expect([c.skip?.n, c.skip?.steps]).toEqual([5, [{ keys: ['5'] }]])
  expect(c.next).toBe(undefined)
})

test('the multi-select second question: boxes off the screen, a digit ticks, Next is Tab', () => {
  const c = card(MULTI, RECORD)!
  expect([c.kind, c.index, c.count, c.multi]).toEqual(['question', 2, 2, true])
  expect(c.rows.map((r) => [r.n, r.label, r.checked])).toEqual([[1, 'Home', true], [2, 'Cart', false], [3, 'Help', true]])
  expect(c.rows[1].steps).toEqual([{ keys: ['2'] }])
  expect(c.next).toEqual([{ keys: ['tab'] }])
  // Its own answer: down from the highlighted option to that row, the text (with Enter, which unticks it), Enter ticks.
  expect(ownSteps(c, 'Footer row')).toEqual([{ keys: ['down', 'down', 'down'] }, { text: 'Footer row' }, { keys: ['enter'] }])
})

test("the review page: the answers it lists, its own Submit and Cancel", () => {
  const c = card(REVIEW, RECORD)!
  expect(c.kind).toBe('review')
  expect(c.review).toEqual([{ q: 'Which plan should the fake shop use?', a: 'Wait for the full rewrite' }, { q: 'Which pages should get the new banner?', a: 'Home, Help' }])
  expect(rows(c)).toEqual([[1, 'Submit answers', '', false], [2, 'Cancel', '', false]])
  expect(c.own).toBe(undefined)
  expect(c.skip).toBe(undefined)
})

test("long options that wrap on the phone's terminal take the record's words whole", () => {
  const long: ChatAsk = { text: '假商店的横幅这周先做哪一种，下周再补另一种？', options: [], questions: [{ header: '横幅', text: '假商店的横幅这周先做哪一种，下周再补另一种？', options: [
    { label: '先只改首页那一个文件，把横幅放到最上面，颜色先用深绿色，别的页面这周都不动', detail: '半小时能做完，改坏了也只影响首页这一处地方', recommended: true },
    { label: 'Redo the banner on every page of the fake shop at once so that they all look the same from the start', detail: 'Takes about two weeks and every page of the shop changes' },
    { label: '先不改', detail: '代码不动' }] }] }
  const c = card(LONG, long)!
  expect([c.index, c.count]).toEqual([1, 1])
  expect(rows(c)).toEqual(long.questions![0].options.map((o, i) => [i + 1, o.label, o.detail, !!o.recommended]))
})

test('a question the record does not have (yet): the screen alone, first line the label, the rest its detail', () => {
  const c = card(QONE, undefined)!
  expect([c.kind, c.index, c.count]).toEqual(['question', undefined, undefined])
  expect(rows(c)).toEqual([[1, 'Green', 'Calm and easy to read', false], [2, 'Orange', 'Loud, stands out', false]])
  expect([c.own?.n, c.skip?.n]).toEqual([3, 4])
})

test("a permission: what it wants to run from its report, each option whole, No asks for words after", () => {
  const c = card(TOUCH, undefined, { state: 'blocked', app: 'claude-code', kind: 'permission', msg: 'approve Bash: touch made-by-fake.txt' })!
  expect([c.kind, c.tool, c.what, c.question]).toEqual(['permission', 'Bash', 'touch made-by-fake.txt', 'Do you want to proceed?'])
  expect(c.rows.map((r) => [r.n, r.label, r.afterNo ?? false])).toEqual([
    [1, 'Yes', false],
    [2, 'Yes, and always allow access to /work/fake-lab/projects/src/fake-shop from this project', false],
    [3, 'Yes, and switch to auto mode · auto mode handles these prompts for you', false],
    [4, 'No', true],
  ])
  expect(c.own).toBe(undefined)
})

test('leaving plan mode is a permission with no command', () => {
  const c = card(PLAN, undefined, { state: 'blocked', app: 'claude-code', kind: 'permission', msg: 'approve plan' })!
  expect([c.kind, c.tool, c.what]).toEqual(['permission', 'plan', ''])
  expect(c.rows.map((r) => r.label)).toEqual(['Yes, and switch to default (ask each time) for this session', 'No'])
})

test('no card when the screen shows no menu, or the agent is not blocked', () => {
  expect(askCard(asking, undefined, RECORD)).toBe(undefined)
  expect(askCard({ ...asking, state: 'working' }, readMenu('claude-code', Q1), RECORD)).toBe(undefined)
})

// Codex 0.167.1 (src/testing/screens/codex-0.167.1.ts; keys as measured, model/ask.ts).
const CODEX_RECORD: ChatAsk = { text: 'Which color should the demo button be?', options: ['Teal (Recommended)', 'Orange', 'Gray'], questions: [
  { header: 'Color', text: 'Which color should the demo button be?', options: [
    { label: 'Teal', detail: 'Matches the demo logo', recommended: true }, { label: 'Orange', detail: 'Stands out on white' }, { label: 'Gray', detail: 'Quiet, fits any page' }] },
  { header: 'Size', text: 'How big should it be?', options: [{ label: 'Small', detail: '32 px tall' }, { label: 'Large', detail: '48 px tall' }] }] }
const codex = (screen: string, status: any, ask?: ChatAsk) => askCard({ state: 'blocked', app: 'codex', ...status }, readMenu('codex', screen), ask)

test("Codex's command approval: the command off the screen (its report has none), No asks for words after", () => {
  const c = codex(CX.CMD, { kind: 'permission' })!
  expect([c.kind, c.action, c.what, c.question]).toEqual(['permission', 'run', 'touch made-1.txt', 'Would you like to run the following command?'])
  expect(c.rows.map((r) => [r.n, r.label, r.afterNo ?? false])).toEqual([
    [1, 'Yes, proceed', false], [2, "Yes, and don't ask again for commands that start with `touch made-1.txt`", false], [3, 'No, and tell Codex what to do differently', true]])
  expect(codex(CX.PATCH, { kind: 'permission' })!.action).toBe('edit')
})

test("Codex's question: the record's words, None of the above is where my own answer goes, as notes", () => {
  const c = codex(CX.ASK1, { kind: 'question' }, CODEX_RECORD)!
  expect([c.kind, c.index, c.count]).toEqual(['question', 1, 2])
  expect(rows(c)).toEqual([[1, 'Teal', 'Matches the demo logo', true], [2, 'Orange', 'Stands out on white', false], [3, 'Gray', 'Quiet, fits any page', false]])
  expect(c.own?.n).toBe(4)
  // Down to None of the above, Tab opens its notes, the text and Enter send them.
  expect(ownSteps(c, 'Purple, like the logo')).toEqual([{ keys: ['down', 'down', 'down'] }, { keys: ['tab'] }, { text: 'Purple, like the logo' }])
  expect(c.skip).toBe(undefined)
})

test("the end of Codex's plan, once the phone has read it off the screen: a permission for the plan", () => {
  const c = codex(CX.PLAN, { kind: 'permission', msg: 'approve plan' })!
  expect([c.kind, c.tool, c.question]).toEqual(['permission', 'plan', 'Implement this plan?'])
  expect(c.rows.map((r) => r.label)).toEqual(['Yes, implement this plan', 'Yes, clear context and implement', 'No, stay in Plan mode'])
})

// OpenCode 2.2.0 (src/testing/screens/opencode-2.2.0.ts).
const opencode = (screen: string, status: any, ask?: ChatAsk) => askCard({ state: 'blocked', app: 'opencode', ...status }, readMenu('opencode', screen), ask)

test("OpenCode's permission: three buttons in its order, unnumbered, each its own keys; Reject asks for words after", () => {
  const c = opencode(OC.PERMISSION, { kind: 'permission', msg: 'Permission needs input' })!
  expect([c.kind, c.unnumbered, c.action, c.what]).toEqual(['permission', true, 'run', 'touch made-1.txt'])
  expect(c.rows.map((r) => [r.label, r.steps, r.afterNo ?? false])).toEqual([
    ['Allow once', [{ keys: ['enter'] }], false], ['Always allow', [{ keys: ['right', 'enter'] }], false], ['Reject', [{ keys: ['right', 'right', 'enter'] }], true]])
})

test("OpenCode's questions: the record's words; its own answer is its row's digit, then the text", () => {
  const c = opencode(OC.TWO_Q1, { kind: 'question', msg: 'Input needs response' }, CODEX_RECORD)!
  expect([c.index, c.count, c.unnumbered ?? false]).toEqual([1, 2, false])
  expect(rows(c)[0]).toEqual([1, 'Teal', 'Matches the demo logo', true])
  expect(ownSteps(c, 'Purple')).toEqual([{ keys: ['4'] }, { text: 'Purple' }])
  const m = opencode(OC.TWO_Q2, { kind: 'question' }, CODEX_RECORD)!
  expect([m.multi, m.next, m.rows.map((r) => r.checked)]).toEqual([true, [{ keys: ['tab'] }], [true, true]])
  expect(ownSteps(m, 'Medium too')).toEqual([{ keys: ['3'] }, { text: 'Medium too' }])
})

test("OpenCode's Submit page: the answers, one button", () => {
  const c = opencode(OC.TWO_SUBMIT, { kind: 'question' })!
  expect([c.kind, c.unnumbered, c.review, c.rows.map((r) => [r.label, r.steps])]).toEqual(['review', true,
    [{ q: 'Color', a: 'Gray' }, { q: 'Size', a: 'Small, Large, Medium too' }], [['Submit', [{ keys: ['enter'] }]]]])
})

// Claude Code 2.1.325, with a made-up model (the screens OWN_TICKED and ON_SUBMIT): Tab on an option's row goes to the
// review; on the own-answer row, typed in or not, it only moves the cursor to Submit.
test('a typed own answer in a multi-select question: its row by place, with its words and tick; on the Submit row, Next is Enter; on the own row, Tab and Enter', () => {
  const c = card(ON_SUBMIT, RECORD)!
  expect([c.index, c.cursor]).toEqual([2, 5])
  expect(c.rows.map((r) => [r.n, r.label, r.checked])).toEqual([[1, 'Home', false], [2, 'Cart', false], [3, 'Help', false]])
  expect(c.own).toEqual({ n: 4, text: 'Footer row', checked: true })
  expect(c.next).toEqual([{ keys: ['enter'] }])
  const typed = card(OWN_TYPED, RECORD)!
  expect([typed.own, typed.next]).toEqual([{ n: 4, text: 'Footer row', checked: false }, [{ keys: ['tab', 'enter'] }]])
})
