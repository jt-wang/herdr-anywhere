import { expect, test } from 'nsh/test'
import { headerChipTight, showsAWord } from './header-fit.ts'

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
