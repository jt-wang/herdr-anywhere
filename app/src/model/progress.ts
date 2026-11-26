// The progress page's data: tasks, each with the tasks under it, from `herdr-anywhere track tree --json`
// (companion/internal/track/goals.go), read through herdr-core's machine.exec. Nothing here is stored on the phone.
// Version one is view only: the computer files and judges every task; the phone only reads.
// Words: stages and flags come as codes and pane rows, worded here; titles and questions are written by the sorting
// agent in the phone's language, which the phone tells the computer (pickLine, langLine).
import { t, type Key } from '../strings/index.ts'
import { duration } from './agents.ts'

export type PaneRow = {
  id?: string; found?: boolean; state?: string; status?: string; name?: string; turns_since?: number; handed_on?: boolean; no_status?: boolean
  quiet?: boolean; quiet_for_ms?: number; msg?: string
}
export type Task = {
  id: string; outcome: string; stage: string; group: string; waiting?: string; next?: string; auto?: boolean; std?: string
  parent?: string; said?: string; evidence: string[]; pane_rows: PaneRow[]; updated?: number
  notes: { at: number; text: string }[]; children: Task[]; done: number; total: number
  /** Only on "needs you" rows: the titles above it. */
  path?: string[]
}
export type Tree = {
  needsYou: Task[]
  /** Finished in the last day, newest first, each with the titles above it. */
  justDone: Task[]
  tasks: Task[]; closed: Task[]; queue: number
  /** The filing pane is up but reports no status (a trust dialog or a login screen) while something waits for it. */
  organizerWaits?: { addr: string }
}

const list = <T>(v: unknown, f: (x: any) => T): T[] => (Array.isArray(v) ? v.map(f) : [])
const task = (x: any): Task => ({
  ...x, flags: undefined, evidence: list(x.evidence, String), pane_rows: list(x.pane_rows, (p) => p),
  notes: list(x.notes, (n) => n), children: list(x.children, task), done: x.done ?? 0, total: x.total ?? 0,
})

export function parseTree(s: string): Tree {
  const v = JSON.parse(s) ?? {}
  return {
    needsYou: list(v.needs_you, (x) => ({ ...task(x), path: list(x.path, String) })),
    justDone: list(v.just_done, (x) => ({ ...task(x), path: list(x.path, String) })),
    tasks: list(v.tasks, task), closed: list(v.closed, task), queue: typeof v.queue === 'number' ? v.queue : 0,
    organizerWaits: v.organizer_waits && typeof v.organizer_waits.addr === 'string' ? { addr: v.organizer_waits.addr } : undefined,
  }
}

// Stages come from the computer as codes (companion/internal/track); the phone words them.
const STAGE_KEY: Record<string, Key> = {
  waiting: 'stageWaiting', stuck: 'stageStuck', review: 'stageReview', doing: 'stageDoing', parked: 'stageParked', done: 'stageDone', dropped: 'stageDropped',
}
export const stageText = (group: string) => (STAGE_KEY[group] ? t(STAGE_KEY[group]) : group)
export const closed = (group: string) => group === 'done' || group === 'dropped'

/** What is wrong with a task's panes, worded on the phone from each pane's row (what track.go flags). */
export function flagsOf(task: Task): string[] {
  const out: string[] = []
  for (const p of task.pane_rows) {
    if (p.handed_on) continue
    const pane = p.name || p.id || '?'
    if (p.state === 'live' && p.status === 'blocked') out.push(p.msg ? t('flagAsks', { pane, msg: p.msg }) : t('flagBlocked', { pane }))
    else if (p.state === 'live' && p.status === 'error') out.push(t('flagError', { pane }))
    else if (p.quiet) out.push(t('flagQuiet', { pane, age: duration(p.quiet_for_ms ?? 0) }))
    else if (p.no_status) out.push(t('flagNoStatus', { pane }))
    else if (p.state === 'missing') out.push(t('flagMissing', { pane }))
  }
  return out
}

/** The line under a task's title: what it waits on, what is wrong, the agent's last words, or what comes next. */
export function subLine(task: Task): string {
  return task.waiting || flagsOf(task)[0] || task.said || task.next || ''
}

/** A task's own tasks as ✓/· lines. */
export function items(t: Task): { done: boolean; text: string }[] {
  return t.children.filter((c) => c.group !== 'dropped').map((c) => ({ done: c.group === 'done', text: c.outcome }))
}

function walk(ts: Task[], f: (t: Task, above: Task[]) => boolean, above: Task[] = []): boolean {
  for (const t of ts) if (f(t, above) || walk(t.children, f, [...above, t])) return true
  return false
}

export function findTask(tr: Tree, id: string): Task | undefined {
  let found: Task | undefined
  walk([...tr.tasks, ...tr.closed], (t) => (t.id === id ? ((found = t), true) : false))
  return found
}

/** The titles of the tasks above `id`, top first. */
export function pathOf(tr: Tree, id: string): string[] {
  let path: string[] = []
  walk([...tr.tasks, ...tr.closed], (t, above) => (t.id === id ? ((path = above.map((a) => a.outcome)), true) : false))
  return path
}

/** Every open task with its depth, for picking where something goes. */
export function flat(tr: Tree): { task: Task; depth: number }[] {
  const out: { task: Task; depth: number }[] = []
  const go = (ts: Task[], d: number) => { for (const t of ts) if (!closed(t.group)) { out.push({ task: t, depth: d }); go(t.children, d + 1) } }
  go(tr.tasks, 0)
  return out
}

/** The open task a pane works on now: the deepest one handed to it and not handed on since. */
export function taskOn(tr: Tree, addr: string): Task | undefined {
  let best: { task: Task; depth: number } | undefined
  for (const r of flat(tr)) if (r.task.pane_rows.some((p) => p.id === addr && !p.handed_on) && (!best || r.depth > best.depth)) best = r
  return best?.task
}

/** A command of the companion's own program: `runs` go one after another while each exits 0, through the
 *  companion's /api/cli, no shell between (Windows too). `line` is the same as a POSIX shell line, for a companion
 *  older than /api/cli, run through herdr-core's machine.exec (`sh -c`, which a Windows computer doesn't have). */
export type Cli = { runs: string[][]; line: string }

const q = (s: string) => `'${s.replace(/'/g, `'\\''`)}'`
// Where the README installs the companion; a shell started by machine.exec may not have ~/.local/bin on PATH.
const BIN = `b="$HOME/.local/bin/herdr-anywhere"; [ -x "$b" ] || b=herdr-anywhere; "$b"`
const cli = (...runs: string[][]): Cli => ({ runs, line: runs.map((r, i) => `${i ? '"$b"' : BIN} ${r[0]} ${r.slice(1).map(q).join(' ')}`).join(' && ') })

/** A `herdr-anywhere track` command for herdr-core only: `--herdr -` skips old Herdr's agent list. */
export function trackLine(args: string[]): Cli {
  return cli(['track', ...args, '--herdr', '-'])
}

/** Turns the computer's filing (the companion's progress loop) on or off with the page. */
export function switchLine(on: boolean): Cli {
  return cli(['progress', on ? 'on' : 'off'])
}

/** The agents on the computer that can sort what the user says. */
export function agentsLine(): Cli {
  return cli(['progress', 'agents'])
}

/** Makes `id` the sorting agent, writing in the phone's language `lang`, and turns sorting on. */
export function pickLine(id: string, lang: string): Cli {
  return cli(['progress', 'organizer', '--agent', id, '--lang', lang], ['progress', 'on'])
}

/** Tells the computer the phone's language changed: what the sorting agent writes in. */
export function langLine(lang: string): Cli {
  return cli(['progress', 'lang', lang])
}

/** `needs` and `version`: an agent too old to report its status; `script`: on Windows, one installed as a .cmd script
 *  whose program can't be found, which a pane can't start as the agent itself. The phone says either in its own language. */
export type AgentChoice = { id: string; name: string; usable: boolean; note: string; needs: string; version: string; script: boolean }

export function parseAgents(s: string): { agents: AgentChoice[]; chosen: string } {
  const v = JSON.parse(s) ?? {}
  const agents = list(v.agents, (a) => ({ id: String(a.id), name: String(a.name ?? a.id), usable: a.usable === true, note: typeof a.note === 'string' ? a.note : '',
    needs: typeof a.needs === 'string' ? a.needs : '', version: typeof a.version === 'string' ? a.version : '', script: a.script === true }))
  return { agents: [...agents.filter((a) => a.usable), ...agents.filter((a) => !a.usable)], chosen: typeof v.chosen === 'string' ? v.chosen : '' }
}

export type View = { name: 'home' } | { name: 'task'; id: string }

/** The live pane working on a task: the last one it was handed to that is still there. */
export function paneOf(task: Task): { addr: string; name?: string } | undefined {
  const p = [...task.pane_rows].reverse().find((p) => p.found && p.state === 'live' && !p.handed_on)
  return p?.id ? { addr: p.id, name: p.name } : undefined
}

/** A paragraph or list item of what an agent said: `mark` is • or the item's number, `depth` its nesting. */
export type Block = { text: string; mark?: string; depth?: number; head?: boolean }

/** The inline marks of Markdown dropped: bold, italics with _ or *, code, links (their words stay). */
function inline(s: string): string {
  return s.replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').replace(/\*\*([^*]+)\*\*/g, '$1').replace(/__([^_]+)__/g, '$1')
    .replace(/`([^`]*)`/g, '$1').replace(/(^|\s)\*([^*\s][^*]*)\*(?=\s|$|[.,;:!?。，；：！？])/g, '$1$2').trim()
}

/** What an agent said (Markdown, as agents write) as paragraphs and list items. */
export function blocks(md: string): Block[] {
  const out: Block[] = []
  let para: string[] = []
  const flush = () => { if (para.length) out.push({ text: para.join('\n') }); para = [] }
  for (const line of md.split('\n')) {
    if (/^\s*(```|~~~)/.test(line)) continue
    if (!line.trim()) { flush(); continue }
    const h = /^\s*#{1,6}\s+(.*)$/.exec(line)
    const li = /^(\s*)([-*+•]|\d+[.)])\s+(.*)$/.exec(line)
    if (h) { flush(); out.push({ text: inline(h[1]), head: true }); continue }
    if (li) {
      flush()
      const depth = Math.min(2, Math.floor(li[1].replace(/\t/g, '  ').length / 2))
      out.push({ mark: /\d/.test(li[2]) ? li[2].replace(')', '.') : '•', text: inline(li[3]), ...(depth ? { depth } : {}) })
      continue
    }
    para.push(inline(line))
  }
  flush()
  return out.filter((b) => b.text)
}

/** The same on one line, for a card. */
export function plain(md: string): string {
  return blocks(md).map((b) => (b.mark ? `${b.mark} ${b.text}` : b.text)).join(' ')
}

// The home page's structure, after Things 3's lists: loose tasks first, then each bigger task as a heading with its
// tasks under it; in each list what needs the user first, queued work after, and what finished today last, where it
// sits. Older finished and dropped work leaves the lists for the "done" entry at the bottom; a heading with nothing
// left to show is not shown.
const RANK: Record<string, number> = { waiting: 0, stuck: 0, review: 1, doing: 2, parked: 3, done: 4, dropped: 4 }
const byRank = (ts: Task[]) => ts.map((t, i) => ({ t, i })).sort((a, b) => (RANK[a.t.group] ?? 2) - (RANK[b.t.group] ?? 2) || a.i - b.i).map((x) => x.t)

/** A row under a heading: `head` when it has tasks of its own to show, which follow it one step in. */
export type Row = { task: Task; depth: number; head?: boolean }
/** `loose`: open tasks under no goal, first; `after`: ones under no goal finished today, after the goals. */
export type Home = { loose: Task[]; groups: { task: Task; rows: Row[] }[]; after: Task[]; done: Task[] }

export function homeOf(tr: Tree): Home {
  const needs = new Set(tr.needsYou.map((t) => t.id)), today = new Set(tr.justDone.map((t) => t.id))
  // What shows in the lists: open work, and work finished today; a task under "needs you" shows there only.
  const shown = (t: Task) => !needs.has(t.id) && (!closed(t.group) || today.has(t.id))
  const kids = (t: Task) => t.children.filter(shown)
  const rows = (ts: Task[], depth: number): Row[] => {
    const leaves = byRank(ts.filter((t) => !kids(t).length)), heads = byRank(ts.filter((t) => kids(t).length))
    return [
      ...leaves.map((task) => ({ task, depth })),
      ...heads.flatMap((task) => [{ task, depth, head: true }, ...rows(kids(task), Math.min(depth + 1, 2))]),
    ]
  }
  const all = [...tr.tasks, ...tr.closed]
  const top = all.filter(shown)
  return {
    loose: byRank(top.filter((t) => !kids(t).length && !closed(t.group))),
    after: top.filter((t) => !kids(t).length && closed(t.group)),
    groups: top.filter((t) => kids(t).length).map((task) => ({ task, rows: rows(kids(task), 0) })),
    done: all.filter((t) => closed(t.group) && !today.has(t.id)),
  }
}

/** The goal a task under "needs you" belongs to, named when the page shows more than one goal; else "". */
export function goalOf(tr: Tree, task: Task): string {
  return tr.tasks.filter((t) => !closed(t.group)).length > 1 ? task.path?.[0] ?? '' : ''
}

/** A pane on a task's page, by its name (the number only when two share a name): working on it (`live`), handed
 *  other work since (`moved`), or gone (`missing`). A closed task lists none. */
export type PaneLine = { label: string; kind: 'live' | 'moved' | 'missing'; addr?: string; row: PaneRow }

export function panesOf(task: Task): PaneLine[] {
  if (closed(task.group)) return []
  const name = (p: PaneRow) => p.name || p.id || '?'
  const twice = (p: PaneRow) => !!p.name && task.pane_rows.filter((q) => q.name === p.name).length > 1
  return task.pane_rows.map((p) => ({
    label: twice(p) && p.id ? `${name(p)} · ${p.id}` : name(p), addr: p.id, row: p,
    kind: p.handed_on ? 'moved' : p.found && p.state === 'live' ? 'live' : 'missing',
  }))
}

/** The rows left showing once the user folded some headings: a folded heading stays, the rows under it go. */
export function visibleRows(rows: Row[], folded: Set<string>): Row[] {
  const out: Row[] = []
  let hideBelow = -1
  for (const r of rows) {
    if (hideBelow >= 0 && r.depth > hideBelow) continue
    hideBelow = r.head && folded.has(r.task.id) ? r.depth : -1
    out.push(r)
  }
  return out
}
