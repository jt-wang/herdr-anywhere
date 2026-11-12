import { expect, test } from 'nsh/test'
import { CLOSE, CREDIT, DATA, FrameParser, RESIZE, encodeFrame } from './frames.ts'
import { HcClient, HcError, type Pipe } from './client.ts'

const enc = new TextEncoder()
const dec = new TextDecoder()

/** Plays herdr-core on the far side of the pipe. */
class FakeCore implements Pipe {
  onBytes?: (b: Uint8Array) => void
  onClose?: (reason: string) => void
  framed = false
  sent: { ch: number; kind: number; payload: Uint8Array }[] = []
  calls: any[] = []
  answer: (msg: any) => any = () => ({})
  private parser = new FrameParser((ch, kind, payload) => {
    this.sent.push({ ch, kind, payload })
    if (ch === 0 && kind === DATA) {
      const msg = JSON.parse(dec.decode(payload))
      this.calls.push(msg)
      const r = this.answer(msg)
      if (r !== undefined) this.reply(msg.id, r)
    }
  })
  hello: any = { generation: 1, version: '0.1.13', framed: true, core: { id: 'c', name: 'mac' },
    methods: ['hello', 'terminal.run', 'terminal.list', 'terminal.kill', 'terminal.label', 'terminal.attach', 'terminal.keep', 'doc.get', 'doc.set', 'events.subscribe'],
    features: ['framed'] }
  send(b: Uint8Array) {
    if (!this.framed) {
      const line = JSON.parse(dec.decode(b))
      this.calls.push(line)
      this.framed = true
      queueMicrotask(() => this.onBytes?.(enc.encode(JSON.stringify({ jsonrpc: '2.0', id: 0, result: this.hello }) + '\n')))
      return
    }
    this.parser.push(b)
  }
  close() { this.onClose?.('closed') }
  reply(id: number, result: any) { this.frame(0, DATA, JSON.stringify({ jsonrpc: '2.0', id, result })) }
  fail(id: number, kind: string, message: string) {
    this.frame(0, DATA, JSON.stringify({ jsonrpc: '2.0', id, error: { code: -32000, message, data: { kind, hint: 'h' } } }))
  }
  frame(ch: number, kind: number, payload: string | Uint8Array) {
    const p = typeof payload === 'string' ? enc.encode(payload) : payload
    queueMicrotask(() => this.onBytes?.(encodeFrame(ch, kind, p)))
  }
}

const tick = () => new Promise((r) => setTimeout(r, 0))

test('hello asks for framed mode and returns what the core said', async () => {
  const core = new FakeCore()
  const hc = new HcClient(core)
  const h = await hc.hello()
  expect(core.calls[0]).toEqual({ jsonrpc: '2.0', id: 0, method: 'hello', params: { framed: true } })
  expect(h.core.name).toBe('mac')
})

test('hello refuses a core older than the desktop app accepts', async () => {
  const core = new FakeCore()
  core.hello.version = '0.1.9'
  await expect(() => new HcClient(core).hello()).rejects.toThrow('0.1.11')
})

test('hello refuses a core that lacks a method the app needs', async () => {
  const core = new FakeCore()
  core.hello.methods = core.hello.methods.filter((m: string) => m !== 'doc.set')
  await expect(() => new HcClient(core).hello()).rejects.toThrow('doc.set')
})

test('calls are matched by id and errors keep the core kind', async () => {
  const core = new FakeCore()
  const hc = new HcClient(core)
  await hc.hello()
  core.answer = (m) => {
    if (m.method === 'terminal.get') { core.fail(m.id, 'not_found', 'no terminal "t9"'); return undefined }
    return { listed: m.method }
  }
  const [a, b] = await Promise.all([hc.call('terminal.list', {}), hc.call('terminal.get', { terminal: 't9' }).catch((e) => e)])
  expect(a).toEqual({ listed: 'terminal.list' })
  expect(b instanceof HcError).toBe(true)
  expect((b as HcError).kind).toBe('not_found')
})

test('a call with no answer times out', async () => {
  const core = new FakeCore()
  const hc = new HcClient(core, { timeoutMs: 20 })
  await hc.hello()
  core.answer = () => undefined
  await expect(() => hc.call('terminal.list', {})).rejects.toThrow('timed out')
})

test('events arrive as lines split anywhere across frames, and each chunk is credited', async () => {
  const core = new FakeCore()
  const hc = new HcClient(core)
  await hc.hello()
  core.answer = (m) => m.method === 'events.subscribe' ? { seq: 4, terminals: [{ address: 't1' }], channel: 3 } : {}
  const seen: any[] = []
  const snap = await hc.subscribe((e) => seen.push(e))
  expect(snap.seq).toBe(4)
  const a = JSON.stringify({ jsonrpc: '2.0', method: 'event', params: { seq: 5, type: 'title', terminal: 't1', data: { title: 'x' } } })
  const b = JSON.stringify({ jsonrpc: '2.0', method: 'heartbeat', params: { seq: 5 } })
  const all = a + '\n' + b + '\n'
  core.frame(3, DATA, all.slice(0, 17))
  core.frame(3, DATA, all.slice(17))
  await tick(); await tick()
  expect(seen.map((e) => e.method)).toEqual(['event', 'heartbeat'])
  const credits = core.sent.filter((f) => f.ch === 3 && f.kind === CREDIT)
  const total = credits.reduce((n, f) => n + new DataView(f.payload.buffer).getUint32(0, true), 0)
  expect(total).toBe(enc.encode(all).length)
})

test('attach streams bytes, sends keys and sizes on its channel, and reports the close reason', async () => {
  const core = new FakeCore()
  const hc = new HcClient(core)
  await hc.hello()
  core.answer = (m) => m.method === 'terminal.attach' ? { channel: 9, terminal: { address: 't1' } } : {}
  const got: string[] = []
  let closed: any
  const s = await hc.attach({ terminal: 't1', cols: 80, rows: 24, history: 2000 }, {
    data: (b) => got.push(dec.decode(b)),
    close: (r) => { closed = r },
  })
  expect(core.calls.at(-1).params).toEqual({ terminal: 't1', cols: 80, rows: 24, history: 2000 })
  core.frame(9, DATA, 'paint')
  await tick()
  expect(got).toEqual(['paint'])
  s.write('ls\r')
  s.resize(100, 30)
  const mine = core.sent.filter((f) => f.ch === 9)
  expect(mine.map((f) => f.kind)).toEqual([CREDIT, DATA, RESIZE])
  expect(dec.decode(mine[1].payload)).toBe('ls\r')
  core.frame(9, CLOSE, JSON.stringify({ reason: 'exited', exit_code: 0 }))
  await tick()
  expect(closed).toEqual({ reason: 'exited', exit_code: 0 })
})

test('detaching sends CLOSE once and stops delivering', async () => {
  const core = new FakeCore()
  const hc = new HcClient(core)
  await hc.hello()
  core.answer = () => ({ channel: 2, terminal: {} })
  const got: string[] = []
  const s = await hc.attach({ terminal: 't1', cols: 80, rows: 24 }, { data: (b) => got.push(dec.decode(b)), close: () => {} })
  s.detach()
  s.detach()
  core.frame(2, DATA, 'late')
  await tick()
  expect(got).toEqual([])
  expect(core.sent.filter((f) => f.ch === 2 && f.kind === CLOSE).length).toBe(1)
})

test('losing the pipe fails pending calls and tells every stream', async () => {
  const core = new FakeCore()
  const hc = new HcClient(core)
  await hc.hello()
  core.answer = (m) => m.method === 'terminal.attach' ? { channel: 4, terminal: {} } : undefined
  let streamClosed: any
  await hc.attach({ terminal: 't1', cols: 80, rows: 24 }, { data: () => {}, close: (r) => { streamClosed = r } })
  const pending = hc.call('terminal.list', {}).then(() => 'answered', (e) => e.message)
  let lost = ''
  hc.onClose = (r) => { lost = r }
  core.close()
  expect(await pending).toBe('connection lost')
  expect(streamClosed).toEqual({ reason: 'disconnected' })
  expect(lost).toBe('closed')
})

test('a first paint and first events that come in the same read as their reply are kept', async () => {
  const core = new FakeCore()
  const hc = new HcClient(core)
  await hc.hello()
  const together = (id: number, result: any, ch: number, data: string) => queueMicrotask(() => core.onBytes?.(concatBytes(
    encodeFrame(0, DATA, enc.encode(JSON.stringify({ jsonrpc: '2.0', id, result }))), encodeFrame(ch, DATA, enc.encode(data)))))
  core.answer = (m) => {
    if (m.method === 'terminal.attach') together(m.id, { channel: 9, terminal: { address: 't1' } }, 9, 'first paint')
    if (m.method === 'events.subscribe') together(m.id, { seq: 1, terminals: [], channel: 3 }, 3, JSON.stringify({ jsonrpc: '2.0', method: 'event', params: { seq: 2 } }) + '\n')
    return undefined
  }
  const got: string[] = []
  await hc.attach({ terminal: 't1', cols: 80, rows: 24 }, { data: (b) => got.push(dec.decode(b)), close: () => {} })
  const seen: any[] = []
  await hc.subscribe((e) => seen.push(e))
  await tick()
  expect(got).toEqual(['first paint'])
  expect(seen.map((e) => e.params.seq)).toEqual([2])
})

function concatBytes(a: Uint8Array, b: Uint8Array) {
  const o = new Uint8Array(a.length + b.length)
  o.set(a)
  o.set(b, a.length)
  return o
}

test('a read full of frames answers with one credit, for all of it', async () => {
  const core = new FakeCore()
  core.answer = (m) => m.method === 'terminal.attach' ? { channel: 7, terminal: { address: 't' } } : {}
  const hc = new HcClient(core)
  await hc.hello()
  let got = 0
  await hc.attach({ terminal: 't', cols: 80, rows: 24 }, { data: (b) => { got += b.length }, close: () => {} })
  core.sent.length = 0
  const frames = Array.from({ length: 50 }, () => encodeFrame(7, DATA, new Uint8Array(100)))
  const read = new Uint8Array(frames.reduce((n, f) => n + f.length, 0))
  let at = 0
  for (const f of frames) { read.set(f, at); at += f.length }
  core.onBytes!(read)
  expect(got).toBe(5000)
  const credits = core.sent.filter((f) => f.ch === 7 && f.kind === CREDIT)
  expect(credits.length).toBe(1)
  expect(new DataView(credits[0].payload.buffer, credits[0].payload.byteOffset).getUint32(0, true)).toBe(5000)
})

test('frames that came before the reply are credited once they are read', async () => {
  const core = new FakeCore()
  core.answer = (m) => {
    if (m.method !== 'terminal.attach') return {}
    for (let i = 0; i < 3; i++) core.frame(7, DATA, new Uint8Array(10))
    return { channel: 7, terminal: { address: 't' } }
  }
  const hc = new HcClient(core)
  await hc.hello()
  await hc.attach({ terminal: 't', cols: 80, rows: 24 }, { data: () => {}, close: () => {} })
  await tick()
  const total = core.sent.filter((f) => f.ch === 7 && f.kind === CREDIT).reduce((n, f) => n + new DataView(f.payload.buffer, f.payload.byteOffset).getUint32(0, true), 0)
  expect(total).toBe(30)
})
