// The agents list: priority or grouped by space, pins, sub-agent trees and "a viewed done agent stays in Done
// until left" (herdr-app defaults/mods/agents/main.js).
import type { Shown } from './model.ts'
import { formats, t, type Key } from '../strings/index.ts'
import { pinOrder } from './pin-order.ts'

export type AgentInfo = {
  address: string
  pane: string
  space: string
  space_name: string
  tab: string
  tab_name: string
  /** The tab has a name someone gave it (else tab_name is its number). */
  named: boolean
  /** None for an agent that never reports its state, as OpenCode before 2.2 doesn't. */
  status: Shown | undefined
  app: string | null
  title: string
  seq: number
  focused: boolean
  /** Pinned on the computer: herdr-core's label `herdr.pinned` on its terminal, which every Herdr screen reads. */
  pinned: boolean
  /** Its place in Pinned (pin-order.ts): the label `herdr-anywhere.pin-order`, or the place a move is writing now. */
  place?: string
  programName: string
  programKey: string
  /** What a blocked agent says it waits for (its report's kind and msg). */
  kind?: string | null
  msg?: string
}

/** An agent's own title, when it tells more than the program's name or where its shell is (herdr-app agents mod). */
export function tellingTitle(title: string, program: string, key: string): string {
  const t = title.replace(/^[^\p{L}\p{N}]+/u, '').trim()
  const where = /^[~/]|^[A-Za-z]:\\/
  return t && t !== program && !where.test(t) && !where.test(title.trim()) && !t.includes('@') && t.toLowerCase() !== key ? t : ''
}

export type SubRow = { term: any; lines: string[]; status: string | undefined }
export type Group = { id: string; label: string; status?: Shown; members: AgentInfo[] }
export type Sort = 'priority' | 'grouped'

export const RANK: Record<string, number> = { blocked: 4, done: 3, working: 2, idle: 1 }
const SECTION: Record<number, Key> = { 4: 'needsYou', 3: 'done', 2: 'working', 1: 'idle' }
const SECTION_STATUS: Record<number, Shown> = { 4: 'blocked', 3: 'done', 2: 'working' }

export function duration(ms: number): string {
  const { span } = formats()
  const m = Math.floor(ms / 60000)
  if (m < 60) return span('m', Math.max(0, m))
  const h = Math.floor(m / 60)
  return h < 24 ? span('h', h) : span('d', Math.floor(h / 24))
}

/** A sub-agent without a pane: its raw word, error counting as blocked, never done (its parent's done is the news). */
export function kidStatus(k: any, listed: Map<string, string>): string | undefined {
  const s = listed.get(k.address) ?? k.status?.state ?? k.status
  return s === 'error' ? 'blocked' : s === 'done' ? 'idle' : s
}

export class AgentList {
  sort: Sort = 'priority'
  changed: Record<string, { s: string; at: number }> = {}
  folded = new Set<string>()
  private held: { address: string; rank: number } | null = null
  onPersist?: () => void

  since(a: AgentInfo) {
    return this.changed[a.address]?.at ?? 0
  }

  tree(all: any[], addr: string, lead: string[] = []): SubRow[] {
    const kids = all.filter((k) => k.parent === addr && k.state === 'live')
    return kids.flatMap((k, i) => {
      const last = i === kids.length - 1
      return [{ term: k, lines: [...lead, last ? '└' : '├'], status: undefined }, ...this.tree(all, k.address, [...lead, last ? '' : '│'])]
    })
  }

  view(input: AgentInfo[], all: any[], now: number) {
    const listed = new Map(input.map((a) => [a.address, a.status as string]))
    this.track(input, now)
    this.hold(input)
    const subs = new Map<string, SubRow[]>()
    for (const a of input) subs.set(a.address, this.tree(all, a.address).map((s) => ({ ...s, status: kidStatus(s.term, listed) })))
    const rankOf = (a: AgentInfo) => Math.max(this.rank(a), ...subs.get(a.address)!.map((s) => RANK[s.status ?? ''] ?? 0))
    const byPriority = this.sort === 'priority'
    let list = byPriority ? [...input].sort((x, y) => rankOf(y) - rankOf(x) || this.since(y) - this.since(x) || y.seq - x.seq) : [...input]
    // Pinned in the order they were pinned or dragged to, never by status: a change only changes a row's dot.
    const pinned = pinOrder(input.filter((a) => a.pinned))
    const rest = list.filter((a) => !a.pinned)
    list = [...pinned, ...rest]

    const groups: Group[] = pinned.length ? [{ id: 'pinned', label: t('pinned'), members: pinned }] : []
    for (const a of rest) {
      const r = rankOf(a) || RANK.idle
      const id = byPriority ? `sec-${r}` : `sp-${a.space}`
      const last = groups.at(-1)
      if (last?.id === id) last.members.push(a)
      else groups.push({ id, label: byPriority ? t(SECTION[r]) : a.space_name || '', status: byPriority ? SECTION_STATUS[r] : undefined, members: [a] })
    }

    // "space · detail": a tab someone named, else the agent's title, else its program; an automatic number only to
    // tell rows apart. A tab named after its program reads as unnamed, as on the desktop (agents mod title()).
    const label = (a: AgentInfo) => {
      if (a.named && a.tab_name !== a.programName) return a.tab_name
      return tellingTitle(a.title, a.programName, a.programKey) || a.programName || a.tab_name
    }
    const detail = new Map<string, string | null>(list.map((a) => [a.address, /^\d+$/.test(label(a)) ? null : label(a)]))
    const seen: Record<string, number> = {}
    for (const a of list) {
      const k = `${a.space_name}\u0000${detail.get(a.address) ?? ''}`
      seen[k] = (seen[k] ?? 0) + 1
    }
    for (const a of list) if (detail.get(a.address) == null && seen[`${a.space_name}\u0000`] > 1) detail.set(a.address, label(a))

    return { groups, order: list.map((a) => a.address), subs, detail, rankOf, byPriority }
  }

  private track(list: AgentInfo[], now: number) {
    let dirty = false
    for (const a of list) {
      const s = a.status === 'done' ? 'idle' : a.status ?? ''
      if (this.changed[a.address]?.s === s) continue
      this.changed[a.address] = { s, at: now }
      dirty = true
    }
    if (!dirty) return
    const live = new Set(list.map((a) => a.address))
    for (const [k, c] of Object.entries(this.changed)) if (!live.has(k) && now - c.at > 30 * 86400000) delete this.changed[k]
    this.onPersist?.()
  }

  private hold(list: AgentInfo[]) {
    const cur = list.find((a) => a.focused)
    const same = cur && cur.address === this.held?.address
    if (!same || cur!.status !== 'idle') this.held = cur ? { address: cur.address, rank: RANK[cur.status ?? ''] ?? 0 } : null
  }

  private rank(a: AgentInfo) {
    const sawDone = a.address === this.held?.address && this.held.rank === RANK.done && a.status === 'idle'
    return sawDone ? RANK.done : RANK[a.status ?? ''] ?? 0
  }

}
