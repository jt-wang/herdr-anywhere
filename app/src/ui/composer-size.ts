// How tall the input box under a terminal is (pane-bar.tsx, Composer).

/** The box's height with nothing in it, at the default text size; it grows with its text up to COMPOSER_MAX, then
 *  scrolls. */
export const COMPOSER_MIN = 36
export const COMPOSER_MAX = 132
/** Around its text: 8 above and 8 below (py-2) and its 1 px border. */
const AROUND = 18

/** `hint`: the box's hint as this phone lays it out at its text size, wrapped at the box's width (0 until that is
 *  known); `content`: what the platform measures of the typed text, when there is some. The box holds its whole hint at
 *  any text size (at Android's text size 2.0 a fixed 36 cut off its top half; one line tall, the second line of a hint
 *  that wraps), and grows with the text from there. */
export function composerHeight(m: { hint: number; content?: number }): number {
  const least = Math.max(COMPOSER_MIN, Math.ceil(m.hint + AROUND))
  if (m.content === undefined) return least
  return Math.max(least, Math.min(COMPOSER_MAX, m.content + 16))
}
