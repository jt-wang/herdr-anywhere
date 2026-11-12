// A herdr-core client over any byte pipe, as herdr-app speaks it (herdr-app/src/hc.rs):
// one `hello {framed:true}` line, then frames; JSON-RPC on channel 0, events and terminals on their own channels.
import { CLOSE, CREDIT, DATA, FOCUS, FrameParser, RESIZE, concat, encodeFrame, resizePayload, u32 } from './frames.ts'

/** Bytes both ways: a WebSocket to the companion, or a fake core in tests. */
export interface Pipe {
  send(bytes: Uint8Array): void
  close(): void
  onBytes?: (bytes: Uint8Array) => void
  onClose?: (reason: string) => void
}

/** What frames on not-yet-claimed channels may hold before more are dropped. */
const EARLY_LIMIT = 4 << 20

export type Hello = {
  generation: number
  version: string
  framed: boolean
  core: { id: string; name: string }
  methods: string[]
  features: string[]
}

export class HcError extends Error {
  constructor(message: string, readonly kind?: string, readonly hint?: string) {
    super(message)
  }
}

export const MIN_VERSION = '0.1.11'
// The methods herdr-app refuses to run without (herdr-app/src/hc.rs:157).
export const NEEDED = ['terminal.run', 'terminal.list', 'terminal.kill', 'terminal.label', 'terminal.attach',
  'terminal.keep', 'doc.get', 'doc.set', 'events.subscribe']

export type AttachParams = {
  terminal: string
  cols: number
  rows: number
  history?: number
  observe?: boolean
  resumed?: boolean
  display?: { fg?: string; bg?: string; cursor?: string }
}

export type CloseReason = { reason: string; exit_code?: number | null; signal?: number | null; [k: string]: unknown }

export type StreamHandlers = { data: (bytes: Uint8Array) => void; close: (why: CloseReason) => void }

export type Stream = {
  channel: number
  terminal: any
  write(data: string | Uint8Array): void
  resize(cols: number, rows: number): void
  focus(on: boolean): void
  detach(): void
}

const enc = new TextEncoder()
const dec = new TextDecoder()

export function newerOrSame(version: string, min: string): boolean {
  const a = version.split(/[.-]/).map((n) => parseInt(n, 10) || 0)
  const b = min.split('.').map((n) => parseInt(n, 10) || 0)
  for (let i = 0; i < 3; i++) if ((a[i] ?? 0) !== (b[i] ?? 0)) return (a[i] ?? 0) > (b[i] ?? 0)
  return true
}

type Pending = { resolve: (v: any) => void; reject: (e: Error) => void; timer: ReturnType<typeof setTimeout> }

export class HcClient {
  onClose?: (reason: string) => void
  private framed = false
  private line: Uint8Array = new Uint8Array(0)
  private nextId = 1
  private pending = new Map<number, Pending>()
  private channels = new Map<number, (kind: number, payload: Uint8Array) => void>()
  /** Frames on a channel whose reply is still being read (a first paint, the first events come in the same read). */
  private early = new Map<number, { kind: number; payload: Uint8Array }[]>()
  private earlyBytes = 0
  /** Bytes read per channel and not yet credited: one credit per read, since NSH's send queue is short. */
  private unsaid = new Map<number, number>()
  private helloWait?: { resolve: (h: Hello) => void; reject: (e: Error) => void }
  private closed = false
  /** Called on any bytes from the computer. */
  onHeard?: () => void
  private timeoutMs: number
  private parser = new FrameParser((ch, kind, payload) => this.frame(ch, kind, payload))

  constructor(private pipe: Pipe, opts: { timeoutMs?: number } = {}) {
    this.timeoutMs = opts.timeoutMs ?? 10_000
    pipe.onBytes = (b) => this.bytes(b)
    pipe.onClose = (r) => this.lost(r)
  }

  hello(): Promise<Hello> {
    return new Promise<Hello>((resolve, reject) => {
      this.helloWait = { resolve, reject }
      this.pipe.send(enc.encode(JSON.stringify({ jsonrpc: '2.0', id: 0, method: 'hello', params: { framed: true } }) + '\n'))
    }).then((h) => {
      if (h.generation !== 1) throw new HcError(`this core speaks protocol generation ${h.generation}; Herdr Anywhere speaks 1`)
      if (!newerOrSame(h.version, MIN_VERSION)) throw new HcError(`herdr-core ${h.version} is too old: update it to ${MIN_VERSION} or newer (hc update)`, 'needs_update')
      const missing = NEEDED.filter((m) => !h.methods.includes(m))
      if (missing.length) throw new HcError(`this herdr-core lacks ${missing.join(', ')}: update it (hc update)`, 'needs_update')
      return h
    })
  }

  call<T = any>(method: string, params: object = {}): Promise<T> {
    if (this.closed) return Promise.reject(new HcError('connection lost', 'disconnected'))
    const id = this.nextId++
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new HcError(`${method} timed out`, 'timeout'))
      }, this.timeoutMs)
      this.pending.set(id, { resolve, reject, timer })
      this.send(0, DATA, enc.encode(JSON.stringify({ jsonrpc: '2.0', id, method, params })))
    })
  }

  /** Follows every change: the snapshot, then `event`, `heartbeat` and `missed` messages in order. */
  async subscribe(onMessage: (msg: any) => void): Promise<{ seq: number; terminals: any[] }> {
    const r = await this.call<{ seq: number; terminals: any[]; channel: number }>('events.subscribe', {})
    let rest = ''
    this.listen(r.channel, (kind, payload) => {
      if (kind !== DATA) return
      this.credit(r.channel, payload.length)
      rest += dec.decode(payload, { stream: true })
      let nl: number
      while ((nl = rest.indexOf('\n')) >= 0) {
        const one = rest.slice(0, nl)
        rest = rest.slice(nl + 1)
        if (one.trim()) onMessage(JSON.parse(one))
      }
    })
    return { seq: r.seq, terminals: r.terminals }
  }

  async attach(params: AttachParams, on: StreamHandlers): Promise<Stream> {
    const r = await this.call<{ channel: number; terminal: any }>('terminal.attach', params)
    const ch = r.channel
    let open = true
    this.listen(ch, (kind, payload) => {
      if (!open) return
      if (kind === DATA) {
        this.credit(ch, payload.length)
        on.data(payload)
      } else if (kind === CLOSE) {
        open = false
        this.channels.delete(ch)
        on.close(payload.length ? JSON.parse(dec.decode(payload)) : { reason: 'detached' })
      }
    })
    const self = this
    return {
      channel: ch,
      terminal: r.terminal,
      write(data) {
        if (open) self.send(ch, DATA, typeof data === 'string' ? enc.encode(data) : data)
      },
      resize(cols, rows) {
        if (open) self.send(ch, RESIZE, resizePayload(cols, rows))
      },
      focus(on) {
        if (open) self.send(ch, FOCUS, new Uint8Array([on ? 1 : 0]))
      },
      detach() {
        if (!open) return
        open = false
        self.channels.delete(ch)
        self.send(ch, CLOSE, new Uint8Array(0))
      },
    }
  }

  close() {
    this.pipe.close()
  }

  private send(ch: number, kind: number, payload: Uint8Array) {
    if (!this.closed) this.pipe.send(encodeFrame(ch, kind, payload))
  }

  private credit(ch: number, bytes: number) {
    this.unsaid.set(ch, (this.unsaid.get(ch) ?? 0) + bytes)
  }

  private sayCredits() {
    for (const [ch, bytes] of this.unsaid) this.send(ch, CREDIT, u32(bytes))
    this.unsaid.clear()
  }

  private bytes(b: Uint8Array) {
    this.onHeard?.()
    if (this.framed) {
      this.parser.push(b)
      this.sayCredits()
      return
    }
    // Before framing: the hello reply is one JSON line; anything after it is already framed.
    this.line = concat(this.line, b)
    const nl = this.line.indexOf(10)
    if (nl < 0) return
    const text = dec.decode(this.line.subarray(0, nl))
    const after = this.line.slice(nl + 1)
    this.line = new Uint8Array(0)
    const msg = JSON.parse(text)
    const wait = this.helloWait
    this.helloWait = undefined
    if (msg.error) {
      wait?.reject(new HcError(msg.error.message, msg.error.data?.kind, msg.error.data?.hint))
      return
    }
    this.framed = true
    wait?.resolve(msg.result)
    if (after.length) this.parser.push(after)
    this.sayCredits()
  }

  private listen(ch: number, h: (kind: number, payload: Uint8Array) => void) {
    this.channels.set(ch, h)
    const q = this.early.get(ch)
    if (!q) return
    this.early.delete(ch)
    for (const f of q) {
      this.earlyBytes -= f.payload.length
      if (this.channels.get(ch) === h) h(f.kind, f.payload)
    }
    this.sayCredits()
  }

  private frame(ch: number, kind: number, payload: Uint8Array) {
    if (ch !== 0) {
      const h = this.channels.get(ch)
      if (h) return h(kind, payload)
      if (this.earlyBytes + payload.length > EARLY_LIMIT) return
      this.earlyBytes += payload.length
      const q = this.early.get(ch) ?? []
      q.push({ kind, payload })
      return void this.early.set(ch, q)
    }
    if (kind !== DATA) return
    const msg = JSON.parse(dec.decode(payload))
    const p = this.pending.get(msg.id)
    if (!p) return
    this.pending.delete(msg.id)
    clearTimeout(p.timer)
    if (msg.error) p.reject(new HcError(msg.error.message, msg.error.data?.kind, msg.error.data?.hint))
    else p.resolve(msg.result)
  }

  private lost(reason: string) {
    if (this.closed) return
    this.closed = true
    this.helloWait?.reject(new HcError(reason || 'connection lost', 'disconnected'))
    for (const p of this.pending.values()) {
      clearTimeout(p.timer)
      p.reject(new HcError('connection lost', 'disconnected'))
    }
    this.pending.clear()
    for (const h of this.channels.values()) h(CLOSE, enc.encode(JSON.stringify({ reason: 'disconnected' })))
    this.channels.clear()
    this.early.clear()
    this.earlyBytes = 0
    this.onClose?.(reason)
  }
}
