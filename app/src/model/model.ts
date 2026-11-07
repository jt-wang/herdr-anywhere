// The shared world of spaces → tabs → panes, plus this screen's focus and attention, as herdr-app keeps it
// (herdr-app/src/model.rs). Pure data: the store drives it from herdr-core.
import {
  type Axis, type Node, addresses, container, find, isPane, pane, panes, remove, split, swap, terminalBlocks, terminalOf, walk,
  structuredCloneNode,
} from './layout.ts'

export type Tab = {
  id: string
  name: string
  root: Node
  focused?: string
  before: string[]
  zoomed?: string
  settled?: number
}

export type Space = {
  id: string
  name: string
  folder: string
  machine?: string
  tabs: Tab[]
  active?: string
}

export type Recent = { name: string; folder: string }
export type Agent = { word?: string; app?: string; seen: boolean; seq: number; since: number }
export type FolderInfo = { top: string; branch?: string; ahead?: number; behind?: number }
export type Shown = 'working' | 'blocked' | 'done' | 'idle'
export type Alert = 'blocked' | 'done'

export function newTab(id: string, name: string, paneId: string, address: string): Tab {
  return withPane(id, name, pane(paneId, address))
}

export function withPane(id: string, name: string, block: Node): Tab {
  return { id, name, focused: block.id, before: [], root: container(id, 'row', [block]) }
}

/** Herdr's attention priority: blocked > done > working > idle > nothing. */
export function priority(s: string | undefined): number {
  return s === 'blocked' ? 4 : s === 'done' ? 3 : s === 'working' ? 2 : s === 'idle' ? 1 : 0
}

/** Needing you always alerts; finishing alerts only if the person wasn't looking. */
export function alert(prev: string | undefined, next: string | undefined, looking: boolean): Alert | undefined {
  const blocked = (w?: string) => w === 'blocked' || w === 'error'
  const busy = (w?: string) => w === 'working' || blocked(w)
  if (blocked(next) && !blocked(prev)) return 'blocked'
  if (busy(prev) && next !== undefined && !busy(next) && !looking) return 'done'
  return undefined
}

/** A folder as a name: its last part, `~` for the home folder. */
export function folderName(path: string, home: string): string {
  const trimmed = path.replace(/[/\\]+$/, '')
  const h = home.replace(/[/\\]+$/, '')
  if (path === '' || trimmed === h || trimmed.replace(/\\/g, '/') === h.replace(/\\/g, '/')) return '~'
  if (trimmed === '') return path.slice(0, 1)
  const parts = trimmed.split(/[/\\]/).filter((s) => s !== '')
  return parts.at(-1) ?? trimmed
}

export function randomHex(bytes = 16): string {
  const b = new Uint8Array(bytes)
  crypto.getRandomValues(b)
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
}

export const qualify = (id: string, machine?: string) => (machine ? `${machine}/${id}` : id)
export const local = (id: string, machine?: string) =>
  machine && id.startsWith(machine + '/') ? id.slice(machine.length + 1) : id
export const machineOfAddress = (addr: string) => (addr.includes('/') ? addr.split('/')[0] : undefined)

export class Model {
  spaces: Space[] = []
  active?: string
  recent: Recent[] = []
  paneFolders = new Map<string, string>()
  agents = new Map<string, Agent>()
  folderInfo = new Map<string, FolderInfo>()
  private seq = 0
  now: () => number = () => Date.now()

  freshId(prefix: string, machine?: string) {
    return qualify(`${prefix}${randomHex()}`, machine)
  }

  orderSpaces() {
    // Rust's Option ordering: this machine (None) first, then machines by name; stable inside each.
    const key = (s: Space) => (s.machine === undefined ? '' : '\u0001' + s.machine)
    this.spaces = this.spaces.map((s, i) => [s, i] as const)
      .sort((a, b) => (key(a[0]) < key(b[0]) ? -1 : key(a[0]) > key(b[0]) ? 1 : a[1] - b[1]))
      .map(([s]) => s)
  }

  moveSpace(id: string, index: number): boolean {
    this.orderSpaces()
    const from = this.spaces.findIndex((s) => s.id === id)
    if (from < 0) return false
    const machine = this.spaces[from].machine
    const first = this.spaces.findIndex((s) => s.machine === machine)
    const last = this.spaces.length - 1 - [...this.spaces].reverse().findIndex((s) => s.machine === machine)
    const to = Math.min(Math.max(Math.max(index - 1, 0), first), last)
    const [space] = this.spaces.splice(from, 1)
    this.spaces.splice(to, 0, space)
    return true
  }

  moveTab(id: string, index: number): boolean {
    const p = this.spaceOf(id)
    if (!p) return false
    const from = p.tabs.findIndex((c) => c.id === id)
    const [c] = p.tabs.splice(from, 1)
    p.tabs.splice(Math.min(Math.max(index - 1, 0), p.tabs.length), 0, c)
    return true
  }

  activeIndex(): number | undefined {
    if (this.active === undefined) return undefined
    const i = this.spaces.findIndex((p) => p.id === this.active)
    return i >= 0 ? i : this.spaces.length ? 0 : undefined
  }

  activeSpace(): Space | undefined {
    const i = this.activeIndex()
    return i === undefined ? undefined : this.spaces[i]
  }

  activeTab(): Tab | undefined {
    const p = this.activeSpace()
    return p && spaceActiveTab(p)
  }

  space(id: string) {
    return this.spaces.find((p) => p.id === id)
  }

  tabs(): Tab[] {
    return this.spaces.flatMap((p) => p.tabs)
  }

  tab(id: string) {
    return this.tabs().find((c) => c.id === id)
  }

  /** The space holding a tab or a node inside one. */
  spaceOf(node: string) {
    return this.spaces.find((p) => p.tabs.some((c) => c.id === node || find(c.root, node)))
  }

  tabOf(node: string) {
    return this.tabs().find((c) => find(c.root, node))
  }

  spaceName(p: Space, home: string): string {
    if (p.name) return p.name
    const first = p.tabs.flatMap((c) => terminalBlocks(c.root))[0]
    const folder = (first && (this.folderInfo.get(terminalOf(first)!)?.top ?? this.paneFolders.get(first.id))) ?? p.folder
    return folderName(folder, home)
  }

  static tabName(p: Space, c: Tab): string {
    if (c.name) return c.name
    return String(Math.max(p.tabs.findIndex((x) => x.id === c.id), 0) + 1)
  }

  addSpace(id: string, name: string, folder: string, tab: Tab, machine?: string) {
    this.spaces.push({ id, name, folder, machine, tabs: [tab], active: tab.id })
    this.recent = this.recent.filter((r) => r.name !== name)
    this.active = id
  }

  /** Adds a tab after the space's selected one and selects it (not the space). */
  addTab(space: string, tab: Tab): boolean {
    const p = this.space(space)
    if (!p) return false
    const i = p.active === undefined ? -1 : p.tabs.findIndex((c) => c.id === p.active)
    p.tabs.splice(i >= 0 ? i + 1 : p.tabs.length, 0, tab)
    p.active = tab.id
    return true
  }

  split(target: string, axis: Axis, newPane: string, address: string) {
    return this.splitWith(target, axis, pane(newPane, address))
  }

  splitWith(target: string, axis: Axis, node: Node): boolean {
    const machine = this.spaceOf(target)?.machine
    const c = this.tabOf(target)
    if (!c || !split(c.root, target, axis, node, this.freshId('s', machine))) return false
    c.zoomed = undefined
    focusIn(c, node.id)
    return true
  }

  movePane(source: string, target: string, edge?: 'top' | 'right' | 'bottom' | 'left'): boolean {
    if (source === target) return false
    const tab = this.tabOf(source)
    if (!tab) return false
    const s = find(tab.root, source)
    const t = find(tab.root, target)
    if (!s || !t || !isPane(s) || !isPane(t)) return false
    const root = structuredCloneNode(tab.root)
    if (edge) {
      const moved = remove(root, source)!
      delete moved.size
      const axis: Axis = edge === 'left' || edge === 'right' ? 'row' : 'column'
      if (!split(root, target, axis, moved, this.freshId('s', this.spaceOf(source)?.machine))) return false
      if (edge === 'left' || edge === 'top') swap(root, source, target)
    } else if (!swap(root, source, target)) return false
    tab.root = root
    focusIn(tab, source)
    return true
  }

  /** Removes a pane. Returns [tab closed, space closed]. */
  removePane(paneId: string): [boolean, boolean] {
    const c = this.tabOf(paneId)
    if (!c) return [false, false]
    // A root can't be taken out of itself; a tab whose whole root is this pane is left with none.
    if (c.root.id !== paneId) remove(c.root, paneId)
    if (c.zoomed === paneId) c.zoomed = undefined
    const here = c.root.id === paneId ? [] : panes(c.root).map((n) => n.id)
    if (here.length) {
      c.before = c.before.filter((p) => p !== paneId)
      if (c.focused === paneId) {
        c.before = c.before.filter((p) => here.includes(p))
        c.focused = c.before.pop() ?? here[0]
      }
      return [false, false]
    }
    const [, spaceClosed] = this.removeTab(c.id)
    return [true, spaceClosed]
  }

  /** Removes a tab. Returns its terminal addresses and whether its space closed. */
  removeTab(id: string): [string[], boolean] {
    const pi = this.spaces.findIndex((p) => p.tabs.some((c) => c.id === id))
    if (pi < 0) return [[], false]
    const p = this.spaces[pi]
    const ci = p.tabs.findIndex((c) => c.id === id)
    const [removed] = p.tabs.splice(ci, 1)
    if (p.active === id) p.active = (p.tabs[ci] ?? p.tabs.at(-1))?.id
    const closed = p.tabs.length === 0
    if (closed) this.removeSpace(p.id)
    return [addresses(removed.root), closed]
  }

  /** Removes a space (remembered in `recent`). Returns its terminal addresses. */
  removeSpace(id: string): string[] {
    const pi = this.spaces.findIndex((p) => p.id === id)
    if (pi < 0) return []
    const wasActive = this.activeIndex() === pi
    const [p] = this.spaces.splice(pi, 1)
    this.recent = [{ name: p.name, folder: p.folder }, ...this.recent.filter((r) => r.name !== p.name)].slice(0, 20)
    if (wasActive) this.active = (this.spaces[pi] ?? this.spaces.at(-1))?.id
    return p.tabs.flatMap((c) => addresses(c.root))
  }

  focusedPane(): string | undefined {
    const t = this.activeTab()
    return t && focusedPaneOf(t)
  }

  selectTab(id: string): boolean {
    const p = this.spaces.find((p) => p.tabs.some((c) => c.id === id))
    if (!p) return false
    p.active = id
    this.active = p.id
    this.unsettle(id)
    return true
  }

  settle(id: string, now: number): boolean {
    const c = this.tab(id)
    if (!c) return false
    c.settled = now
    const p = this.spaces.find((p) => p.tabs.some((c) => c.id === id))
    if (p && spaceActiveTab(p)?.id === id) {
      const i = p.tabs.findIndex((c) => c.id === id)
      const n = p.tabs.length
      for (let k = 1; k < n; k++) {
        const next = p.tabs[(i + k) % n]
        if (next.settled === undefined) {
          p.active = next.id
          break
        }
      }
    }
    return true
  }

  unsettle(id: string): boolean {
    const c = this.tab(id)
    if (!c || c.settled === undefined) return false
    c.settled = undefined
    return true
  }

  focusPane(paneId: string): boolean {
    const c = this.tabOf(paneId)
    if (!c) return false
    focusIn(c, paneId)
    return this.selectTab(c.id)
  }

  addressOf(paneId: string): string | undefined {
    for (const c of this.tabs()) {
      const n = find(c.root, paneId)
      if (n) return terminalOf(n)
    }
    return undefined
  }

  paneOf(address: string): string | undefined {
    for (const c of this.tabs()) for (const n of terminalBlocks(c.root)) if (terminalOf(n) === address) return n.id
    return undefined
  }

  allAddresses() {
    return this.tabs().flatMap((c) => addresses(c.root))
  }

  visibleAddresses(): string[] {
    const t = this.activeTab()
    return t ? addresses(shownNode(t)) : []
  }

  /** Applies a status word from core (Herdr's seen rules). Returns whether the word changed. */
  setWord(address: string, word: string | undefined, app: string | undefined, looking: boolean): boolean {
    let a = this.agents.get(address)
    if (!a) {
      a = { seen: true, seq: 0, since: 0 }
      this.agents.set(address, a)
    }
    if (app !== undefined) a.app = app
    if (a.word === word) return false
    const wasBusy = a.word === 'working' || a.word === 'blocked'
    if (word === 'working' || word === 'blocked') a.seen = true
    else if (word !== undefined && wasBusy) a.seen = looking
    a.word = word
    a.seq = ++this.seq
    a.since = this.now()
    return true
  }

  alertFor(addr: string, subAgent: boolean, prev: string | undefined, next: string | undefined, looking: boolean) {
    if (subAgent || this.paneOf(addr) === undefined) return undefined
    return alert(prev, next, looking)
  }

  markSeen(addrs: string[]): boolean {
    let changed = false
    for (const addr of addrs) {
      const a = this.agents.get(addr)
      if (a && !a.seen) {
        a.seen = true
        changed = true
      }
    }
    return changed
  }

  status(address: string): Shown | undefined {
    const a = this.agents.get(address)
    if (!a || a.word === undefined) return undefined
    if (a.word === 'working') return 'working'
    if (a.word === 'blocked' || a.word === 'error') return 'blocked'
    return a.seen ? 'idle' : 'done'
  }

  rollup(addrs: Iterable<string>): Shown | undefined {
    let best: Shown | undefined
    for (const a of addrs) {
      const s = this.status(a)
      if (s && priority(s) > priority(best)) best = s
    }
    return best
  }

  changedAt(c: Tab): number | undefined {
    const t = addresses(c.root).map((a) => this.agents.get(a)?.since ?? 0).filter((t) => t > 0)
    return t.length ? Math.max(...t) : undefined
  }

  /**
   * Puts a live terminal where its labels say without changing what's shown (herdr-app window.rs `adopt`).
   * A space or tab not known yet is made under the label's id, unnamed.
   */
  adopt(addr: string, space: string, tabId: string | undefined, folder: string, fresh: (prefix: string) => string = (p) => this.freshId(p, machineOfAddress(addr))) {
    const machine = machineOfAddress(addr)
    const paneId = fresh('p')
    const id = tabId ?? fresh('tb')
    const tab = newTab(id, '', paneId, addr)
    if (!this.space(space)) {
      const active = this.active
      this.addSpace(space, '', folder, tab, machine)
      this.active = active ?? space
      return
    }
    const existing = this.tab(id)
    const target = existing && panes(existing.root).at(-1)?.id
    if (existing && target) {
      const { focused, before, zoomed } = existing
      this.split(target, 'row', paneId, addr)
      Object.assign(existing, { focused, before: [...before], zoomed })
      return
    }
    const p = this.space(space)!
    const keep = p.active
    this.addTab(space, tab)
    p.active = keep
  }
}

export function spaceActiveTab(p: Space): Tab | undefined {
  return (p.active !== undefined ? p.tabs.find((c) => c.id === p.active) : undefined) ?? p.tabs[0]
}

export function focusIn(t: Tab, paneId: string) {
  const old = t.focused
  if (old !== undefined && old !== paneId) {
    t.before = t.before.filter((p) => p !== old && p !== paneId)
    t.before.push(old)
    if (t.before.length > 32) t.before.shift()
  }
  t.focused = paneId
}

export function focusedPaneOf(t: Tab): string | undefined {
  if (t.focused && find(t.root, t.focused)) return t.focused
  return panes(t.root)[0]?.id
}

/** What's drawn: only the zoomed pane when one is zoomed. */
export function shownNode(t: Tab): Node {
  return (t.zoomed && find(t.root, t.zoomed)) || t.root
}

export { walk }
