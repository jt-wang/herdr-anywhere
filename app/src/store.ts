// The live connection to herdr-core and the world it shows, driven the way herdr-app's window is
// (herdr-app/src/window.rs): every change folds into the model, the model is reconciled with the core's
// terminals, and this machine's layout document is written back with compare-and-set.
import type { FileApi } from './link.ts'
import { HcClient, HcError, type Pipe, type Stream, type StreamHandlers, type Hello } from './hc/client.ts'
import { Model, type FolderInfo, type Space, type Tab, newTab, local, machineOfAddress, spaceActiveTab, shownNode, focusedPaneOf, withPane } from './model/model.ts'
import { captureDoc, replaceDoc, captureView, applyView, type View } from './model/doc.ts'
import { Terminals, cleanTitle, type Term } from './model/terminals.ts'
import { AgentList, type AgentInfo } from './model/agents.ts'
import { programOf, agentArgv, INSTALLED_PROBE, parseInstalled, startable, type Program } from './model/programs.ts'
const AGENT_KEYS = new Set(startable().map((p) => p.key))
import { addresses, find, modBlock, panes, terminalOf, type Axis, type Node } from './model/layout.ts'
import { recentFolders } from './model/folders.ts'
import { activity } from './model/answers.ts'
import { t as tr, type Key } from './strings/index.ts'
import { readMenu, type Menu } from './model/menu.ts'
import { spaceMarks } from './model/marks.ts'

/** The herdr-core label that pins a terminal's agent to the top of the agent lists (Herdr 0.1.12 and later). */
const PIN_LABEL = 'herdr.pinned'

export type Conn = 'idle' | 'connecting' | 'ready' | 'reconnecting' | 'failed' | 'unpaired'
export type Toast = { id: number; title: string; body?: string; level: 'info' | 'error'; action?: { pane?: string; terminal?: string; run?: () => void } }
export type Machine = { name: string; host?: string; state?: string; temporary?: boolean }
export type Slot = { version: number; last: string; available: boolean; loaded: boolean; error?: string; writing: boolean }

export type StoreIO = {
  /** Opens a byte pipe to herdr-core (a WebSocket to the companion, a fake core in tests). */
  pipe: () => Pipe
  setInterval?: typeof setInterval
  clearInterval?: typeof clearInterval
  setTimeout?: typeof setTimeout
  now?: () => number
  /** Keeps this screen's view (focus, zoom, seen) across launches. */
  load?: (key: string) => Promise<string | null>
  save?: (key: string, value: string) => void
}

/** A socket that closes sooner than this after the attempt began, never having opened, was refused. */
const REFUSED_MS = 3000
const ENV = { TERM: 'xterm-256color', COLORTERM: 'truecolor', TERM_PROGRAM: 'herdr' }
const BORN_GRACE_MS = 5000
/** How many left terminals the phone keeps its width on (Store.hold). */
const HELD_MAX = 6
/** The pipe's close reasons (src/hc/ws.ts), as the status line says them. A socket that never opened is told apart by
 *  how soon it closed: a refused connection (the computer is up, nothing listens: the companion stopped) closes at once,
 *  one to a computer that is asleep or away only when the connect times out. */
export const LOST_TEXT: Record<string, Key> = {
  'no-core': 'lostNoCore',
  network: 'lostNetwork',
  refused: 'lostRefused',
  unreachable: 'lostUnreachable',
  tunnel: 'lostTunnel',
  'binary-unsupported': 'lostSlower',
}

/** Tiny observable so views can follow the store without knowing NSH (and tests can run without a screen). */
export class Signal<T> {
  private subs = new Set<(v: T) => void>()
  constructor(private v: T) {}
  get() { return this.v }
  set(v: T) { this.v = v; for (const s of this.subs) s(v) }
  sub(f: (v: T) => void) { this.subs.add(f); return () => this.subs.delete(f) }
}

/** Silence that means the line is dead, though the socket says it is open: the phone asks for the machines every 2.5 s
 *  and herdr-core sends a heartbeat every 15 s (measured 2026-11-05, hc 0.1.18). The socket alone doesn't tell:
 *  a close from the computer can fail to reach the phone, which then goes on saying Connected. */
const QUIET_MS = 15_000
/** A check later than this after the last one: the app was suspended, so nothing could have been heard. */
const ASLEEP_MS = 5_000
/** How long something the person just asked for waits for a lost line to come back before it fails. */
const COMEBACK_MS = 10_000

export class Store {
  readonly model = new Model()
  readonly terms = new Terminals()
  readonly agentList = new AgentList()
  readonly conn = new Signal<Conn>('idle')
  readonly error = new Signal<string>('')
  readonly rev = new Signal(0)
  readonly toasts = new Signal<Toast[]>([])
  readonly machines = new Signal<Machine[]>([])
  /** Composer text typed while the line was down: sent, in order, once it is back. */
  readonly pending = new Signal<{ address: string; text: string }[]>([])
  hello?: Hello
  home = ''
  shell = '/bin/zsh'
  slots = new Map<string, Slot>()
  /** What the person can see: the app is in front and a tab's panes are on screen. */
  appActive = true
  viewingTab = false
  /** Tabs this phone draws whole, every pane on screen; the others show the focused pane. Forgotten when the app restarts. */
  shownWhole = new Set<string>()
  /** An alert, or (`again`) new words for the last one of its terminal, to be put on its notification without a sound. */
  onAlert?: (kind: 'blocked' | 'done', toast: Toast, again?: boolean) => void
  /** Each terminal's last alert while its state stays the one it rang for: Claude Code says what it did a moment after
   *  it reports done (store.test.ts "told again"). */
  private rung = new Map<string, { kind: 'blocked' | 'done'; state: string; body?: string }>()
  /** Which alerts the person wants (Settings › Alerts). */
  wants: (kind: 'blocked' | 'done') => boolean = () => true
  /** Whether agent rows say the program's name (Settings › Look, Show agent names); read where the rows draw. */
  showNames: () => boolean = () => true

  private hc?: HcClient
  private io: Required<Omit<StoreIO, 'load' | 'save'>> & Pick<StoreIO, 'load' | 'save'>
  private loop?: ReturnType<typeof setInterval>
  private machineLoop?: ReturnType<typeof setInterval>
  private quietLoop?: ReturnType<typeof setInterval>
  private heardAt = 0
  private checkedAt = 0
  private born = new Map<string, number>()
  private checking = new Set<string>()
  private adopting = new Set<string>()
  /** The tabs (`space tab`) this screen is starting a terminal in and places it itself: herdr-core reports the terminal
   *  (`created`) before terminal.run answers, and adopting it then put the space or tab in twice (seen on Android). */
  private placing = new Set<string>()
  private firstSync = true
  private savedView?: View
  private toastId = 0
  private wantConnected = false
  private retry = 0
  private gitAt = new Map<string, number>()
  private gitInfo = new Map<string, FolderInfo>() // the last lookup per folder, for panes that open there before the next
  private dirty = false
  installed = new Map<string, string[]>()
  /** Blocked agents' menus read off their screens (model/menu.ts); a key with no menu was read and had none. */
  private menus = new Map<string, Menu | undefined>()

  constructor(io: StoreIO) {
    this.io = {
      setInterval: globalThis.setInterval.bind(globalThis), clearInterval: globalThis.clearInterval.bind(globalThis),
      setTimeout: globalThis.setTimeout.bind(globalThis), now: () => Date.now(), ...io,
    }
    this.model.now = this.io.now
  }

  // ---- connection -------------------------------------------------------------------------------

  async connect(): Promise<void> {
    this.wantConnected = true
    this.attemptAt = this.io.now()
    this.opened = false
    this.conn.set(this.retry ? 'reconnecting' : 'connecting')
    try {
      if (!this.savedView && this.io.load) {
        const v = await this.io.load('view')
        if (v) try { this.savedView = JSON.parse(v) } catch {}
        const changed = await this.io.load('agents.changed')
        if (changed) try { this.agentList.changed = JSON.parse(changed) } catch {}
        const seen = await this.io.load('seen')
        if (seen) try { this.seen = new Map(JSON.parse(seen)) } catch {}
        const sort = await this.io.load('agents.sort')
        if (sort === 'grouped' || sort === 'priority') this.agentList.sort = sort
      }
      this.agentList.onPersist = () => {
        this.io.save?.('agents.changed', JSON.stringify(this.agentList.changed))
      }
      const hc = new HcClient(this.io.pipe())
      this.hc = hc
      hc.onClose = (r) => this.lost(r)
      hc.onHeard = () => { this.heardAt = this.io.now() }
      this.hello = await hc.hello()
      this.opened = true
      const snap = await hc.subscribe((m) => this.event(m))
      this.terms.snapshot(snap.seq, snap.terminals)
      await this.readEnv()
      await this.refreshMachines()
      await Promise.all(this.machineNames().map((m) => this.readDoc(m)))
      this.retry = 0
      this.firstSync = true
      this.sync()
      this.conn.set('ready')
      this.error.set('')
      this.loop = this.io.setInterval(() => this.sync(), 500)
      this.machineLoop = this.io.setInterval(() => void this.refreshMachines(), 2500)
      this.heardAt = this.checkedAt = this.io.now()
      this.quietLoop = this.io.setInterval(() => this.checkQuiet(), 1000)
      void this.refreshInstalled(undefined)
    } catch (e: any) {
      this.hc?.close()
      this.hc = undefined
      // A lost line already says why and tries again.
      if (this.conn.get() !== 'unpaired' && !this.retrying) {
        this.error.set(explain(e))
        this.conn.set('failed')
      }
      throw e
    }
  }

  disconnect() {
    this.wantConnected = false
    this.stopLoops()
    this.hc?.close()
    this.hc = undefined
    this.conn.set('idle')
  }

  private stopLoops() {
    if (this.loop) this.io.clearInterval(this.loop)
    if (this.machineLoop) this.io.clearInterval(this.machineLoop)
    if (this.quietLoop) this.io.clearInterval(this.quietLoop)
    this.loop = this.machineLoop = this.quietLoop = undefined
  }

  private checkQuiet() {
    const now = this.io.now()
    if (now - this.checkedAt > ASLEEP_MS) this.heardAt = now
    this.checkedAt = now
    if (now - this.heardAt >= QUIET_MS) this.hc?.close()
  }

  private attemptAt = 0
  private opened = false
  private retrying = false
  private syncedAt = 0

  /** How old the state on screen is while the line is down. */
  staleFor(): number | undefined {
    return this.conn.get() !== 'ready' && this.syncedAt ? this.io.now() - this.syncedAt : undefined
  }

  private lost(reason: string) {
    this.stopLoops()
    this.hc = undefined
    this.held.clear()
    if (!this.wantConnected) return
    if (reason === 'unpaired') {
      this.wantConnected = false
      this.conn.set('unpaired')
      this.error.set(tr('forgotten'))
      return
    }
    this.conn.set('reconnecting')
    const plain = reason === 'network' || reason === 'closed' || reason === 'retry failed'
    const why = plain && !this.opened ? (this.io.now() - this.attemptAt < REFUSED_MS ? 'refused' : 'unreachable') : reason
    this.error.set(LOST_TEXT[why] ? tr(LOST_TEXT[why]) : plain ? tr('lostNetwork') : tr('lostReason', { reason }))
    if (this.retrying) return
    // Back off like the desktop's 1 s retry, capped so a phone in a tunnel doesn't spin.
    const wait = Math.min(1000 * 2 ** this.retry, 15000)
    this.retry++
    this.retrying = true
    this.io.setTimeout(() => {
      this.retrying = false
      if (this.wantConnected && !this.hc) this.connect().catch(() => { if (this.wantConnected && !this.hc) this.lost('retry failed') })
    }, wait)
  }

  /** Try again now (the network came back, the app came to the front). */
  reconnectNow() {
    if (!this.wantConnected || this.hc) return
    this.retry = 0
    this.connect().catch(() => this.lost('retry failed'))
  }

  get connected() {
    return !!this.hc && this.conn.get() === 'ready'
  }

  /** A phone's line drops for a moment often: what the person asks for then waits for it, and has it retried at once. */
  private whenConnected(): Promise<void> {
    if (this.connected || !this.wantConnected) return Promise.resolve()
    return new Promise<void>((resolve) => {
      let off = () => {}
      const done = () => { off(); resolve() }
      off = this.conn.sub((c) => { if (this.connected || c === 'unpaired' || c === 'idle') done() }) as () => void
      this.io.setTimeout(done, COMEBACK_MS)
      this.reconnectNow()
    })
  }

  call<T = any>(method: string, params: object = {}): Promise<T> {
    if (!this.hc) return Promise.reject(new HcError(tr('disconnected'), 'disconnected'))
    return this.hc.call<T>(method, params)
  }

  private async readEnv() {
    try {
      const r = await this.call('machine.exec', { line: 'printf "%s\\n%s\\n" "$HOME" "$SHELL"', timeout: 10 })
      const [home, shell] = b64text(r.stdout).split('\n')
      this.home = home || ''
      this.shell = shell || '/bin/zsh'
    } catch {}
  }

  async refreshMachines() {
    try {
      const r = await this.call<{ machines: Machine[] }>('machine.list', {})
      const list = (r.machines ?? []).filter((m) => !m.temporary)
      const before = this.machineNames().join(',')
      this.machines.set(list)
      for (const m of list) {
        const slot = this.slots.get(m.name)
        const up = m.state === undefined || m.state === 'connected'
        if (!slot || (up && !slot.available)) void this.readDoc(m.name)
      }
      if (this.machineNames().join(',') !== before) this.bump()
    } catch {}
  }

  machineNames(): (string | undefined)[] {
    return [undefined, ...this.machines.get().map((m) => m.name)]
  }

  // ---- events and documents ---------------------------------------------------------------------

  private event(msg: any) {
    const r = this.terms.apply(msg)
    if (r.rereadDocs) for (const m of this.machineNames()) void this.readDoc(m)
    if (r.doc && r.doc.name === 'herdr-app') {
      const slot = this.slots.get(r.doc.machine ?? '')
      if (!slot || slot.version !== r.doc.version) void this.readDoc(r.doc.machine)
    }
    if (r.changed) this.schedule()
  }

  private scheduled = false
  private schedule() {
    if (this.scheduled) return
    this.scheduled = true
    queueMicrotask(() => {
      this.scheduled = false
      this.sync()
    })
  }

  async readDoc(machine: string | undefined) {
    const key = machine ?? ''
    try {
      const d = await this.call<{ value: unknown; version: number }>('doc.get', machine ? { name: 'herdr-app', on: machine } : { name: 'herdr-app' })
      const slot = this.slots.get(key)
      if (slot && slot.writing) return
      if (slot && slot.loaded && slot.version === d.version) return
      replaceDoc(d.value, this.model, machine)
      if (!slot?.loaded && this.savedView) applyView(this.savedView, this.model)
      this.slots.set(key, { version: d.version, last: JSON.stringify(captureDoc(this.model, machine)), available: true, loaded: true, writing: false })
      this.bump()
    } catch (e: any) {
      const slot = this.slots.get(key)
      this.slots.set(key, { version: slot?.version ?? 0, last: slot?.last ?? '', available: false, loaded: true, writing: false, error: explain(e) })
      this.bump()
    }
  }

  /** Writes each machine's document that this screen changed. On a conflict the other screen's layout wins. */
  private async flush() {
    for (const machine of this.machineNames()) {
      const key = machine ?? ''
      const slot = this.slots.get(key)
      if (!slot || !slot.available || slot.writing) continue
      const doc = captureDoc(this.model, machine)
      const text = JSON.stringify(doc)
      if (text === slot.last) continue
      slot.writing = true
      try {
        const r = await this.call<{ version: number }>('doc.set', { name: 'herdr-app', ...(machine ? { on: machine } : {}), version: slot.version, value: doc })
        slot.version = r.version
        slot.last = text
      } catch (e: any) {
        if (e instanceof HcError && e.kind === 'conflict') {
          slot.writing = false
          slot.loaded = false
          await this.readDoc(machine)
          this.toast({ title: tr('layoutChanged'), body: tr('layoutChangedBody'), level: 'info' })
          continue
        }
      } finally {
        slot.writing = false
      }
    }
  }

  private requireDoc(machine: string | undefined) {
    const slot = this.slots.get(machine ?? '')
    if (slot?.available) return
    throw new HcError(slot?.error ?? (machine ? tr('noMachine', { machine }) : tr('layoutUnavailable')))
  }

  // ---- the 500 ms loop: status, seen, alerts, reconcile, flush -----------------------------------

  sync() {
    if (!this.hc) return
    this.syncedAt = this.io.now()
    const live = new Map(this.terms.list().filter((t) => t.state === 'live').map((t) => [t.address, t]))
    const visible = new Set(this.model.visibleAddresses())
    const looking = (a: string) => this.appActive && this.viewingTab && visible.has(a)
    let changed = this.dirty
    this.dirty = false
    for (const t of this.terms.list()) {
      const word = t.reported && t.status?.state ? t.status.state : undefined
      const prev = this.model.agents.get(t.address)?.word
      if (!this.model.setWord(t.address, word, t.status?.app ?? undefined, looking(t.address))) { this.ringAgain(t); continue }
      this.rung.delete(t.address)
      changed = true
      if (this.firstSync) continue
      const sub = !!t.parent && this.model.paneOf(t.address) === undefined
      const kind = this.model.alertFor(t.address, sub, prev, word, looking(t.address))
      if (kind) this.ring(kind, t)
    }
    if (this.appActive && this.viewingTab && this.model.markSeen([...visible])) changed = true
    for (const t of this.terms.list()) {
      const blocked = t.state === 'live' && t.reported && t.status?.state === 'blocked'
      if (blocked && !this.menus.has(t.address)) { this.menus.set(t.address, undefined); void this.readChoices(t.address, 3) }
      else if (!blocked && this.menus.delete(t.address)) { this.menuTries.delete(t.address); changed = true }
    }
    for (const t of this.terms.list()) {
      const cwd = t.foreground?.cwd ?? t.cwd
      const p = this.model.paneOf(t.address)
      if (p && cwd && this.model.paneFolders.get(p) !== cwd) {
        this.model.paneFolders.set(p, cwd)
        changed = true
      }
    }
    this.firstSync = false
    void this.flushPending()
    if (this.reconcile(live)) changed = true
    void this.flush()
    void this.refreshGit()
    if (changed) this.bump()
  }

  private ring(kind: 'blocked' | 'done', t: Term) {
    const toast = this.alertToast(kind, t)
    const pane = this.model.paneOf(t.address)
    const tab = pane ? this.model.tabOf(pane) : undefined
    if (tab?.settled !== undefined) this.model.unsettle(tab.id)
    if (!this.wants(kind)) return
    if (t.status?.state) this.rung.set(t.address, { kind, state: t.status.state, body: toast.body })
    if (!(this.appActive && this.viewingTab && this.model.visibleAddresses().includes(t.address))) this.toast(toast)
    this.onAlert?.(kind, toast)
  }

  /** Same state, new words: the last alert of this terminal says them. */
  private ringAgain(t: Term) {
    const r = this.rung.get(t.address)
    if (!r || t.status?.state !== r.state) return
    const toast = this.alertToast(r.kind, t)
    if (toast.body === r.body) return
    r.body = toast.body
    this.onAlert?.(r.kind, toast, true)
  }

  private alertToast(kind: 'blocked' | 'done', t: Term): Toast {
    const pane = this.model.paneOf(t.address)
    const space = pane ? this.model.spaceOf(pane) : undefined
    const tab = pane ? this.model.tabOf(pane) : undefined
    const name = this.terminalName(t)
    // What it asks or did, in its own words, then where it is: a notification's first line is what shows when folded.
    const place = space && tab ? `${this.spaceName(space)} › ${Model.tabName(space, tab)}` : undefined
    // Any program can report its state; only an agent's title is its summary of the task (a shell's is user@host:path).
    const p = this.program(t.address)
    const said = activity(t.status, p.agent ? cleanTitle(t.title) : '', p.name)
    const body = [said, place].filter(Boolean).join('\n') || undefined
    return kind === 'blocked'
      ? { id: 0, title: tr('nameNeedsYou', { name }), body, level: 'error', action: { terminal: t.address } }
      : { id: 0, title: tr('nameFinished', { name }), body, level: 'info', action: { terminal: t.address } }
  }

  private reconcile(live: Map<string, Term>): boolean {
    const now = this.io.now()
    let changed = false
    // Panes whose terminal is gone: ask whether it ended (it may be on its way back after a core restart).
    for (const c of this.model.tabs()) for (const n of panes(c.root)) {
      const a = terminalOf(n)
      if (!a || live.has(a) || this.checking.has(a) || now - (this.born.get(a) ?? 0) < BORN_GRACE_MS) continue
      const machine = machineOfAddress(a)
      if (!this.slots.get(machine ?? '')?.available) continue
      this.checking.add(a)
      this.call('terminal.get', { terminal: a }).then(
        (r) => r.state !== 'live' && (r.reason === 'exited' || r.reason === 'killed'),
        (e) => e instanceof HcError && e.kind === 'not_found',
      ).then((closed) => {
        this.checking.delete(a)
        const pane = this.model.paneOf(a)
        if (!closed || !pane) return
        this.model.removePane(pane)
        // Only a change earns another pass now; otherwise the next loop asks again (no change → no loop).
        if (this.model.paneOf(a) === pane) return
        this.dirty = true
        this.schedule()
      })
    }
    // Live terminals labeled for a space that no pane shows: place them (started by an agent, a script, another screen).
    const orphans = [...live.values()].filter((t) => this.model.paneOf(t.address) === undefined && !this.adopting.has(t.address))
      .map((t) => {
        const machine = machineOfAddress(t.address)
        const space = t.labels?.['herdr.space']
        if (!space || !this.slots.get(machine ?? '')?.available) return undefined
        const tab = t.labels?.['herdr.tab'] || undefined
        return { t, machine, space: machine ? `${machine}/${space}` : space, tab: tab && machine ? `${machine}/${tab}` : tab }
      }).filter((o): o is NonNullable<typeof o> => !!o)
      .sort((a, b) => (a.t.address < b.t.address ? -1 : 1))
    for (const o of orphans) {
      this.adopting.add(o.t.address)
      void (async () => {
        try {
          const rec = await this.call<Term>('terminal.get', { terminal: o.t.address })
          const labels = rec.labels ?? {}
          if (rec.state !== 'live' || labels['herdr.space'] !== local(o.space, o.machine)) return
          if (rec.keep !== true) await this.call('terminal.keep', { terminal: o.t.address, keep: true })
          if (this.model.paneOf(o.t.address) !== undefined || this.placing.has(`${o.space} ${o.tab}`)) return
          this.model.adopt(o.t.address, o.space, o.tab, rec.foreground?.cwd ?? rec.cwd ?? this.home)
          if (this.model.paneOf(o.t.address) === undefined) return
          this.dirty = true
          this.schedule()
        } catch {} finally {
          this.adopting.delete(o.t.address)
        }
      })()
    }
    return changed
  }

  private bump() {
    this.io.save?.('view', JSON.stringify(captureView(this.model)))
    this.rev.set(this.rev.get() + 1)
  }

  toast(t: Omit<Toast, 'id'>) {
    const toast = { ...t, id: ++this.toastId }
    this.toasts.set([...this.toasts.get(), toast].slice(-4))
    this.io.setTimeout(() => this.dismiss(toast.id), 5000)
  }

  dismiss(id: number) {
    this.toasts.set(this.toasts.get().filter((t) => t.id !== id))
  }

  // ---- what the screens show ---------------------------------------------------------------------

  term(addr: string | undefined) {
    return addr ? this.terms.get(addr) : undefined
  }

  program(addr: string | undefined): Program {
    const t = this.term(addr)
    return programOf(t?.status?.app ?? null, t?.foreground?.argv ?? t?.argv ?? [])
  }

  terminalName(t: Term | undefined): string {
    if (!t) return tr('terminal')
    const p = this.program(t.address)
    return t.name || (p.shell ? cleanTitle(t.title) || p.name : p.name)
  }

  /** What an agent is doing or did, in its own words (model/answers.ts). */
  activityOf(addr: string | undefined): string {
    const t = this.term(addr)
    if (!t) return ''
    const p = this.program(t.address)
    return p.agent ? activity(t.status, cleanTitle(t.title), p.name) : ''
  }

  /** A pane header's name (herdr-app defaults/mods/pane): a shell or an agent by its program, anything else by its title. */
  paneName(addr: string | undefined): string {
    const t = this.term(addr)
    if (!t) return tr('terminal')
    const p = this.program(t.address)
    return (p.agent || p.shell ? p.name : cleanTitle(t.title)) || p.name
  }

  spaceName(s: Space) {
    return this.model.spaceName(s, this.home)
  }

  /** Each space's badge colour, by space id (model/marks.ts). */
  spaceMarks(): Record<string, string> {
    return spaceMarks(this.model.spaces.map((s) => ({ id: s.id, name: this.spaceName(s) })))
  }

  /** A tab's caption: its name; an automatic one shows its program, as the tab bar does. */
  tabLabel(s: Space, c: Tab): string {
    if (c.name) return c.name
    const lead = this.leadPane(c)
    const a = lead && terminalOf(lead)
    if (!a) return Model.tabName(s, c)
    const p = this.program(a)
    return p.shell ? tr('terminal') : p.name
  }

  leadPane(c: Tab): Node | undefined {
    const all = panes(c.root).filter((n) => terminalOf(n))
    return all.find((n) => this.program(terminalOf(n)).agent && this.term(terminalOf(n))?.reported) ?? all.find((n) => n.id === c.focused) ?? all[0]
  }

  statusOf(addr: string | undefined) {
    return addr ? this.model.status(addr) : undefined
  }

  tabStatus(c: Tab) {
    return this.model.rollup(addresses(c.root))
  }

  spaceStatus(s: Space) {
    return this.model.rollup(s.tabs.flatMap((c) => addresses(c.root)))
  }

  /** Terminals whose program reports a status, in space, tab, pane order (herdr's `agents()`), and the agents that never
   *  report one (OpenCode before 2.2 prints no OSC 7501: its packages/tui/src/context/program-status.ts came in
   *  with anomalyco/opencode#55172, is in the v2.2.0 release and not in v1.19.5; read 2026-11-06), known by what they run. */
  agents(): AgentInfo[] {
    const out: AgentInfo[] = []
    const focusedPane = this.viewingTab ? this.model.focusedPane() : undefined
    for (const s of this.model.spaces) for (const [ti, c] of s.tabs.entries()) for (const n of panes(c.root)) {
      const a = terminalOf(n)
      const t = a ? this.terms.get(a) : undefined
      const st = a ? this.model.status(a) : undefined
      if (!a || !t || t.state !== 'live') continue
      const p = this.program(a)
      if (!st && !AGENT_KEYS.has(p.key)) continue
      out.push({
        address: a, pane: n.id, space: s.id, space_name: this.spaceName(s), tab: c.id, tab_name: c.name || String(ti + 1), named: !!c.name,
        status: st, app: t.status?.app ?? null, title: cleanTitle(t.title), seq: this.model.agents.get(a)?.seq ?? 0,
        focused: n.id === focusedPane, pinned: !!t.labels?.[PIN_LABEL], programName: p.name, programKey: p.key,
        kind: t.status?.state === 'blocked' ? t.status?.kind ?? null : null, msg: t.status?.state === 'blocked' ? t.status?.msg ?? '' : '',
      })
    }
    return out
  }

  /** Pins or unpins an agent on the computer, as the desktop's `terminal.pin` does: the label shows on every screen
   *  when herdr-core's `labels` event comes back. An empty value removes the label (herdr-core 0.1.18, 2026-11-07).
   *  Herdr 0.1.13 writes the same values, "1" or "" (2026-11-07). */
  async pin(addr: string, pinned: boolean) {
    await this.call('terminal.label', { terminal: addr, labels: { [PIN_LABEL]: pinned ? '1' : '' } })
  }

  agentsView() {
    return this.agentList.view(this.agents(), this.terms.list(), this.io.now())
  }

  /** Every agent that waits on you, across the spaces, the longest wait first. */
  queue(): AgentInfo[] {
    this.agentsView()
    return this.agents().filter((a) => a.status === 'blocked').sort((x, y) => this.agentList.since(x) - this.agentList.since(y))
  }

  /** "space › tab", as the pane's header says it. */
  place(a: AgentInfo): string {
    const sp = this.model.space(a.space)
    const tab = sp?.tabs.find((c) => c.id === a.tab)
    return sp && tab ? `${this.spaceName(sp)} › ${this.tabLabel(sp, tab)}` : a.space_name
  }

  private seen = new Map<string, { at: number; tail: string[] }>()
  /** When you last looked at a pane and the last lines you saw there (model/seen.ts), kept on the phone. */
  lastSeen(addr: string) { return this.seen.get(addr) }
  saw(addr: string, tail: string[], at = this.io.now()) {
    this.seen.delete(addr)
    this.seen.set(addr, { at, tail })
    while (this.seen.size > 200) this.seen.delete(this.seen.keys().next().value!)
    this.io.save?.('seen', JSON.stringify([...this.seen]))
  }

  private composeFocus: string | undefined
  /** `addr`'s composer takes the focus when its pane shows it (a choice on home that wants text), once. */
  focusComposerOn(addr: string | undefined) { this.composeFocus = addr }
  wantsComposerFocus(addr: string): boolean { return this.composeFocus === addr }

  /** Answers a blocked agent's prompt (its keys off its screen, model/menu.ts): presses them, then types `text` and Enter for a reply.
   * herdr-core sends both once nobody has typed for a moment; `force` lets the text reach a blocked program. */
  async answer(addr: string, a: { keys: string[] }, text?: string) {
    await this.call('terminal.keys', { terminal: addr, keys: a.keys })
    if (text !== undefined) await this.call('terminal.send', { terminal: addr, text, force: true })
  }

  /** The blocked agent's choices as its screen shows them, when its layout is known (model/menu.ts). */
  menuFor(addr: string | undefined): Menu | undefined {
    return addr ? this.menus.get(addr) : undefined
  }

  /** Whether the blocked agent's screen was read (a menu or not), so the phone can say the keys are the way. */
  menuRead(addr: string | undefined): boolean {
    return !!addr && this.menuTries.get(addr) === 0
  }

  private menuTries = new Map<string, number>()

  /** Reads the screen for the menu; Claude Code may report blocked a moment before it draws it, so a screen
   *  without one is read again 700 ms later, `tries` times in all. */
  private async readChoices(addr: string, tries: number) {
    this.menuTries.set(addr, tries)
    let menu: Menu | undefined
    try {
      const r = await this.call<{ text: string }>('terminal.read', { terminal: addr, lines: 0 })
      menu = readMenu(this.terms.get(addr)?.status?.app, r.text ?? '')
    } catch {}
    if (!this.menus.has(addr)) return
    if (menu) { this.menus.set(addr, menu); this.menuTries.set(addr, 0); this.bump(); return }
    if (tries > 1) this.io.setTimeout(() => { if (this.menus.has(addr) && !this.menus.get(addr)) void this.readChoices(addr, tries - 1) }, 700)
    else { this.menuTries.set(addr, 0); this.bump() }
  }

  /** The phone's composer: the text, pasted into the pane and submitted with Enter (herdr-core's terminal.send, with
   *  `force` so it also reaches an agent that asked for text). Typed while the line is down, it waits as pending. */
  async compose(addr: string, text: string) {
    if (this.conn.get() !== 'ready' || !this.hc) { this.pending.set([...this.pending.get(), { address: addr, text }]); return }
    await this.call('terminal.send', { terminal: addr, text, force: true })
  }

  /** Sends what the composer attaches to the computer (link.ts); the screens that start the store set it. */
  files?: FileApi

  private flushing = false
  private async flushPending() {
    if (this.flushing || !this.pending.get().length || this.conn.get() !== 'ready') return
    this.flushing = true
    try {
      while (this.pending.get().length && this.conn.get() === 'ready') {
        // Off the queue first: when the line goes before the answer comes, the computer may have it, and sending it
        // again would run it twice.
        const first = this.pending.get()[0]
        this.pending.set(this.pending.get().slice(1))
        await this.call('terminal.send', { terminal: first.address, text: first.text, force: true })
          .catch(() => this.toast({ title: tr('sendUnsure', { text: first.text }), level: 'error' }))
      }
    } finally { this.flushing = false }
  }

  counts() {
    const list = this.agents()
    return { agents: list.length, needsYou: list.filter((a) => a.status === 'blocked').length }
  }

  /** The folders this computer's spaces and terminals use, most recently used first: the folder picker's Recent. */
  recentFolders(): string[] {
    const used: { folder: string; at: number }[] = []
    for (const t of this.terms.list()) {
      if (t.address.includes('/')) continue
      used.push({ folder: t.foreground?.cwd ?? t.cwd ?? '', at: Math.max(Number(t.status_ms) || 0, Number(t.created_ms) || 0) })
    }
    for (const sp of this.model.spaces) if (!sp.machine && sp.folder) used.push({ folder: sp.folder, at: 0 })
    return recentFolders(used)
  }

  /** Where new terminals of a space start: the shown pane's live folder when it's in that space, else the space's. */
  folderFor(spaceId: string): string {
    const p = this.model.space(spaceId)
    const tab = p && spaceActiveTab(p)
    const pane = tab && focusedPaneOf(tab)
    const a = pane ? this.model.addressOf(pane) : undefined
    const t = a ? this.terms.get(a) : undefined
    return t?.foreground?.cwd ?? t?.cwd ?? p?.folder ?? this.home
  }

  // ---- commands (herdr-app src/commands.rs) ------------------------------------------------------

  /** Starts a terminal labeled for `space`/`tab` and lets `place` put it in the layout, adopting nothing there meanwhile. */
  private async spawnInto(argv: string[] | undefined, cwd: string, space: string, tab: string, on: string | undefined, place: (addr: string) => void): Promise<string> {
    const key = `${space} ${tab}`
    this.placing.add(key)
    try {
      const addr = await this.spawn(argv, cwd, space, tab, on)
      place(addr)
      return addr
    } finally {
      this.placing.delete(key)
    }
  }

  private async spawn(argv: string[] | undefined, cwd: string, space: string, tab: string, on: string | undefined): Promise<string> {
    await this.whenConnected()
    this.requireDoc(on)
    const r = await this.call<{ address: string }>('terminal.run', {
      argv: argv ?? [], ...(cwd ? { cwd } : {}), env: ENV, keep: true,
      labels: { 'herdr.space': local(space, on), 'herdr.tab': local(tab, on) }, ...(on ? { on } : {}),
      ...(this.display ? { display: this.display } : {}),
    })
    const addr = on && !r.address.includes('/') ? `${on}/${r.address}` : r.address
    this.born.set(addr, this.io.now())
    return addr
  }

  private agentCommand(argv: string[] | undefined, task: string | undefined, on: string | undefined) {
    return agentArgv(argv, task, on, this.shell) ?? argv
  }

  async newSpace(o: { name?: string; folder?: string; tab?: string; argv?: string[]; task?: string; machine?: string } = {}) {
    const on = o.machine || undefined
    const folder = o.folder ?? (on ? '' : this.home)
    const [space, tab, pane] = [this.model.freshId('sp', on), this.model.freshId('tb', on), this.model.freshId('p', on)]
    const addr = await this.spawnInto(this.agentCommand(o.argv, o.task, on), folder, space, tab, on,
      (a) => this.model.addSpace(space, (o.name ?? '').trim(), folder, newTab(tab, (o.tab ?? '').trim(), pane, a), on))
    this.changed()
    return { space, tab, terminal: addr, pane }
  }

  async newTab(o: { space?: string; name?: string; folder?: string; argv?: string[]; task?: string } = {}) {
    const space = o.space ?? this.model.activeSpace()?.id
    if (!space) return this.newSpace({ name: '', argv: o.argv, task: o.task })
    const p = this.model.space(space)
    if (!p) throw new HcError(tr('noSpace', { id: space }))
    const on = p.machine
    const [tab, pane] = [this.model.freshId('tb', on), this.model.freshId('p', on)]
    const addr = await this.spawnInto(this.agentCommand(o.argv, o.task, on), o.folder ?? this.folderFor(space), space, tab, on,
      (a) => this.model.addTab(space, newTab(tab, (o.name ?? '').trim(), pane, a)))
    this.model.active = space
    this.changed()
    return { space, tab, terminal: addr, pane }
  }

  async newModTab(mod: string, props: unknown, name = '') {
    const space = this.model.activeSpace()
    if (!space) throw new HcError(tr('noSpaceYet'))
    this.requireDoc(space.machine)
    const [tab, pane] = [this.model.freshId('tb', space.machine), this.model.freshId('p', space.machine)]
    this.model.addTab(space.id, withPane(tab, name, modBlock(pane, mod, props)))
    this.changed()
    return { tab, pane }
  }

  async splitPane(paneId: string | undefined, direction: 'right' | 'down', argv?: string[]) {
    const target = paneId ?? this.model.focusedPane()
    if (!target) throw new HcError(tr('noPaneToSplit'))
    const p = this.model.spaceOf(target)
    const c = this.model.tabOf(target)
    if (!p || !c) throw new HcError(tr('noPane', { id: target }))
    const targetAddr = this.model.addressOf(target)
    const t = targetAddr ? this.terms.get(targetAddr) : undefined
    const cwd = t?.foreground?.cwd ?? t?.cwd ?? p.folder
    const on = (targetAddr && machineOfAddress(targetAddr)) ?? p.machine
    const fresh = this.model.freshId('p', on)
    const addr = await this.spawnInto(argv, cwd, p.id, c.id, on, (a) => this.model.split(target, direction === 'right' ? 'row' : 'column' as Axis, fresh, a))
    this.changed()
    return { pane: fresh, terminal: addr }
  }

  showWhole(tab: string, on: boolean) {
    if (on) this.shownWhole.add(tab)
    else this.shownWhole.delete(tab)
    this.changed()
  }

  /** Closes a pane and ends its terminal; the last pane takes its tab (and the last tab its space). */
  closePane(paneId: string) {
    const c = this.model.tabOf(paneId)
    if (!c) return
    const n = find(c.root, paneId)
    if (n?.closable === false) return
    const s = this.model.spaceOf(paneId)!
    this.requireDoc(s.machine)
    if (panes(c.root).length === 1) return this.closeTab(c.id)
    const addr = this.model.addressOf(paneId)
    this.model.removePane(paneId)
    this.kill(addr ? [addr] : [])
    this.changed()
  }

  closeTab(id: string) {
    const s = this.model.spaceOf(id)
    if (!s) return
    this.requireDoc(s.machine)
    if (s.tabs.length === 1) return this.closeSpace(s.id)
    const [addrs] = this.model.removeTab(id)
    this.kill(addrs)
    this.changed()
  }

  closeSpace(id: string) {
    const s = this.model.space(id)
    if (!s) return
    this.requireDoc(s.machine)
    this.kill(this.model.removeSpace(id))
    this.changed()
  }

  /** Ends terminals and their sub-agents that no space keeps (herdr-app window.rs `kill`). */
  kill(addrs: string[]) {
    const doomed = [...addrs]
    for (let i = 0; i < doomed.length; i++) {
      for (const t of this.terms.list()) {
        const kept = !!t.labels?.['herdr.space']
        if (t.parent === doomed[i] && !kept && !doomed.includes(t.address)) doomed.push(t.address)
      }
    }
    for (const a of doomed) void this.call('terminal.kill', { terminal: a }).catch(() => {})
  }

  renameSpace(id: string, name: string) {
    const s = this.model.space(id)
    if (!s) return
    this.requireDoc(s.machine)
    s.name = name.trim()
    this.changed()
  }

  renameTab(id: string, name: string) {
    const c = this.model.tab(id)
    if (!c) return
    this.requireDoc(this.model.spaceOf(id)?.machine)
    c.name = name.trim()
    this.changed()
  }

  moveSpace(id: string, index: number) {
    if (this.model.moveSpace(id, index)) this.changed()
  }

  moveTab(id: string, index: number) {
    if (this.model.moveTab(id, index)) this.changed()
  }

  selectSpace(id: string) {
    if (!this.model.space(id)) return
    this.model.active = id
    this.changed()
  }

  selectTab(id: string) {
    if (this.model.selectTab(id)) this.changed()
  }

  /** The tab beside the space's selected one; none past either end. */
  stepTab(spaceId: string, dir: 'next' | 'prev') {
    const sp = this.model.space(spaceId)
    const at = sp ? sp.tabs.findIndex((c) => c.id === spaceActiveTab(sp)?.id) : -1
    const to = sp?.tabs[at + (dir === 'next' ? 1 : -1)]
    if (at >= 0 && to) this.selectTab(to.id)
  }

  focusPane(paneId: string) {
    if (this.model.focusPane(paneId)) this.changed()
  }

  focusTerminal(addr: string): boolean {
    const p = this.model.paneOf(addr)
    if (!p) return false
    this.focusPane(p)
    return true
  }

  zoom(paneId?: string) {
    const pane = paneId ?? this.model.focusedPane()
    const c = pane ? this.model.tabOf(pane) : undefined
    if (!c || !pane || panes(c.root).length < 2) return
    c.zoomed = c.zoomed === pane ? undefined : pane
    this.changed()
  }

  swapPane(paneId: string, direction: 'left' | 'right' | 'up' | 'down') {
    const target = this.neighbor(paneId, direction)
    if (target && this.model.movePane(paneId, target)) this.changed()
  }

  /** The nearest pane in a direction, by the tab's split tree. */
  neighbor(paneId: string, direction: 'left' | 'right' | 'up' | 'down'): string | undefined {
    const c = this.model.tabOf(paneId)
    if (!c) return undefined
    const rects = layoutRects(shownNode(c), 0, 0, 1, 1)
    const me = rects.get(paneId)
    if (!me) return undefined
    const cx = me.x + me.w / 2, cy = me.y + me.h / 2
    let best: string | undefined, bestD = Infinity
    for (const [id, r] of rects) {
      if (id === paneId) continue
      const ok = direction === 'left' ? r.x + r.w <= me.x + 1e-6 : direction === 'right' ? r.x >= me.x + me.w - 1e-6
        : direction === 'up' ? r.y + r.h <= me.y + 1e-6 : r.y >= me.y + me.h - 1e-6
      if (!ok) continue
      const d = Math.hypot(r.x + r.w / 2 - cx, r.y + r.h / 2 - cy)
      if (d < bestD) { bestD = d; best = id }
    }
    return best
  }

  settleTab(id: string) {
    if (this.model.settle(id, this.io.now())) this.changed()
  }

  unsettleTab(id: string) {
    if (this.model.unsettle(id)) this.changed()
  }

  /** A sub-agent (no pane) becomes a tab of its own in its parent's space. */
  async keepTerminal(addr: string) {
    const t = this.terms.get(addr)
    if (!t) return
    const parentPane = t.parent ? this.model.paneOf(t.parent) : undefined
    const space = (parentPane && this.model.spaceOf(parentPane)) || this.model.activeSpace()
    if (!space) throw new HcError(tr('noSpaceToKeep'))
    const tab = this.model.freshId('tb', space.machine)
    await this.call('terminal.label', { terminal: addr, labels: { 'herdr.space': local(space.id, space.machine), 'herdr.tab': local(tab, space.machine) } })
    await this.call('terminal.keep', { terminal: addr, keep: true })
    this.model.addTab(space.id, newTab(tab, '', this.model.freshId('p', space.machine), addr))
    this.model.active = space.id
    this.changed()
  }

  /** The phone's terminal colours (`#rrggbb`), for programs it starts: herdr-core answers their colour queries with them. */
  display?: { fg: string; bg: string; cursor: string }

  /** Attachments to terminals the phone left, newest last. herdr-core sizes a terminal to its last used viewer and,
   *  when that one detaches, to the last used of those left: leaving would hand the width to an idle split on the
   *  computer, and what an agent prints meanwhile stays that narrow in its history (measured 2026-11-06 on the emulator). Kept, a
   *  left terminal stays the phone's width until someone uses another viewer. */
  private held = new Map<string, Stream>()

  /** The phone left this terminal: keep its attachment (its output unread) for the last HELD_MAX terminals. */
  hold(addr: string, s: Stream) {
    this.release(addr)
    this.held.set(addr, s)
    for (const [a, old] of this.held) {
      if (this.held.size <= HELD_MAX) break
      this.held.delete(a)
      old.detach()
    }
  }

  /** Let go of a left terminal's attachment: the phone attached to it again, or it ended. */
  release(addr: string) {
    const s = this.held.get(addr)
    if (!s) return
    this.held.delete(addr)
    s.detach()
  }

  async attach(addr: string, size: { cols: number; rows: number }, on: StreamHandlers, opts: { observe?: boolean; resumed?: boolean; display?: any } = {}): Promise<Stream> {
    if (!this.hc) throw new HcError(tr('disconnected'), 'disconnected')
    return this.hc.attach({ terminal: addr, cols: size.cols, rows: size.rows, history: 2000, ...opts }, on)
  }

  async exec(line: string, o: { on?: string; cwd?: string; timeout?: number } = {}) {
    const r = await this.call('machine.exec', { line, ...(o.on ? { on: o.on } : {}), ...(o.cwd ? { cwd: o.cwd } : {}), timeout: o.timeout ?? 30 })
    return { code: r.code as number | null, stdout: b64text(r.stdout), stderr: b64text(r.stderr) }
  }

  async refreshInstalled(on: string | undefined) {
    const commands = startable().map((p) => p.run![0])
    try {
      const words = commands.map((c) => `'${c.replace(/'/g, '')}'`).join(' ')
      const r = await this.exec(`${this.shell} -ic ${shQuote(`sh -c '${INSTALLED_PROBE.replace(/'/g, '')}' sh ${words}`)}`, { on, timeout: 15 })
      this.installed.set(on ?? '', parseInstalled(r.stdout, commands))
      this.bump()
    } catch {}
  }

  private gitKey(t: Term) { return JSON.stringify([machineOfAddress(t.address) ?? null, t.foreground?.cwd ?? t.cwd]) }

  /** Branch, ahead/behind and repo top for each pane's folder (herdr's spaces mod), at most once a minute per folder. */
  private async refreshGit() {
    const now = this.io.now()
    const todo = new Map<string, { on?: string; cwd: string }>()
    for (const t of this.terms.list()) {
      if (t.state !== 'live' || !this.model.paneOf(t.address)) continue
      const cwd = t.foreground?.cwd ?? t.cwd
      const on = machineOfAddress(t.address)
      const key = this.gitKey(t)
      if (!cwd) continue
      if (now - (this.gitAt.get(key) ?? -Infinity) < 60_000) {
        const known = this.gitInfo.get(key)
        if (known && this.model.folderInfo.get(t.address) !== known) { this.model.folderInfo.set(t.address, known); this.bump() }
        continue
      }
      todo.set(key, { on, cwd })
    }
    for (const [key, e] of todo) {
      this.gitAt.set(key, now)
      try {
        const r = await this.exec('git --no-optional-locks status --porcelain=v2 --branch && echo @@herdr && git rev-parse --show-toplevel', { on: e.on, cwd: e.cwd, timeout: 10 })
        if (r.code !== 0) continue
        const [status, top] = r.stdout.split('@@herdr\n')
        let branch: string | undefined, ahead = 0, behind = 0
        for (const line of status.split('\n')) {
          if (line.startsWith('# branch.head ')) branch = line.slice(14)
          else if (line.startsWith('# branch.ab ')) {
            const [a, b] = line.slice(12).split(' ')
            ahead = Math.abs(parseInt(a, 10)) || 0
            behind = Math.abs(parseInt(b, 10)) || 0
          }
        }
        const info = { top: (top ?? '').trim() || e.cwd, branch, ahead, behind }
        this.gitInfo.set(key, info)
        // Every pane in the folder now, also one that opened while git ran.
        for (const t of this.terms.list()) if (this.model.paneOf(t.address) && this.gitKey(t) === key) this.model.folderInfo.set(t.address, info)
        this.bump()
      } catch {}
    }
  }

  private changed() {
    this.sync()
    this.bump()
  }
}

/** Fractions of the tab each pane takes, from the split tree's weights. */
export function layoutRects(n: Node, x: number, y: number, w: number, h: number, out = new Map<string, { x: number; y: number; w: number; h: number }>()) {
  if (n.type === 'block') {
    out.set(n.id, { x, y, w, h })
    return out
  }
  const kids = (n.children ?? []).filter((c) => !c.hidden)
  if (n.type === 'stack') {
    const a = kids.find((c) => c.id === n.active) ?? kids[0]
    if (a) layoutRects(a, x, y, w, h, out)
    return out
  }
  const weights = kids.map((c) => (c.size && 'weight' in c.size ? c.size.weight : 1))
  const total = weights.reduce((s, v) => s + v, 0) || 1
  let at = 0
  kids.forEach((c, i) => {
    const f = weights[i] / total
    if (n.type === 'row') layoutRects(c, x + at * w, y, f * w, h, out)
    else layoutRects(c, x, y + at * h, w, f * h, out)
    at += f
  })
  return out
}

export function b64text(s: string | undefined): string {
  if (!s) return ''
  const bin = atob(s)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  return new TextDecoder().decode(bytes)
}

export function shQuote(s: string) {
  return `'${s.replace(/'/g, `'\\''`)}'`
}

export function explain(e: any): string {
  if (e instanceof HcError) return e.hint ? `${e.message} (${e.hint})` : e.message
  return String(e?.message ?? e)
}
