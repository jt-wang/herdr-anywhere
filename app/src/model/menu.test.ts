// Reading a blocked agent's menu off its screen. The screens are Claude Code 2.1.314's, captured with
// `hc read` in a scratch herdr-core session (paths changed to fake ones).
import { expect, test } from 'nsh/test'
import { readMenu } from './menu.ts'
import { LONG, MULTI, ON_SUBMIT, OWN_TYPED, PLAN, REVIEW } from '../testing/screens/claude-2.1.325.ts'
import * as CX from '../testing/screens/codex-0.167.1.ts'
import * as OC from '../testing/screens/opencode-2.2.0.ts'
import * as AG from '../testing/screens/agy-1.5.14.ts'
import { joinWrapped } from './ask.ts'

const RULE = '─'.repeat(60)
const DASH = '╌'.repeat(60)

const BASH = `
⏺ Bash(touch made-by-claude.txt)
  ⎿  Waiting…

${RULE}
 Bash command
 Tip: auto mode handles these prompts for you — choose "switch to auto mode" below
 Create empty file made-by-claude.txt
${DASH}
 touch made-by-claude.txt
${DASH}
 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and always allow access to /Users/sam/src/demo-app from this project
   3. Yes, and switch to auto mode · auto mode handles these prompts for you
   4. No

 Esc to cancel · Tab to amend
`

const QUESTION = `
❯ Use the AskUserQuestion tool to ask me which color I like.
${RULE}
 ☐ Color

Which color do you like?

❯ 1. Red
     Red
  2. Blue
     Blue
  3. Green
     Green
  4. Type something.
${RULE}
  5. Chat about this

Enter to select · ↑/↓ to navigate · Esc to cancel
`

// The same permission menu on a phone-width terminal: the long labels wrap.
const NARROW = `
 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for git push
      commands in this project
   3. No, and tell Claude what to do
      differently (esc)

 Esc to cancel · Tab to amend
`

test("Claude Code's permission menu: every option in order, the highlighted one marked, No asks for text", () => {
  const m = readMenu('claude-code', BASH)!
  expect(m.question).toBe('Do you want to proceed?')
  expect(m.options.map((o) => [o.n, o.label, o.selected, o.typed])).toEqual([
    [1, 'Yes', true, false],
    [2, 'Yes, and always allow access to /Users/sam/src/demo-app from this project', false, false],
    [3, 'Yes, and switch to auto mode · auto mode handles these prompts for you', false, false],
    // Measured: 4 interrupts, and Claude asks "What should Claude do instead?" at its prompt.
    [4, 'No', false, true],
  ])
  expect(m.options.map((o) => o.keys)).toEqual([['1'], ['2'], ['3'], ['4']])
})

test("a question's options keep their first line as the label and the rest as detail; Type something asks for text", () => {
  const m = readMenu('claude-code', QUESTION)!
  expect(m.question).toBe('Which color do you like?')
  expect(m.options.map((o) => [o.n, o.label, o.typed])).toEqual([[1, 'Red', false], [2, 'Blue', false], [3, 'Green', false], [4, 'Type something.', true], [5, 'Chat about this', false]])
  expect(m.options[0].full).toBe('Red — Red')
})

test('wrapped labels are joined again', () => {
  const m = readMenu('claude-code', NARROW)!
  expect(m.options.map((o) => o.full)).toEqual(['Yes', "Yes, and don't ask again for git push commands in this project", 'No, and tell Claude what to do differently (esc)'])
  expect(m.options.map((o) => o.typed)).toEqual([false, false, true])
})

test('no menu, or a program whose layout is not known: nothing, never a guess', () => {
  expect(readMenu('claude-code', '❯ \n  ⏸ manual mode on')).toBe(undefined)
  expect(readMenu('pi', BASH)).toBe(undefined)
  expect(readMenu(null, BASH)).toBe(undefined)
  // A numbered list in the conversation above the prompt is not a menu: nothing is highlighted.
  expect(readMenu('claude-code', '⏺ Steps:\n  1. Build\n  2. Test\n\n❯ \n')).toBe(undefined)
})

test("a multi-select question: each option's box read off its label, the Submit row is no option's detail", () => {
  const m = readMenu('claude-code', MULTI)!
  expect(m.question).toBe('Which pages should get the new banner?')
  expect(m.multi).toBe(true)
  expect(m.options.map((o) => [o.n, o.label, o.checked, o.selected, o.typed])).toEqual([
    [1, 'Home', true, true, false], [2, 'Cart', false, false, false], [3, 'Help', true, false, false],
    [4, 'Type something', false, false, true], [5, 'Chat about this', undefined, false, false],
  ])
  expect(m.options[3].full).toBe('Type something')
  expect(m.submit).toEqual({ cursor: false })
})

test('a single-select question is not multi and has no Submit row', () => {
  const m = readMenu('claude-code', QUESTION)!
  expect(m.multi).toBe(false)
  expect(m.submit).toBe(undefined)
  expect(m.review).toBe(undefined)
})

test("the review page: the answers it lists, and its own Submit / Cancel", () => {
  const m = readMenu('claude-code', REVIEW)!
  expect(m.question).toBe('Ready to submit your answers?')
  expect(m.review).toEqual([
    { q: 'Which plan should the fake shop use?', a: 'Wait for the full rewrite' },
    { q: 'Which pages should get the new banner?', a: 'Home, Help' },
  ])
  expect(m.options.map((o) => [o.n, o.label])).toEqual([[1, 'Submit answers'], [2, 'Cancel']])
})

test("leaving plan mode: the question is the paragraph over the options, a wrapped label is joined", () => {
  const m = readMenu('claude-code', PLAN)!
  expect(m.question).toBe('Claude wants to exit plan mode')
  expect(m.options.map((o) => o.label)).toEqual(['Yes, and switch to default (ask each time) for this session', 'No'])
})

test('long wrapped labels in Chinese: every number is there, in order', () => {
  const m = readMenu('claude-code', LONG)!
  expect(m.question).toBe('假商店的横幅这周先做哪一种，下周再补另一种？')
  expect(m.options.map((o) => o.n)).toEqual([1, 2, 3, 4, 5])
  expect(m.options[3].typed).toBe(true)
})

// Codex 0.167.1 at a phone's width (src/testing/screens/codex-0.167.1.ts).
test("Codex's approval: › marks the highlighted option, the shortcut in brackets is left off, what it runs is read", () => {
  const m = readMenu('codex', CX.CMD)!
  expect(m.question).toBe('Would you like to run the following command?')
  expect([m.action, m.subject]).toEqual(['run', 'touch made-1.txt'])
  expect(m.options.map((o) => [o.n, joinWrapped(o.lines), o.selected, o.typed])).toEqual([
    [1, 'Yes, proceed', true, false],
    [2, "Yes, and don't ask again for commands that start with `touch made-1.txt`", false, false],
    [3, 'No, and tell Codex what to do differently', false, true],
  ])
  expect(m.options.map((o) => o.keys)).toEqual([['1'], ['2'], ['3']])
})

test("Codex's change approval says which file", () => {
  const m = readMenu('codex', CX.PATCH)!
  expect([m.question, m.action, m.subject]).toEqual(['Would you like to make the following edits?', 'edit', '/work/fake-lab/projects/src/outside-4.txt'])
  expect(m.options.map((o) => joinWrapped(o.lines))).toEqual(['Yes, proceed', "Yes, and don't ask again for these files", 'No, and tell Codex what to do differently'])
})

test("Codex's question: the label, and the description in its own column under the wraps", () => {
  const m = readMenu('codex', CX.ASK1)!
  expect(m.question).toBe('Which color should the demo button be?')
  expect(m.options.map((o) => [o.n, o.label, o.full])).toEqual([
    [1, 'Teal (Recommended)', 'Teal (Recommended) — Matches the demo logo'],
    [2, 'Orange', 'Orange — Stands out on white'],
    [3, 'Gray', 'Gray — Quiet, fits any page'],
    [4, 'None of the above', 'None of the above — Optionally, add details in notes (tab)'],
  ])
  expect(readMenu('codex', CX.ASK2)!.question).toBe('How big should it be?')
})

test("the end of Codex's plan is a menu; its prompt after an ordinary turn is none", () => {
  const m = readMenu('codex', CX.PLAN)!
  expect(m.question).toBe('Implement this plan?')
  expect(m.options.map((o) => o.label)).toEqual(['Yes, implement this plan', 'Yes, clear context and implement', 'No, stay in Plan mode'])
  expect(readMenu('codex', CX.DONE)).toBe(undefined)
})

// OpenCode 2.2.0 at a phone's width (src/testing/screens/opencode-2.2.0.ts): it marks the highlighted option with color
// only, so nothing is selected here; its permission and its Submit page have no numbers.
test("OpenCode's permission: a row of three without numbers, Enter on the first, Right to the others", () => {
  const m = readMenu('opencode', OC.PERMISSION)!
  expect([m.unnumbered, m.action, m.subject]).toEqual([true, 'run', 'touch made-1.txt'])
  expect(m.options.map((o) => [o.label, o.keys, o.typed])).toEqual([
    ['Allow once', ['enter'], false], ['Always allow', ['right', 'enter'], false], ['Reject', ['right', 'right', 'enter'], true]])
})

test("OpenCode's questions: the question over its options, details under them, its own answer row, ticks", () => {
  const one = readMenu('opencode', OC.SINGLE)!
  expect(one.question).toBe('Which color should the demo button be?')
  expect(one.options.map((o) => [o.n, o.label, o.full, o.selected])).toEqual([
    [1, 'Teal (Recommended)', 'Teal (Recommended) — Matches the demo logo', false], [2, 'Orange', 'Orange — Stands out on white', false],
    [3, 'Gray', 'Gray — Quiet, fits any page', false], [4, 'Type your own answer', 'Type your own answer', false]])
  const two = readMenu('opencode', OC.TWO_Q2)!
  expect([two.question, two.multi]).toEqual(['How big should it be?', true])
  expect(two.options.map((o) => [o.n, o.label, o.checked])).toEqual([[1, 'Small', true], [2, 'Large', true], [3, 'Type your own answer', false]])
})

test("OpenCode's Submit page: each question's answer, and Enter sends them", () => {
  const m = readMenu('opencode', OC.TWO_SUBMIT)!
  expect(m.review).toEqual([{ q: 'Color', a: 'Gray' }, { q: 'Size', a: 'Small, Large, Medium too' }])
  expect([m.unnumbered, m.options.map((o) => [o.label, o.keys])]).toEqual([true, [['Submit', ['enter']]]])
})

test('the cursor on the Submit row: no option highlighted, the typed answer is its row', () => {
  const m = readMenu('claude-code', ON_SUBMIT)!
  expect(m.submit).toEqual({ cursor: true })
  expect(m.options.map((o) => [o.n, o.label, o.checked, o.selected])).toEqual([
    [1, 'Home', false, false], [2, 'Cart', false, false], [3, 'Help', false, false], [4, 'Footer row', true, false], [5, 'Chat about this', undefined, false]])
  const typed = readMenu('claude-code', OWN_TYPED)!.options[3]
  expect([typed.label, typed.checked, typed.selected]).toEqual(['Footer row', false, true])
})

// The Antigravity CLI 1.5.14 at 46 and 50 columns (src/testing/screens/agy-1.5.14.ts): "> " marks the highlighted
// option, a wrapped line starts at the screen's edge, and "Question 1/2: " heads a question.
for (const w of [46, 50] as const) {
  const at = (name: string) => (AG as Record<string, string>)[`${name}_${w}`]
  test(`agy's questions at ${w} columns: the question whole, Write-in typed, boxes`, () => {
    const one = readMenu('antigravity', at('Q1'))!
    expect([one.question, one.multi]).toEqual(['Which color should the button be?', false])
    expect(one.options.map((o) => [o.n, o.label, o.selected, o.typed, o.keys])).toEqual([
      [1, '(Recommended) Blue', true, false, ['1']], [2, 'Red', false, false, ['2']], [3, 'Write-in...', false, true, ['3']]])
    const two = readMenu('antigravity', at('Q2'))!
    expect([two.question, two.multi]).toEqual(['Which sizes should it come in?', true])
    expect(two.options.map((o) => [o.n, o.label, o.checked])).toEqual([[1, 'Small', false], [2, 'Large', false], [3, 'Write-in...', undefined]])
    const ticked = readMenu('antigravity', at('Q2_TICKED'))!
    expect(ticked.options.map((o) => [o.checked, o.selected])).toEqual([[true, true], [false, false], [undefined, false]])
    expect(readMenu('antigravity', at('WRITE_IN'))!.options.find((o) => o.selected)?.n).toBe(3)
  })
  test(`agy's approvals at ${w} columns: what for, wrapped options joined, No typed`, () => {
    const run = readMenu('antigravity', at('RUN'))!
    expect([run.action, run.subject, run.question]).toEqual(['run', "echo checking; echo 'one test failed' >&2; exit 3", 'Run this command?'])
    expect(run.options.map((o) => [o.n, o.selected, o.typed])).toEqual([[1, true, false], [2, false, false], [3, false, false], [4, false, true]])
    expect(run.options[0].label).toBe('Yes, run command')
    expect(joinWrapped(run.options[1].lines)).toBe("Yes, and always allow in this conversation for commands that start with 'echo'")
    expect(run.options[3].label).toBe('No, cancel')
    const create = readMenu('antigravity', at('CREATE'))!
    expect([create.action, create.subject, create.question]).toEqual(['edit', '/work/acme/hello.txt', 'Allow creation of this file?'])
    expect(create.options.map((o) => [o.n, o.typed])).toEqual([[1, false], [2, false], [3, true]])
    const edit = readMenu('antigravity', at('EDIT'))!
    expect([edit.action, edit.subject]).toEqual(['edit', '/work/acme/notes.txt'])
    expect(edit.options.map((o) => [o.label, o.typed])).toEqual([['Yes, accept this change', false], ['No, reject this change', true]])
  })
  test(`agy's plan review at ${w} columns: Approve and Reject, by their letters`, () => {
    const m = readMenu('antigravity', at('REVIEW'))!
    expect([m.unnumbered, m.question, m.action, m.subject]).toEqual([true, '', undefined, 'implementation_plan.md'])
    expect(m.options.map((o) => [o.label, o.keys, o.typed])).toEqual([['Approve', ['y'], false], ['Reject', ['n'], false]])
  })
}

test("agy's flush-left wrapped text is not another numbered option", () => {
  const screen = AG.Q1_46.replace('  2. Red', '2. from the setup instructions\n  2. Red')
  const m = readMenu('antigravity', screen)!
  expect(m.options.map((o) => [o.label, o.keys])).toEqual([
    ['(Recommended) Blue 2. from the setup instructions', ['1']], ['Red', ['2']], ['Write-in...', ['3']]])
})

test("agy's artifact review requires one highlighted artifact and names that artifact", () => {
  const screen = AG.REVIEW_46.replace('› □ new implementation_plan.md', '  □ new implementation_plan.md\n› □ new revised_plan.md')
  const m = readMenu('antigravity', screen)!
  expect(m.subject).toBe('revised_plan.md')
  expect(m.options.map((o) => o.keys)).toEqual([['y'], ['n']])
  expect(readMenu('antigravity', screen.replace('› □', '  □'))).toBe(undefined)
  expect(readMenu('antigravity', screen.replace('  □', '› □'))).toBe(undefined)
})

test("agy's screens without a menu give nothing", () => {
  expect(readMenu('antigravity', '> please ASK now\n\n  Hello from the fake model.\n\n? for shortcuts')).toBe(undefined)
  // A wrapped command line starting with ">" is not the cursor.
  expect(readMenu('antigravity', 'Requesting permission for:\n   ls\n>&2\n')).toBe(undefined)
})
