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

/** Compact spacing at larger system text sizes leaves more room for the uncapped title.
 *  This changes spacing only; the header grows vertically with both text lines. */
export function headerChipTight(scale: number): boolean {
  return scale >= 1.25
}

/** Whether the title got the width it takes on one line (`natural`, measured apart from the header) or was cut ("Clau…").
 *  When it was and others are waiting, "+N waiting ›" gives way to "+N ›" so the agent's name gets the room. Widths,
 *  not `onTextLayout`'s lines: with those, Android at 360 dp kept "+1 waiting ›" beside a title drawn as "Clau…". */
export function titleWhole(natural: number, shown: number): boolean {
  return shown + 0.1 >= natural
}
