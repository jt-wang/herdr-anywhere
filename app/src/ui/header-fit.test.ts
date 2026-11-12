import { expect, test } from 'nsh/test'
import { headerChipTight, showsAWord, titleWhole } from './header-fit.ts'

// What line 2 of a pane's header showed of what the agent is doing, on the devices,
// as the platform laid it out (`onTextLayout`): the "·" and the activity go when not even its first word is whole.
const WRITE = 'Write: src/api/login.ts'
const BASH = 'Bash: npm test'
test("line 2 keeps what the agent is doing only while at least its first word is whole", () => {
  // iPhone SE 375 pt, fix login with +1 waiting: "Claude Co… · …".
  expect(showsAWord(WRITE, [{ text: '…' }])).toBe(false)
  // Android 360 dp at font scale 2.0: "Cla… ·", the activity squeezed to nothing.
  expect(showsAWord(WRITE, [])).toBe(false)
  expect(showsAWord(WRITE, [{ text: '' }])).toBe(false)
  // Round 1 on the iPhone SE: "Claude Co… · W…".
  expect(showsAWord(WRITE, [{ text: 'W…' }])).toBe(false)
  expect(showsAWord(BASH, [{ text: 'Bas…' }])).toBe(false)
  // Enough of it: Android 360 dp ("Bash: npm t…"), font scale 2.0 ("Bash…"), iPhone largest ("Bash:…"), whole.
  expect(showsAWord(BASH, [{ text: 'Bash: npm t…' }])).toBe(true)
  expect(showsAWord(BASH, [{ text: 'Bash…' }])).toBe(true)
  expect(showsAWord(BASH, [{ text: 'Bash:…' }])).toBe(true)
  expect(showsAWord(BASH, [{ text: BASH }])).toBe(true)
  expect(showsAWord('api', [{ text: 'api' }])).toBe(true)
})

test("an activity squeezed to no width is cut, whatever its text layout says", () => {
  // Android 360 dp at font scale 2.0 with only the rule above (952936f) still showed "Cla… ·": the activity there is
  // squeezed to about no width, and a box that narrow holds no word, whatever its text layout reports.
  expect(showsAWord(WRITE, [{ text: WRITE }], 0)).toBe(false)
  expect(showsAWord(BASH, [{ text: 'Bash…' }], 8)).toBe(false)
  expect(showsAWord(BASH, [{ text: 'Bash…' }], 34)).toBe(true)
  expect(showsAWord(BASH, [{ text: 'Bash…' }])).toBe(true)
})

test("the title's chip is tighter only from the text size where the header's text stops growing (1.3x)", () => {
  for (const scale of [0.85, 1, 1.15]) expect(headerChipTight(scale)).toBe(false)
  for (const scale of [1.3, 1.5, 2]) expect(headerChipTight(scale)).toBe(true)
})

test("the title is whole only when it got the width it takes on one line, so +N waiting gives way when it didn't", () => {
  // Android 360 dp with +1 waiting and the chat button, default text: the name takes 116 dp and got 70 ("Clau…").
  // A check on onTextLayout's lines left "+1 waiting ›" whole there, so the widths decide.
  expect(titleWhole(116, 70)).toBe(false)
  expect(titleWhole(116, 0)).toBe(false)
  expect(titleWhole(116, 115)).toBe(false)
  // A name 1 dp short is already cut ("Claude Co…"); only rounding is let through.
  expect(titleWhole(116, 115.5)).toBe(false)
  expect(titleWhole(116.05, 116)).toBe(true)
  expect(titleWhole(116, 116)).toBe(true)
  expect(titleWhole(116, 130)).toBe(true)
})
