// What a pane's header gives up when it is short of room (space.tsx, PhoneHeader).

/** Below this many points (one 11.5 px letter and an ellipsis) a box holds no word. */
const NO_WORD_FITS = 12

/** Whether what the platform laid out of `full` on one line (`onTextLayout`'s lines) holds at least its first word whole
 *  ("Bash…" for "Bash: npm test" does; "W…" and "…" for "Write: …" don't). Line 2 drops the "·" and what the agent is
 *  doing when it doesn't, instead of ending in a lone "· …". `width`: the box the activity was given, when known. */
export function showsAWord(full: string, lines: { text: string }[], width?: number): boolean {
  if (width !== undefined && width < NO_WORD_FITS) return false
  const shown = lines.map((l) => l.text).join('').trim()
  if (!shown) return false
  if (shown === full.trim()) return true
  const kept = shown.replace(/\s*(…|\.\.\.)$/, '')
  const word = full.trim().split(/\s+/)[0].replace(/\p{P}+$/u, '')
  return kept.length >= word.length
}

/** Whether the title's chip drops its border and most of its padding: from the text size where the header's text stops
 *  growing (`HEADER_MAX_SCALE`, 1.3x), where those few points are what the title needs to stay whole ("fix log…" at
 *  Android's largest size). Below it the chip keeps its tab look. */
export function headerChipTight(scale: number): boolean {
  return scale >= 1.25
}
