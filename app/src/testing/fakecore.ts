// An in-memory herdr-core speaking the framed protocol, for headless tests of the store and the screens.
// It keeps terminals, the `herdr-app` documents with compare-and-set versions, and the event stream.
import { CLOSE, CREDIT, DATA, FOCUS, FrameParser, RESIZE, encodeFrame } from '../hc/frames.ts'
import type { Pipe } from '../hc/client.ts'

const enc = new TextEncoder()
const dec = new TextDecoder()

export type FakeTerm = Record<string, any> & { address: string }
export type FakeTab = { id: string; name?: string; argv: string[]; status?: string; title?: string; kind?: string; msg?: string; screen?: string; foreground?: boolean }

/** Claude Code's permission menu as it draws it (2.1.314), for the choices the phone reads off the screen. */
export const CLAUDE_EDIT_MENU = [' Edit file', ' src/api/login.ts', '', ' Do you want to make this edit to login.ts?', ' ❯ 1. Yes',
  '   2. Yes, allow all edits during this session (shift+tab)', '   3. No, and tell Claude what to do differently (esc)', ''].join('\n')

export class FakeCore implements Pipe {
  onBytes?: (b: Uint8Array) => void
  onClose?: (reason: string) => void
  /** The line stays open and nothing more arrives: a close that never reached the phone. */
  mute = false
  terms = new Map<string, FakeTerm>()
  docs = new Map<string, { value: any; version: number }>()
  machines: any[] = []
  calls: { method: string; params: any }[] = []
  written: { channel: number; data: string }[] = []
  /** Types back what is written, as a terminal in cooked mode does (Enter starts a new prompt): for the demo app. */
  echo = false
  resized: { channel: number; cols: number; rows: number }[] = []
  focused: number[] = []
  home = '/Users/sam'
  shell = '/bin/zsh'
  installed = ['claude', 'codex']
  /** `git status --porcelain=v2 --branch` output by folder, for the spaces list's branch. */
  git = new Map<string, string>()
  private framed = false
  private seq = 0
  private nextT = 1
  private nextCh = 1
  private eventCh?: number
  private attached = new Map<number, string>()
  /** Each viewer's size and when it was last used (attach, typing, resize, focus). herdr-core sizes a terminal to its
   *  last used viewer, and to the last used of those left when that one detaches (measured
   *  on the emulator: the phone leaving gave the width back to a 32-column viewer that never typed). */
  private viewing = new Map<number, { cols: number; rows: number; observe: boolean; used: number }>()
  private uses = 0
  /** A viewer that isn't the app (the computer's), at this size; `use` it to stand for typing there. */
  view(addr: string, cols: number, rows: number) {
    const ch = this.nextCh++
    this.attached.set(ch, addr)
    this.viewing.set(ch, { cols, rows, observe: false, used: ++this.uses })
    return { use: () => { this.viewing.get(ch)!.used = ++this.uses }, detach: () => { this.attached.delete(ch); this.viewing.delete(ch) } }
  }
  /** The terminal's size now, by herdr-core's rule above. */
  sizeOf(addr: string) {
    let best: { cols: number; rows: number; used: number } | undefined
    for (const [ch, a] of this.attached) {
      const v = this.viewing.get(ch)
      if (a === addr && v && !v.observe && (!best || v.used > best.used)) best = v
    }
    return best && { cols: best.cols, rows: best.rows }
  }
  /** How many viewers a terminal has. */
  viewers(addr: string) { return [...this.attached.values()].filter((a) => a === addr).length }
  private parser = new FrameParser((ch, kind, p) => this.frame(ch, kind, p))
  closed = false

  send(b: Uint8Array) {
    if (this.closed) return
    if (!this.framed) {
      this.framed = true
      const hello = {
        generation: 1, version: '0.1.13', framed: true, core: { id: 'core1', name: 'studio' }, features: ['framed', 'attach', 'events'],
        methods: ['hello', 'terminal.run', 'terminal.list', 'terminal.get', 'terminal.kill', 'terminal.label', 'terminal.attach', 'terminal.keep',
          'terminal.keys', 'terminal.send', 'doc.get', 'doc.set', 'events.subscribe', 'machine.list', 'machine.exec', 'file.read', 'templates.list', 'server.status'],
      }
      this.later(enc.encode(JSON.stringify({ jsonrpc: '2.0', id: 0, result: hello }) + '\n'))
      return
    }
    this.parser.push(b)
  }

  close() {
    this.drop('closed')
  }

  /** The line goes down: both sides see it close. */
  drop(reason = 'network') {
    if (this.closed) return
    this.closed = true
    queueMicrotask(() => this.onClose?.(reason))
  }

  private later(b: Uint8Array) {
    queueMicrotask(() => { if (!this.closed && !this.mute) this.onBytes?.(b) })
  }

  private out(ch: number, kind: number, payload: Uint8Array | string) {
    this.later(encodeFrame(ch, kind, typeof payload === 'string' ? enc.encode(payload) : payload))
  }

  emit(type: string, terminal: string | undefined, data: any, machine?: string) {
    const params: any = { seq: ++this.seq, type, data }
    if (terminal) params.terminal = terminal
    if (machine) params.machine = machine
    if (this.eventCh !== undefined) this.out(this.eventCh, DATA, JSON.stringify({ jsonrpc: '2.0', method: 'event', params }) + '\n')
  }

  /** Starts a terminal as `hc run` from outside would (an agent's helper, a script). */
  run(params: any): FakeTerm {
    const address = `t${this.nextT++}`
    const t: FakeTerm = {
      address, name: params.name ?? null, state: 'live', pid: 1000 + this.nextT, argv: params.argv?.length ? params.argv : [this.shell],
      cwd: params.cwd ?? this.home, labels: params.labels ?? {}, keep: !!params.keep, parent: params.parent ?? null, title: '',
      cols: 80, rows: 24, created_ms: 1, screen: 'exact', status: null, records: {}, reported: false, foreground: { name: 'zsh', argv: ['zsh'], cwd: params.cwd ?? this.home },
    }
    this.terms.set(address, t)
    this.emit('created', address, t)
    return t
  }

  /** A space of `doc`, the `herdr-app` document, with a live terminal for each tab. */
  addSpace(doc: { spaces: any[] }, space: string, folder: string, name: string, tabs: FakeTab[]) {
    const s = { id: space, name, folder, tabs: [] as any[] }
    for (const t of tabs) {
      const term = this.run({ argv: t.argv, cwd: folder, labels: { 'herdr.space': space, 'herdr.tab': t.id } })
      // What herdr-core answers for each (hc get): Codex is its node launcher in the foreground.
      if (t.foreground) term.foreground = t.argv[0] === 'codex' ? { name: 'node', argv: ['node', '/usr/local/bin/codex'], cwd: folder } : { name: t.argv[0], argv: t.argv, cwd: folder }
      if (t.screen) this.screens.set(term.address, t.screen)
      if (t.status) this.setStatus(term.address, t.status, t.argv[0] === 'codex' ? 'codex' : 'claude-code', { kind: t.kind, msg: t.msg })
      if (t.title) term.title = t.title
      s.tabs.push({ id: t.id, name: t.name ?? '', root: { id: t.id, type: 'row', children: [{ id: `p-${t.id}`, type: 'block', content: { terminal: term.address } }] } })
    }
    doc.spaces.push(s)
  }

  /** Methods whose answers wait for `release()`: herdr-core reports a new terminal (`created`) before `terminal.run`
   *  answers, and the app may hear both in either order. */
  holding = new Set<string>()
  private held: (() => void)[] = []
  release() { for (const f of this.held.splice(0)) f() }

  /** Keys pressed in a terminal (`terminal.keys`), for a test to play the program that reads them. */
  onKeys?: (terminal: string, keys: string[]) => void
  /** What `terminal.read` gives for a terminal: its screen. */
  screens = new Map<string, string>()
  /** Text sent to a terminal (`terminal.send`), for a test to read. */
  onSend?: (terminal: string, text: string, force: boolean) => void
  /** machine.exec lines it answers itself (the progress page's `herdr-anywhere track …`); UTF-8 out. */
  onExec?: (line: string) => { code?: number; stdout: string; stderr?: string } | undefined

  setStatus(addr: string, state: string | null, app: string | null = 'claude-code', extra: { kind?: string; msg?: string } = {}) {
    const t = this.terms.get(addr)!
    t.status = state ? { state, app, ...extra } : null
    t.reported = !!state
    this.emit('status', addr, { status: t.status, records: {}, reported: t.reported })
  }

  /** Sets labels as herdr-core does (an empty value removes one; hc 0.1.18) and tells every screen. */
  label(addr: string, labels: Record<string, string>) {
    const t = this.terms.get(addr)!
    for (const [k, v] of Object.entries(labels)) if (v === '') delete t.labels[k]; else t.labels[k] = v
    this.emit('labels', t.address, { labels: { ...t.labels } })
  }

  finish(addr: string, reason = 'exited') {
    const t = this.terms.get(addr)!
    t.state = 'finished'
    t.reason = reason
    this.emit('finished', addr, { reason, exit_code: 0, signal: null, finished_ms: 2 })
    for (const [ch, a] of this.attached) if (a === addr) {
      this.out(ch, CLOSE, JSON.stringify({ reason, exit_code: 0 }))
      this.attached.delete(ch)
    }
  }

  /** Another screen writes the layout document. */
  writeDoc(value: any, machine?: string) {
    const key = machine ?? ''
    const d = this.docs.get(key) ?? { value: null, version: 0 }
    this.docs.set(key, { value, version: d.version + 1 })
    this.emit('doc', undefined, { name: 'herdr-app', version: d.version + 1 }, machine)
  }

  doc(machine?: string) {
    return this.docs.get(machine ?? '')?.value ?? null
  }

  private used(ch: number) {
    const v = this.viewing.get(ch)
    if (v && !v.observe) v.used = ++this.uses
  }

  output(addr: string, text: string) {
    for (const [ch, a] of this.attached) if (a === addr) this.out(ch, DATA, text)
  }

  private frame(ch: number, kind: number, p: Uint8Array) {
    if (ch !== 0) {
      if (kind === DATA) {
        const data = dec.decode(p)
        this.written.push({ channel: ch, data })
        this.used(ch)
        const a = this.attached.get(ch)
        if (this.echo && a) this.out(ch, DATA, data.replace(/\r/g, `\r\n${a}$ `))
      }
      if (kind === FOCUS) { this.focused.push(ch); this.used(ch) }
      if (kind === RESIZE) {
        const v = new DataView(p.buffer, p.byteOffset)
        const size = { cols: v.getUint16(0, true), rows: v.getUint16(2, true) }
        this.resized.push({ channel: ch, ...size })
        const w = this.viewing.get(ch)
        if (w) Object.assign(w, size)
        this.used(ch)
      }
      if (kind === CLOSE && this.attached.delete(ch)) { this.viewing.delete(ch); this.out(ch, CLOSE, new Uint8Array(0)) }
      return
    }
    if (kind !== DATA) return
    const msg = JSON.parse(dec.decode(p))
    this.calls.push({ method: msg.method, params: msg.params })
    const ok = (result: any) => {
      const send = () => this.out(0, DATA, JSON.stringify({ jsonrpc: '2.0', id: msg.id, result }))
      if (this.holding.has(msg.method)) this.held.push(send)
      else send()
    }
    const fail = (kind: string, message: string) =>
      this.out(0, DATA, JSON.stringify({ jsonrpc: '2.0', id: msg.id, error: { code: -32000, message, data: { kind } } }))
    const pr = msg.params ?? {}
    const term = (a: string) => this.terms.get(a) ?? [...this.terms.values()].find((t) => t.name === a)
    switch (msg.method) {
      case 'events.subscribe': {
        this.eventCh = this.nextCh++
        return ok({ seq: this.seq, terminals: [...this.terms.values()], channel: this.eventCh })
      }
      case 'terminal.list': return ok({ terminals: [...this.terms.values()] })
      case 'terminal.get': { const t = term(pr.terminal); return t ? ok(t) : fail('not_found', `no terminal "${pr.terminal}"`) }
      case 'terminal.run': return ok(this.run(pr))
      case 'terminal.kill': { const t = term(pr.terminal); if (!t) return fail('not_found', 'no terminal'); this.finish(t.address, 'killed'); return ok(t) }
      case 'terminal.keep': { const t = term(pr.terminal)!; t.keep = pr.keep; this.emit('keep', t.address, { keep: pr.keep }); return ok(t) }
      case 'terminal.label': {
        const t = term(pr.terminal)
        if (!t) return fail('not_found', `no terminal "${pr.terminal}"`)
        this.label(t.address, pr.labels)
        return ok(t)
      }
      case 'terminal.keys': this.onKeys?.(pr.terminal, pr.keys); return ok({ terminal: pr.terminal, keys: pr.keys })
      case 'terminal.send': this.onSend?.(pr.terminal, pr.text, !!pr.force); return ok({ terminal: pr.terminal })
      case 'terminal.read': return ok({ text: this.screens.get(pr.terminal) ?? '', complete: true, how: 'screen', why: null })
      case 'terminal.attach': {
        const t = term(pr.terminal)
        if (!t) return fail('not_found', 'no terminal')
        if (t.state !== 'live') return fail('finished', 'it finished')
        const ch = this.nextCh++
        this.attached.set(ch, t.address)
        this.viewing.set(ch, { cols: pr.cols, rows: pr.rows, observe: !!pr.observe, used: ++this.uses })
        ok({ channel: ch, terminal: t })
        // A terminal with a screen of its own (`screens`) draws it, as the program would; else a prompt.
        const screen = this.screens.get(t.address)
        this.out(ch, DATA, `\x1b[H\x1b[2J${screen ? screen.replace(/\n/g, '\r\n') : `${t.address}$ `}`)
        return
      }
      case 'doc.get': { const d = this.docs.get(pr.on ?? '') ?? { value: null, version: 0 }; return ok(d) }
      case 'doc.set': {
        const key = pr.on ?? ''
        const d = this.docs.get(key) ?? { value: null, version: 0 }
        if (d.version !== pr.version) return fail('conflict', 'the document changed')
        this.docs.set(key, { value: pr.value, version: d.version + 1 })
        ok({ version: d.version + 1 })
        this.emit('doc', undefined, { name: pr.name, version: d.version + 1 }, pr.on ?? undefined)
        return
      }
      case 'machine.list': return ok({ machines: this.machines })
      case 'machine.exec': {
        const line: string = pr.line ?? ''
        const own = this.onExec?.(line)
        if (own) return ok({ code: own.code ?? 0, stdout: b64utf8(own.stdout), stderr: b64utf8(own.stderr ?? ''), ms: 1, signal: null, timed_out: false })
        let stdout = ''
        if (line.includes('$HOME')) stdout = `${this.home}\n${this.shell}\n`
        else if (line.includes('herdr-has')) stdout = this.installed.map((c) => `herdr-has:${c}\n`).join('')
        else if (line.startsWith('git ')) {
          const g = this.git.get(pr.cwd ?? '')
          if (g === undefined) return ok({ code: 128, stdout: '', stderr: btoa('not a git repository'), ms: 1, signal: null, timed_out: false })
          stdout = `${g}@@herdr\n${pr.cwd}\n`
        }
        return ok({ code: 0, stdout: btoa(stdout), stderr: '', ms: 1, signal: null, timed_out: false })
      }
      case 'file.read': return ok({ text: 'hello\n', first: 1, last: 1, lines: 1 })
      case 'templates.list': return ok({ templates: [] })
      case 'server.status': return ok({ version: '0.1.13' })
      default: return fail('unknown', `no method ${msg.method}`)
    }
  }
}

function b64utf8(s: string) {
  let bin = ''
  for (const b of new TextEncoder().encode(s)) bin += String.fromCharCode(b)
  return btoa(bin)
}

export const settle = async (n = 5) => {
  for (let i = 0; i < n; i++) await new Promise((r) => setTimeout(r, 0))
}

export { CREDIT }
