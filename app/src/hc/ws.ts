// The byte pipe to the companion's `/hc` (companion/internal/server/bridge.go): each binary message is bytes for
// `hc connect`'s stdin and back, or base64 text with `enc=b64` for a WebSocket that cannot carry binary.
// Close reasons: `unpaired` (4401: the computer forgot this phone), `no-core` (4503: herdr-core is not installed),
// `binary-unsupported` (1003, or binary that gets no answer), `network`, `closed` (we closed it). Through TailPocket,
// src/link.ts turns a `network` close before any answer into `no-answer`.
import type { Pipe } from './client.ts'

export type Enc = 'bin' | 'b64'
export type SocketIO = { WebSocket: typeof WebSocket; setTimeout: typeof setTimeout; clearTimeout: typeof clearTimeout }

/** The first answer (hello's) comes in well under this; binary that stays unanswered is binary not getting through. */
const ANSWER_MS = 8000
/** NSH's send queue is short (it refused a send with 8 frames in it, on the emulator) and empties in moments.
 *  What it refuses waits here. A socket that takes nothing this long has gone without saying so. */
const RETRY_MS = 20
const REFUSED_MS = 5000
const MAX_WAITING = 8 << 20
const MAX_MESSAGE = 2 << 20

export function socketUrl(base: string, token: string, enc: Enc): string {
  const u = base.replace(/\/+$/, '').replace(/^http/, 'ws')
  return `${u}/hc?token=${encodeURIComponent(token)}${enc === 'b64' ? '&enc=b64' : ''}`
}

export function toBase64(b: Uint8Array): string {
  let s = ''
  for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode(...b.subarray(i, i + 0x8000))
  return btoa(s)
}

export function fromBase64(t: string): Uint8Array {
  const s = atob(t)
  const b = new Uint8Array(s.length)
  for (let i = 0; i < s.length; i++) b[i] = s.charCodeAt(i)
  return b
}

const REASON: Record<number, string> = { 4401: 'unpaired', 4503: 'no-core', 1003: 'binary-unsupported' }

export class SocketPipe implements Pipe {
  onBytes?: (b: Uint8Array) => void
  onClose?: (reason: string) => void
  private ws: WebSocket
  private queue: Uint8Array[] = []
  private done = false
  private heard = false
  private watchdog?: number
  private refused?: number
  private again?: number
  private waiting: Uint8Array[] = []
  private waitingBytes = 0

  constructor(url: string, private enc: Enc, private io: SocketIO = { WebSocket, setTimeout, clearTimeout }) {
    this.ws = new io.WebSocket(url)
    this.ws.binaryType = 'arraybuffer'
    this.ws.onopen = () => {
      for (const b of this.queue.splice(0)) this.send(b)
    }
    this.ws.onmessage = (e) => {
      this.heard = true
      this.io.clearTimeout(this.watchdog)
      const d = e.data
      this.onBytes?.(typeof d === 'string' ? fromBase64(d) : new Uint8Array(d as ArrayBuffer))
    }
    this.ws.onerror = () => {} // a close always follows, with the code
    this.ws.onclose = (e) => this.end(REASON[e.code] ?? 'network')
  }

  send(b: Uint8Array) {
    if (this.done) return
    if (this.ws.readyState !== 1) this.queue.push(b)
    else if (this.waiting.length || !this.write(b)) this.wait(b)
  }

  close() {
    this.end('closed')
  }

  private write(b: Uint8Array): boolean {
    try {
      if (this.enc === 'b64') this.ws.send(toBase64(b))
      else this.ws.send(b.slice().buffer)
    } catch {
      // NSH's send throws when its queue is full, and when the socket is closing before readyState says so. A close
      // brings its code ("pair again" is 4401).
      return false
    }
    if (this.enc === 'bin' && !this.heard && this.watchdog === undefined) this.watchdog = this.io.setTimeout(() => { if (!this.heard) this.end('binary-unsupported') }, ANSWER_MS)
    return true
  }

  private wait(b: Uint8Array) {
    this.waiting.push(b)
    this.waitingBytes += b.length
    if (this.waitingBytes > MAX_WAITING) return this.end('network')
    if (this.refused === undefined) this.refused = this.io.setTimeout(() => this.end('network'), REFUSED_MS)
    if (this.again === undefined) this.again = this.io.setTimeout(() => this.drain(), RETRY_MS)
  }

  /** The bytes are one stream to the computer, so what waited goes out together. */
  private drain() {
    this.again = undefined
    if (this.done) return
    while (this.waiting.length) {
      let n = 1
      let size = this.waiting[0].length
      while (n < this.waiting.length && size + this.waiting[n].length <= MAX_MESSAGE) size += this.waiting[n++].length
      const message = new Uint8Array(size)
      let at = 0
      for (const b of this.waiting.slice(0, n)) { message.set(b, at); at += b.length }
      if (!this.write(message)) { this.again = this.io.setTimeout(() => this.drain(), RETRY_MS); return }
      this.waiting.splice(0, n)
      this.waitingBytes -= size
    }
    this.io.clearTimeout(this.refused)
    this.refused = undefined
  }

  private end(reason: string) {
    if (this.done) return
    this.done = true
    this.io.clearTimeout(this.watchdog)
    this.io.clearTimeout(this.refused)
    this.io.clearTimeout(this.again)
    if (this.ws.readyState < 2) this.ws.close(1000, reason)
    this.onClose?.(reason)
  }
}
