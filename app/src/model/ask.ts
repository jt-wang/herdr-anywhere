// The decision card in the chat view: what the agent asked, as its record has it (the computer's ChatAsk), laid over
// where its terminal is now (its screen, model/menu.ts), and what each tap sends. The record has the words whole; the
// screen has the numbers, which question is up, the ticks, and the page it is on. Each tap's keys are the agent's
// own, as measured on the versions below with a made-up model, and the phone reads the screen again after them (store.press).
//
// Claude Code 2.1.325: a digit picks an option and goes to the next question (after the last, a review page, where
// 1 sends the answers and 2 cancels); in a multi-select question a digit ticks a box and the cursor stays, Tab goes
// on, Enter on the Submit row too. Its own answer: the row's digit, then the text and Enter (terminal.send with
// force). In a multi-select question that row is reached with the arrows, and the text's Enter unticks it, so Enter
// once more ticks it again. Tab goes on from an option's row; from the own row it stops on Submit, so Next there is Tab and
// Enter. A permission: a digit, at once; No ends the turn and Claude asks at its prompt.
//
// Codex 0.167.1: an approval's digit acts at once ("No, and tell Codex …" ends the turn). A question (Plan mode): a
// digit picks and goes on, the last one sends every answer; its own answer is None of the above with notes: that row
// highlighted, Tab, the text and Enter. "Implement this plan?": a digit (store.ts reads it off the screen, as Codex
// reports done meanwhile).
//
// OpenCode 2.2.0 (model/menu.ts has its keys): a permission's three buttons, unnumbered; a question's digits; its own
// answer, the row's digit, then text and Enter, in a multi-select question too; Tab goes on; Enter on the Submit page.
import type { Status } from './answers.ts'
import { asks } from './answers.ts'
import type { ChatAsk, ChatQuestion } from './chat.ts'
import type { Menu, Option } from './menu.ts'

/** What a tap sends: keys (terminal.keys), or text and Enter (terminal.send with force). */
export type Step = { keys: string[] } | { text: string }

export type CardRow = {
  n: number
  label: string
  detail?: string
  recommended?: boolean
  /** A multi-select option's tick, off the screen. */
  checked?: boolean
  /** Picking it ends the turn and the agent asks at its prompt what to do instead. */
  afterNo?: boolean
  steps: Step[]
}

export type Card = {
  kind: 'question' | 'review' | 'permission'
  /** The agent, as its report names it ("claude-code", "codex"): whose keys the own answer takes. */
  app?: string
  /** A permission to run a command or to change a file. */
  action?: 'run' | 'edit'
  /** Its options have no numbers on screen (OpenCode's permission row and Submit page): buttons, numbered here only
   *  to tell them apart. */
  unnumbered?: boolean
  /** Which question of the call is up, and how many there are, when the record has it. */
  index?: number
  count?: number
  multi: boolean
  question: string
  /** A permission: the tool ("Bash", "Write", "plan") and what it runs or changes, from the agent's report. */
  tool?: string
  what?: string
  /** The review page: each question and the answer that will be sent. */
  review?: { q: string; a: string }[]
  rows: CardRow[]
  /** The agent's own-answer row ("Type something."), with the words once they are typed (and its tick, in a multi-select question). */
  own?: { n: number; text?: string; checked?: boolean }
  /** "Chat about this": no answer, talk it over instead. */
  skip?: { n: number; label: string; steps: Step[] }
  /** A multi-select question: on to the next question, or to the review page. */
  next?: Step[]
  /** The highlighted row's number; one past the own-answer row when it is the Submit row. */
  cursor: number
}

/** The row the agent adds for an answer of one's own: Claude Code's, Codex's (its notes), OpenCode's. */
const TYPE_OWN = /^(Type something\.?|None of the above|Type your own answer)$/
const EDITS = /^(Write|Edit|MultiEdit|NotebookEdit)$/
const CHAT = /^Chat about this$/

const norm = (s: string) => s.replace(/\s+/g, '')
const CJK = /[\u3000-\u30ff\u3400-\u9fff\uac00-\ud7af\uff00-\uffef]/
/** Wrapped lines back into one: no space between two CJK characters. */
export function joinWrapped(lines: string[]): string {
  return lines.reduce((a, l) => (!a ? l : CJK.test(a.at(-1)!) && CJK.test(l[0]) ? a + l : `${a} ${l}`), '')
}

export function askCard(status: Status, menu: Menu | undefined, ask: ChatAsk | undefined): Card | undefined {
  if (status?.state !== 'blocked' || !menu?.options.length) return undefined
  const sel = menu.options.find((o) => o.selected)
  // Claude Code's own-answer row is the one before Chat about this; once an answer is typed there it shows the words.
  const chat = menu.options.find((o) => CHAT.test(o.label))
  const own = menu.options.find((o) => TYPE_OWN.test(o.label)) ?? (chat ? menu.options.find((o) => o.n === chat.n - 1) : undefined)
  const cursor = sel ? sel.n : menu.submit?.cursor ? (own?.n ?? 0) + 1 : 0
  const base = { multi: !!menu.multi, question: menu.question, cursor, ...(status.app ? { app: status.app } : {}), ...(menu.unnumbered ? { unnumbered: true } : {}) }
  const digit = (o: Option): Step[] => [{ keys: o.keys }]
  if (menu.unnumbered) menu = { ...menu, options: menu.options.map((o, i) => ({ ...o, n: i + 1 })) }
  if (menu.review) return { ...base, kind: 'review', multi: false, review: menu.review, rows: menu.options.map((o) => ({ n: o.n, label: joinWrapped(o.lines), steps: digit(o) })) }
  if (status.kind === 'permission' || (status.kind !== 'question' && !ask)) {
    const said = asks(status)
    const m = /^([\w.-]+):\s*(.*)$/s.exec(said)
    const tool = m ? m[1] : said
    const action = menu.action ?? (tool === 'Bash' ? 'run' : EDITS.test(tool) ? 'edit' : undefined)
    return { ...base, kind: 'permission', tool, what: m ? m[2].trim() : menu.subject ?? '', ...(action ? { action } : {}),
      rows: menu.options.map((o) => ({ n: o.n, label: joinWrapped(o.lines), ...(o.typed ? { afterNo: true } : {}), steps: digit(o) })) }
  }
  const qs = ask?.questions ?? []
  const at = qs.findIndex((q) => norm(q.text) === norm(menu.question))
  const q: ChatQuestion | undefined = qs[at]
  const listed = menu.options.filter((o) => o !== own && o !== chat)
  const fromRecord = !!q && q.options.length === listed.length && listed.every((o, i) => {
    const a = norm(o.lines[0]), b = norm(q.options[i].label)
    return a.startsWith(b) || b.startsWith(a)
  })
  const rows: CardRow[] = listed.map((o, i) => {
    const r = fromRecord ? q!.options[i] : undefined
    const label = r ? r.label : o.label
    const detail = r ? r.detail : joinWrapped(o.lines.slice(linesIn(o)))
    return { n: o.n, label, ...(detail ? { detail } : {}), ...(r?.recommended ? { recommended: true } : {}), ...(o.checked !== undefined ? { checked: o.checked } : {}), steps: digit(o) }
  })
  return { ...base, kind: 'question', ...(q ? { index: at + 1, count: qs.length } : {}), rows,
    ...(own ? { own: { n: own.n, ...(TYPE_OWN.test(own.label) ? {} : { text: joinWrapped(own.lines) }), ...(own.checked !== undefined && !TYPE_OWN.test(own.label) ? { checked: own.checked } : {}) } } : {}),
    ...(chat ? { skip: { n: chat.n, label: chat.label, steps: digit(chat) } } : {}),
    ...(menu.multi ? { next: [{ keys: menu.submit?.cursor ? ['enter'] : status.app === 'claude-code' && own && sel?.n === own.n ? ['tab', 'enter'] : ['tab'] }] } : {}) }
}

/** How many of an option's screen lines its label took (menu.ts joins a wrapped label that goes on in lower case). */
function linesIn(o: Option): number {
  let n = 1, got = o.lines[0]
  while (n < o.lines.length && got !== o.label) got = `${got} ${o.lines[n++]}`
  return n
}

/** The steps for the agent's own answer: its row, then the text. */
export function ownSteps(c: Card, text: string): Step[] {
  if (!c.own) return []
  const d = c.own.n - c.cursor
  const moves: Step[] = d ? [{ keys: Array(Math.abs(d)).fill(d > 0 ? 'down' : 'up') }] : []
  // Codex: None of the above, highlighted, takes notes after Tab; Enter sends them with the answer.
  if (c.app === 'codex') return [...moves, { keys: ['tab'] }, { text }]
  // OpenCode: the row's digit opens the answer (in a multi-select question, it ticks the row and takes its words).
  if (!c.multi || c.app === 'opencode') return [{ keys: [String(c.own.n)] }, { text }]
  return [...moves, { text }, { keys: ['enter'] }]
}
