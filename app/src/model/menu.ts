// A blocked agent's choices, read off its screen: the numbered menu it draws, one option per number. Only layouts
// checked against a real run are read; any other program, or a screen without its menu, gives nothing, and the
// phone falls back to the key row rather than guess.
//
// Claude Code 2.1.314, measured in a scratch herdr-core session: the menu is "❯ 1. Yes / 2. … / 4. No"
// under the question, the highlighted line marked ❯; pressing an option's digit picks it at once. "No" interrupts
// the tool, and Claude asks "What should Claude do instead?" at its prompt; "Type something." (AskUserQuestion)
// opens its text answer. Both want text next.
export type Option = {
  n: number
  /** The option's first line: what a button shows. */
  label: string
  /** Everything it says, wrapped lines joined and a detail line after " — ": what a long press shows. */
  full: string
  selected: boolean
  /** Picking it asks for text next: the composer takes the focus. */
  typed: boolean
  keys: string[]
  /** Its lines on screen, the first without its number (and box), as wrapped. */
  lines: string[]
  /** A multi-select option's box: ticked or not. Undefined for an option with no box. */
  checked?: boolean
}
export type Menu = {
  question: string
  options: Option[]
  /** Its options have boxes: a digit ticks one, and the answer goes on from the Submit row or with Tab. */
  multi?: boolean
  /** A multi-select question's unnumbered Submit row, and whether it is highlighted. */
  submit?: { cursor: boolean }
  /** Claude Code's page before it sends a set of answers: each question and the answer it will send. */
  review?: { q: string; a: string }[]
  /** An approval that says on screen what it is for (Codex): a command to run, or a file to change. */
  action?: 'run' | 'edit'
  subject?: string
  /** Its options have no numbers on screen (OpenCode's permission and Submit page): keys pick them, not digits. */
  unnumbered?: boolean
}

const READERS: Record<string, (screen: string) => Menu | undefined> = { 'claude-code': readClaude, codex: readCodex, opencode: readOpenCode }

/** The options as the phone shows them: the highlighted one first, then the others in the menu's order. */
export function shownOrder(m: Menu): Option[] {
  return [...m.options.filter((o) => o.selected), ...m.options.filter((o) => !o.selected)]
}

export function readMenu(app: string | null | undefined, screen: string): Menu | undefined {
  const read = app ? READERS[app] : undefined
  return read?.(screen)
}

const OPTION = /^(\s*)(❯\s*)?(\d+)\.\s+(.*)$/
const SUBMIT = /^\s*(❯\s*)?Submit$/
const BOX = /^\[([ ✔✓xX])\]\s+(.*)$/
const RULE = /^\s*[─╌━▔-]{3,}\s*$/
const TYPED = /^No\b|tell Claude|Type something|something else/i

function readClaude(screen: string): Menu | undefined {
  const lines = screen.split('\n').map((l) => l.replace(/\s+$/, ''))
  // The highlighted line: an option, or a multi-select question's Submit row.
  let cursor = -1
  for (let i = lines.length - 1; i >= 0 && cursor < 0; i--) { if (OPTION.exec(lines[i])?.[2] || SUBMIT.exec(lines[i])?.[1]) cursor = i }
  if (cursor < 0) return undefined
  // Up from the highlighted line to option 1, over the lines that belong to options.
  let first = -1
  for (let i = cursor; i >= 0; i--) {
    const m = OPTION.exec(lines[i])
    if (m) { first = i; if (m[3] === '1') break; continue }
    if (i === cursor || SUBMIT.test(lines[i])) continue
    if (!lines[i].trim() || RULE.test(lines[i]) || !/^\s/.test(lines[i])) return undefined
  }
  if (first < 0 || OPTION.exec(lines[first])?.[3] !== '1') return undefined
  const options: Option[] = []
  let submit: Menu['submit']
  let col = 0
  for (let i = first; i < lines.length; i++) {
    const line = lines[i]
    const m = OPTION.exec(line)
    if (m && Number(m[3]) === options.length + 1) {
      col = line.length - m[4].length
      const box = BOX.exec(m[4].trim())
      const label = (box ? box[2] : m[4]).trim()
      options.push({ n: Number(m[3]), label, full: label, lines: [label], selected: !!m[2], typed: false, keys: [m[3]], ...(box ? { checked: box[1] !== ' ' } : {}) })
      continue
    }
    if (RULE.test(line)) continue
    const s = SUBMIT.exec(line)
    if (s && options.some((o) => o.checked !== undefined)) { submit = { cursor: !!s[1] }; continue }
    const indent = line.length - line.trimStart().length
    if (!line.trim() || m || indent < col) break
    // A line under an option: its label wrapped (it goes on in lower case) or a line of detail.
    const o = options[options.length - 1]
    const text = line.trim()
    o.lines.push(text)
    if (/^[a-z(]/.test(text) && o.full === o.label) { o.label = `${o.label} ${text}`; o.full = o.label }
    else o.full = `${o.full} — ${text}`
  }
  for (const o of options) o.typed = TYPED.test(o.full)
  // The question: the paragraph right over the options.
  const said: string[] = []
  let i = first - 1
  while (i >= 0 && !lines[i].trim()) i--
  for (; i >= 0 && lines[i].trim() && !RULE.test(lines[i]); i--) said.unshift(lines[i].trim())
  const multi = options.some((o) => o.checked !== undefined)
  return { question: said.join(' '), options, multi, ...(submit ? { submit } : {}), ...reviewOf(lines.slice(0, Math.max(0, i))) }
}

/** Claude Code's review page, over its question: "Review your answers", then "● question" and "→ answer" lines
 *  (each may wrap). */
function reviewOf(lines: string[]): { review?: Menu['review'] } {
  const at = lines.findLastIndex((l) => l.trim() === 'Review your answers')
  if (at < 0) return {}
  const review: { q: string; a: string }[] = []
  let into: 'q' | 'a' | '' = ''
  for (const l of lines.slice(at + 1)) {
    const t = l.trim()
    if (!t) continue
    if (t.startsWith('● ')) { review.push({ q: t.slice(2).trim(), a: '' }); into = 'q' }
    else if (t.startsWith('→ ') && review.length) { review[review.length - 1].a = t.slice(2).trim(); into = 'a' }
    else if (into && review.length) review[review.length - 1][into] += ` ${t}`
  }
  return { review }
}

// Codex 0.167.1, measured at 46, 50 and 200 columns (src/testing/screens/codex-0.167.1.ts): its menus mark the highlighted option
// with "›" and end an approval's options with the key that also picks them ("(y)", "(esc)"); a digit picks at once.
// An approval says what it is for over its options: "$ <command>", or "Destination:" and the file. A question (Plan
// mode) is headed "Question 1/2 (…)", with each option's description in a column of its own; Codex adds "None of the
// above". The end of a plan asks "Implement this plan?" (Codex reports done meanwhile).
const CX_OPTION = /^(\s*)(›\s*)?(\d+)\.\s+(.*)$/
const CX_KEY = /\s*\((y|p|a|n|esc)\)$/
const CX_TYPED = /^No, and tell \w+ what to do/

function readCodex(screen: string): Menu | undefined {
  const lines = screen.split('\n').map((l) => l.replace(/\s+$/, ''))
  let cursor = -1
  for (let i = lines.length - 1; i >= 0 && cursor < 0; i--) if (CX_OPTION.exec(lines[i])?.[2]) cursor = i
  if (cursor < 0) return undefined
  let first = -1
  for (let i = cursor; i >= 0; i--) {
    const m = CX_OPTION.exec(lines[i])
    if (m) { first = i; if (m[3] === '1') break; continue }
    if (!lines[i].trim() || !/^\s/.test(lines[i])) return undefined
  }
  if (first < 0 || CX_OPTION.exec(lines[first])?.[3] !== '1') return undefined
  const options: Option[] = []
  let col = 0, descCol = 0
  for (let i = first; i < lines.length; i++) {
    const line = lines[i]
    const m = CX_OPTION.exec(line)
    if (m && Number(m[3]) === options.length + 1) {
      col = line.length - m[4].length
      // A question's option: its label, two spaces or more, then its description.
      const two = /^(.*?\S)\s{2,}(\S.*)$/.exec(m[4])
      descCol = two ? col + m[4].indexOf(two[2], two[1].length) : 0
      const label = two ? two[1] : m[4]
      options.push({ n: Number(m[3]), label, full: two ? `${label} — ${two[2]}` : label, lines: two ? [label, two[2]] : [label], selected: !!m[2], typed: false, keys: [m[3]] })
      continue
    }
    const indent = line.length - line.trimStart().length
    if (!line.trim() || m || indent < col) break
    const o = options[options.length - 1]
    const text = line.trim()
    if (descCol && indent >= descCol) { o.lines[o.lines.length - 1] += ` ${text}`; o.full += ` ${text}` }
    else { o.lines.push(text); o.label = `${o.label} ${text}`; o.full = o.label }
  }
  for (const o of options) {
    const last = o.lines.length - 1
    if (!descCol) {
      o.lines[last] = o.lines[last].replace(CX_KEY, '')
      if (!o.lines[last] && last > 0) o.lines.pop()
      o.label = o.label.replace(CX_KEY, '').trim()
      o.full = o.full.replace(CX_KEY, '').trim()
    }
    o.typed = CX_TYPED.test(o.label)
  }
  // What it asks: an approval's "Would you like …" paragraph, else the paragraph right over the options (a question
  // without its "Question 1/2" line).
  const above = lines.slice(0, first)
  const para = (end: number) => { const out: string[] = []; let i = end; while (i >= 0 && !above[i].trim()) i--; for (; i >= 0 && above[i].trim(); i--) out.unshift(above[i].trim()); return out }
  const would = above.findLastIndex((l) => /^\s*Would you like/.test(l))
  let question = '', action: Menu['action'], subject: string | undefined
  if (would >= 0) {
    let end = would
    while (end + 1 < above.length && above[end + 1].trim()) end++
    question = above.slice(would, end + 1).map((l) => l.trim()).join(' ')
    const run = above.findLastIndex((l) => /^\s*\$ /.test(l))
    const dest = above.findLastIndex((l) => /^\s*Destination:/.test(l))
    if (run > would) { action = 'run'; subject = para(above.length - 1).join(' ').replace(/^\$ /, '') }
    else if (dest > would) {
      action = 'edit'
      const path = [above[dest].trim().replace(/^Destination:\s*/, '')]
      for (let i = dest + 1; i < above.length && above[i].trim(); i++) path.push(above[i].trim())
      subject = path.join('')
    }
  } else question = para(above.length - 1).filter((l) => !/^Question \d+\/\d+\b/.test(l)).join(' ')
  return { question, options, multi: false, ...(action ? { action, subject } : {}) }
}

// OpenCode 2.2.0, measured at 46, 50 and 200 columns (src/testing/screens/opencode-2.2.0.ts): its prompts sit in a box of "┃" lines,
// and the highlighted option is marked by color only. A permission is a row "Allow once  Always allow  Reject",
// unnumbered: Enter takes the first, Right moves on (digits do nothing); Reject ends the turn. A question has
// numbered options with their descriptions under them and "Type your own answer" last (its digit opens the answer,
// then text and Enter); a digit sends a single question at once, or goes to the next; a multi-select one has
// "[✓]" boxes and goes on with Tab. Several questions end on a Submit page ("Color: Gray"), sent with Enter.
const OC_ROW = /^\s*Allow once\s+Always allow\s+Reject$/
const OC_OWN = /^Type your own answer$/

function readOpenCode(screen: string): Menu | undefined {
  const lines = screen.split('\n').map((l) => l.replace(/\s+$/, '').replace(/^\s*┃ ?/, ''))
  const plain = (label: string, keys: string[], typed = false): Option => ({ n: 0, label, full: label, lines: [label], selected: false, typed, keys })
  const row = lines.findLastIndex((l) => OC_ROW.test(l))
  const asked = lines.findLastIndex((l) => /Permission required/.test(l))
  if (row >= 0 && asked >= 0 && asked < row) {
    const run = lines.slice(asked, row).find((l) => /^\s*\$ /.test(l))
    return { question: '', multi: false, unnumbered: true, ...(run ? { action: 'run', subject: run.trim().slice(2) } : {}),
      options: [plain('Allow once', ['enter']), plain('Always allow', ['right', 'enter']), plain('Reject', ['right', 'right', 'enter'], true)] }
  }
  const head = lines.findLastIndex((l) => l.trim() === 'Questions')
  if (head < 0) return undefined
  const box = lines.slice(head + 1)
  const first = box.findIndex((l) => /^\s*1\.\s/.test(l))
  if (first < 0) {
    if (!box.some((l) => /enter submit/.test(l))) return undefined
    const review = box.map((l) => l.trim()).filter((l) => /^[^:⇆]+: /.test(l)).map((l) => ({ q: l.slice(0, l.indexOf(': ')), a: l.slice(l.indexOf(': ') + 2) }))
    return { question: '', multi: false, unnumbered: true, review, options: [plain('Submit', ['enter'])] }
  }
  const options: Option[] = []
  let col = 0
  for (const line of box.slice(first)) {
    const m = OPTION.exec(line)
    if (m && !m[2] && Number(m[3]) === options.length + 1) {
      col = line.length - m[4].length
      const box = BOX.exec(m[4].trim())
      const label = (box ? box[2] : m[4]).trim()
      options.push({ n: Number(m[3]), label, full: label, lines: [label], selected: false, typed: OC_OWN.test(label), keys: [m[3]], ...(box ? { checked: box[1] !== ' ' } : {}) })
      continue
    }
    const indent = line.length - line.trimStart().length
    if (!line.trim() || m || indent < col || !options.length) break
    const o = options[options.length - 1]
    const text = line.trim()
    o.lines.push(text)
    if (/^[a-z(]/.test(text) && o.full === o.label) { o.label = `${o.label} ${text}`; o.full = o.label }
    else o.full = `${o.full} — ${text}`
  }
  const said: string[] = []
  let i = first - 1
  while (i >= 0 && !box[i].trim()) i--
  for (; i >= 0 && box[i].trim(); i--) said.unshift(box[i].trim())
  return { question: said.join(' '), options, multi: options.some((o) => o.checked !== undefined) }
}
