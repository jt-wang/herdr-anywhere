// The chat view's shape: turns as the computer sends them (GET /api/chat/{terminal}, companion/internal/chat).
// The phone knows nothing of any agent: the computer finds the record, reads it, and says when it is not sure.
import { formats, t } from '../strings/index.ts'

export type ChatStep = { id: string; kind: 'read' | 'edit' | 'run' | 'search' | 'web' | 'agent' | 'note' | 'think' | 'you' | 'other' | string; title: string
  /** The record says it failed (a command's exit code, a refused call). */
  failed?: boolean
  /** The lines an edit added and removed. */
  added?: number; deleted?: number }
/** An image of a turn or a step: `id` for ChatApi.image; `source`, where it came from (a file's path) when the record
 *  says, shown above it so it can be copied. */
export type ChatPicture = { id: string; source?: string }
export type ChatTurn = {
  id: string
  from: 'you' | 'other'
  said: string
  /** The images sent with it, fetched one by one (ChatApi.image). */
  images?: ChatPicture[]
  steps: ChatStep[]
  reply: string
  ask?: { text: string; options: string[] }
  end: 'open' | 'done' | 'stopped'
  secs?: number
}
/** `chat` false: no chat button. `unknown`: nothing sure to show, and why (the companion's chat.go lists them). */
/** `setup`: with unknown "setup-needed" or "setup-not-loaded", the agent whose chat setup (ChatSetup) is missing or
 *  not yet used. */
export type ChatAnswer = { chat: boolean; unknown?: string; setup?: string; turns?: ChatTurn[] }
/** A small file some agents' chats need in the agent's own folder (companion/internal/chat/setup.go): the phone shows
 *  what the computer says of it, by agent, and knows no agent by name. `reload`: what to type in an agent already
 *  running for it to use the file. */
export type ChatSetupState = 'no-agent' | 'absent' | 'installed' | 'old' | 'changed'
export type ChatSetup = { agent: string; name: string; file: string; reload?: string; version: number; state: ChatSetupState }
/** What installing or removing did: done, refused because the file was changed (or the agent isn't there), or failed. */
export type ChatSetupResult = { result: 'ok' | 'changed' | 'no-agent' | 'failed'; setup?: ChatSetup }
/** A command the agent ran: whether it failed, its exit code, the end of what it printed (`lines` counts all of it). */
export type ChatRun = { command: string; failed: boolean; code?: number; output: string; lines: number }
/** One place a file changed: its first line before and after, each line marked " ", "+" or "-". */
export type ChatHunk = { oldStart: number; newStart: number; lines: string[] }
/** What a step changed in a file; `cut`: more lines than the computer sends. */
export type ChatChange = { file: string; added: number; deleted: number; hunks: ChatHunk[]; cut?: boolean }
/** Which lines of a file a step read. */
export type ChatRead = { file: string; from: number; count: number; total: number }
/** A step's details: its raw input and output, and, when the computer could tell, what it was (`run`, `change`, `read`). */
export type ChatDetail = { input: string; output: string; images?: ChatPicture[]; run?: ChatRun; change?: ChatChange; read?: ChatRead }

/** A change's lines as the phone shows them: each with its number in the file (the old number for a removed line,
 *  the new one otherwise), a gap between hunks. */
export function changeRows(c: ChatChange): { key: string; mark: ' ' | '+' | '-' | 'gap'; num: number; text: string }[] {
  const rows: { key: string; mark: ' ' | '+' | '-' | 'gap'; num: number; text: string }[] = []
  c.hunks.forEach((h, hi) => {
    if (hi > 0) rows.push({ key: `${hi}-gap`, mark: 'gap', num: 0, text: '⋯' })
    let o = h.oldStart, n = h.newStart
    h.lines.forEach((l, li) => {
      const mark = l[0] === '+' || l[0] === '-' ? l[0] : ' '
      rows.push({ key: `${hi}-${li}`, mark, num: mark === '-' ? o : n, text: l.slice(1) })
      if (mark !== '+') o++
      if (mark !== '-') n++
    })
  })
  return rows
}

/** A file a reply names, as the computer found it: where it is there, its name and size, and what it is. */
export type ChatFile = { asked: string; path: string; name: string; kind: 'video' | 'pdf' | 'text' | 'image' | 'other'; size: number }

/** The endings of files worth a card: videos, PDFs, text, pictures, documents, archives, sound. */
const FILE_END = /\.(mp4|mov|m4v|webm|pdf|md|markdown|txt|log|csv|json|ya?ml|toml|html?|png|jpe?g|gif|webp|zip|docx?|xlsx?|pptx?|key|mp3|wav|m4a)$/i

/** The paths of files a reply names, in order, each once, at most 8: a word ending in a file's ending (in code or not,
 *  in a Markdown link or not), without a web address. Which are files the computer has, it says (ChatApi.files). */
export function pathsIn(text: string): string[] {
  const out: string[] = []
  for (const word of text.split(/[\s`'"()<>[\]{}|]+/)) {
    const w = word.replace(/[.,:;!?*]+$/, '')
    if (!w || w.includes('://') || !FILE_END.test(w) || !/^[\w~./@+-]+$/.test(w) || out.includes(w)) continue
    out.push(w)
    if (out.length === 8) break
  }
  return out
}

/** A file's size as people say it: 900 B, 3.1 KB, 18.4 MB, 1.2 GB (1000s, as phones and Finder count). */
export function sizeText(n: number): string {
  if (n < 1000) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1000, i = 0
  while (v >= 1000 && i < units.length - 1) { v /= 1000; i++ }
  return `${v < 10 ? v.toFixed(1) : v < 100 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}

/** A video's length as a player shows it: m:ss, or h:mm:ss from an hour. */
export function clockText(ms: number): string {
  const all = Math.floor(Math.max(0, ms) / 1000), h = Math.floor(all / 3600), m = Math.floor((all % 3600) / 60), sec = all % 60
  const two = (n: number) => String(n).padStart(2, '0')
  return h ? `${h}:${two(m)}:${two(sec)}` : `${m}:${two(sec)}`
}

export type ChatApi = {
  get(address: string): Promise<ChatAnswer>
  /** null: the step is gone (the agent moved to another session) or the computer doesn't know it. */
  step(address: string, id: string): Promise<ChatDetail | null>
  /** An image of a turn or a step as a data: URL; null when it is gone. */
  image(address: string, id: string): Promise<string | null>
  /** Which of the paths a reply names are files the computer has (POST /api/file/check), in order. */
  files(address: string, paths: string[]): Promise<ChatFile[]>
  /** A link to one file a video player can open (it sends no token); null when the file can't be sent. */
  fileLink(address: string, path: string): Promise<string | null>
  /** The start of a file as text (at most `max` bytes); null when it can't be read. */
  fileText(address: string, path: string, max: number): Promise<string | null>
  /** The computer's chat setups; none from a Companion from before them. */
  setups(): Promise<ChatSetup[]>
  /** Install (on) or remove a setup's file; `force` replaces or removes a file someone changed. */
  setSetup(agent: string, on: boolean, force?: boolean): Promise<ChatSetupResult>
}

const span = (secs: number) => {
  const m = Math.floor(secs / 60)
  return m < 60 ? formats().span('m', m) : formats().span('h', Math.floor(m / 60))
}

/** The one line a finished turn's steps fold behind. */
export function foldLine(turn: ChatTurn): string {
  const n = t('chatSteps', { n: turn.steps.filter((x) => x.kind !== 'you').length })
  return turn.secs ? `${t('chatWorked', { time: span(turn.secs) })} · ${n}` : n
}

/** Whether a turn shows its fold line, and its steps. */
export function shows(turn: ChatTurn, expanded: boolean): { fold: boolean; steps: boolean } {
  if (!turn.steps.length) return { fold: false, steps: false }
  if (turn.end === 'open') return { fold: false, steps: true }
  return { fold: true, steps: expanded }
}

/** What you said while the agent worked (steps of kind `you`): shown above a closed fold line, so a finished turn
 *  doesn't hide it; in its place among the steps once they show. */
export function outsideFold(turn: ChatTurn, expanded: boolean): ChatStep[] {
  const s = shows(turn, expanded)
  return s.fold && !s.steps ? turn.steps.filter((x) => x.kind === 'you') : []
}

/** How often an open chat view asks again: while the agent works or waits on you, its record grows; an agent that
 *  reports no status (OpenCode 1, `undefined`) never says when, so it is asked as often. */
export function refreshEvery(status: string | undefined): number {
  return status === undefined || status === 'working' || status === 'blocked' ? 3000 : 0
}

/** What the view says when the computer isn't sure. `agent`: the program's name, for a reason that names it. */
export function unknownText(reason: string, agent?: string): string {
  if (reason === 'not-reported') return t('chatNotReported', { agent: agent || 'agent' })
  if (reason === 'behind') return t('chatBehind')
  if (reason === 'format') return t('chatFormat')
  if (reason === 'no-record-yet') return t('chatEmpty')
  if (reason === 'no-record') return t('chatNoRecord')
  return t('chatUnsure')
}
