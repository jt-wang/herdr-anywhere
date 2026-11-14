import { expect, test } from 'nsh/test'
import { composerHeight } from './composer-size.ts'

// The hint as the phone lays it out, wrapped at the box's width: one line of 14 px text is about 16 at the default text
// size; at Android's text size 2.0 the empty box stayed 36 and cut the top of its hint ("Tell Claude", 360 dp), and a
// box one line tall showed the top 5 px of the hint's second line ("Code…").
test('the empty box holds its whole hint, at any text size', () => {
  expect(composerHeight({ hint: 16.4 })).toBe(36)
  expect(composerHeight({ hint: 0 })).toBe(36)
  expect(composerHeight({ hint: 33 })).toBe(51)
  expect(composerHeight({ hint: 21.4 })).toBe(40)
  // Two lines.
  expect(composerHeight({ hint: 66 })).toBe(84)
})

test('it grows with what is typed, from that height up to the most it grows', () => {
  expect(composerHeight({ hint: 16.4, content: 19 })).toBe(36)
  expect(composerHeight({ hint: 16.4, content: 60 })).toBe(76)
  expect(composerHeight({ hint: 16.4, content: 400 })).toBe(132)
  expect(composerHeight({ hint: 33, content: 33 })).toBe(51)
  expect(composerHeight({ hint: 33, content: 400 })).toBe(132)
})

test('a hint taller than the most it grows still shows whole', () => {
  expect(composerHeight({ hint: 130 })).toBe(148)
  expect(composerHeight({ hint: 130, content: 400 })).toBe(148)
})
