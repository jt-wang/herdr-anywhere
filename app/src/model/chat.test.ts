// The chat view's words and its refresh rule, apart from any agent: the phone renders the turns the computer sends.
import { expect, test } from 'nsh/test'
import { foldLine, refreshEvery, shows, unknownText, type ChatTurn, pathsIn, sizeText, changeRows, clockText } from './chat.ts'

const turn = (over: Partial<ChatTurn> = {}): ChatTurn => ({ id: 'u1', from: 'you', said: 'go', steps: [], reply: '', end: 'done', ...over })
const steps = (n: number) => Array.from({ length: n }, (_, i) => ({ id: `s${i}`, kind: 'run', title: `step ${i}` }))

test('a finished turn folds its steps behind one line: how long it worked and how many steps', () => {
  expect(foldLine(turn({ steps: steps(5), secs: 130 }))).toBe('Worked for 2m · 5 steps')
  expect(foldLine(turn({ steps: steps(1), secs: 20 }))).toBe('Worked for <1m · 1 step')
  expect(foldLine(turn({ steps: steps(3) }))).toBe('3 steps')
})

test('an open turn shows its steps; a finished one hides them until tapped; a turn with no steps has no line', () => {
  expect(shows(turn({ end: 'open', steps: steps(2) }), false)).toEqual({ fold: false, steps: true })
  expect(shows(turn({ steps: steps(2) }), false)).toEqual({ fold: true, steps: false })
  expect(shows(turn({ steps: steps(2) }), true)).toEqual({ fold: true, steps: true })
  expect(shows(turn({ end: 'stopped', steps: steps(2) }), false)).toEqual({ fold: true, steps: false })
  expect(shows(turn(), false)).toEqual({ fold: false, steps: false })
})

test('an agent that says its conversation only while it works: the view says so by its name, and that its next turn shows it', () => {
  expect(unknownText('not-reported', 'OpenCode')).toBe("Can't tell which conversation this is yet. It shows once OpenCode does some work.")
  expect(unknownText('ambiguous', 'OpenCode')).toBe("Can't tell for sure which conversation this is.")
})

test('the view asks again every 3 s while the agent works or waits, and when its status changes otherwise', () => {
  expect(refreshEvery('working')).toBe(3000)
  expect(refreshEvery('blocked')).toBe(3000)
  expect(refreshEvery('done')).toBe(0)
  expect(refreshEvery('idle')).toBe(0)
  // An agent that reports no status (OpenCode 1) never says when its record grows: asked every 3 s.
  expect(refreshEvery(undefined)).toBe(3000)
})

test('the files a reply names: words ending in a file\'s ending, in code or links or not, once each, no web addresses', () => {
  expect(pathsIn('Rendered `renders/demo.mp4` (42 s). See [the guide](docs/guide.pdf), NOTES.md and ~/Desktop/shot.PNG.')).toEqual(['renders/demo.mp4', 'docs/guide.pdf', 'NOTES.md', '~/Desktop/shot.PNG'])
  expect(pathsIn('https://example.com/a.pdf and a.pdf, a.pdf again; v1.2 is not a file, nor is e.g.')).toEqual(['a.pdf'])
  expect(pathsIn(Array.from({ length: 12 }, (_, i) => `f${i}.txt`).join(' '))).toHaveLength(8)
})

test('a file\'s size as people say it', () => {
  expect([900, 3_100, 18_400_000, 245_000_000, 1_200_000_000].map(sizeText)).toEqual(['900 B', '3.1 KB', '18.4 MB', '245 MB', '1.2 GB'])
})

test('a change\'s lines numbered as in the file: a removed line by its old number, the rest by their new one', () => {
  const rows = changeRows({ file: 'a', added: 2, deleted: 1, hunks: [{ oldStart: 41, newStart: 41, lines: [' a', '-b', '+c', '+d'] }, { oldStart: 90, newStart: 91, lines: [' z'] }] })
  expect(rows.map((r) => `${r.mark}${r.num}`)).toEqual([' 41', '-42', '+42', '+43', 'gap0', ' 91'])
})

test('a video\'s length reads as a player shows it', () => {
  expect([0, 999, 42_000, 61_500, 600_000, 3_723_000].map(clockText)).toEqual(['0:00', '0:00', '0:42', '1:01', '10:00', '1:02:03'])
})
