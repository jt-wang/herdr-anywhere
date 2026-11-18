// Real hc, real HcClient, isolated HOME/XDG/session. No phone rendering or radio-power claim.
// HM_TEST_HC=/path/to/herdr-core bun app/e2e/terminal-credit.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { once } from 'node:events'
import { mkdirSync, mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { createConnection } from 'node:net'
import { resolve, join } from 'node:path'
import { HcClient } from '../src/hc/client.ts'
import { FrameParser, DATA, CREDIT } from '../src/hc/frames.ts'

const bin = process.env.HM_TEST_HC
assert(bin, 'set HM_TEST_HC to an installed core; this test never downloads or upgrades it')
const scratch = resolve('.amp/in/orb-backup')
mkdirSync(scratch, { recursive: true })
const root = mkdtempSync(join(scratch, 'credit-'))
const env = { PATH: process.env.PATH, HOME: root, HC_SESSION: 'power',
  XDG_RUNTIME_DIR: join(root, 'run'), XDG_STATE_HOME: join(root, 'state'), XDG_CONFIG_HOME: join(root, 'config') }
for (const p of [env.XDG_RUNTIME_DIR, env.XDG_STATE_HOME, env.XDG_CONFIG_HOME]) mkdirSync(p)
const fake = join(root, 'fake-agent')
const cli = (...args) => execFileSync(bin, args, { env, encoding: 'utf8', timeout: 10_000 })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
async function until(what, done) {
  const end = Date.now() + 15_000
  while (!done()) { assert(Date.now() < end, `timed out: ${what}`); await sleep(20) }
}

let socket
try {
  execFileSync('go', ['build', '-o', fake, 'app/e2e/fake-claude/main.go'])
  console.log('CORE_VERSION', cli('--version').trim())
  cli('--json', 'server', 'start')
  socket = createConnection(join(env.XDG_RUNTIME_DIR, 'herdr-core/power.sock'))
  await once(socket, 'connect')
  const counts = new Map()
  const count = (ch) => { if (!counts.has(ch)) counts.set(ch, { rx: 0, credit: 0, credits: 0 }); return counts.get(ch) }
  const incoming = new FrameParser((ch, kind, b) => { if (kind === DATA) count(ch).rx += b.length })
  const outgoing = new FrameParser((ch, kind, b) => {
    if (kind === CREDIT) { count(ch).credit += new DataView(b.buffer, b.byteOffset).getUint32(0, true); count(ch).credits++ }
  })
  let firstSend = true, firstReceive = true
  let hello = Buffer.alloc(0)
  const pipe = {
    send(b) { if (firstSend) firstSend = false; else outgoing.push(b); socket.write(b) },
    close() { socket.destroy() },
  }
  socket.on('data', (b) => {
    if (firstReceive) {
      hello = Buffer.concat([hello, b])
      const nl = hello.indexOf(10)
      if (nl >= 0) { firstReceive = false; incoming.push(hello.subarray(nl + 1)); hello = Buffer.alloc(0) }
    } else incoming.push(b)
    pipe.onBytes?.(b)
  })
  socket.on('close', () => pipe.onClose?.('disconnected'))
  const hc = new HcClient(pipe)
  await hc.hello()
  const events = []
  await hc.subscribe((e) => events.push(e))
  for (const paused of [false, true]) {
    const go = join(root, `go-${paused}`)
    const run = await hc.call('terminal.run', { argv: [fake], cwd: root,
      env: { FAKE_STATE: 'working', FAKE_THEN: 'done', FAKE_GO_FILE: go, FAKE_FLOOD_BYTES: String(8 << 20) } })
    const terminal = run.address
    // An older desktop viewer makes a detach's unintended width hand-off observable.
    const desktop = await hc.attach({ terminal, cols: 97, rows: 33 }, { data() {}, close() {} })
    let received = '', delivered = 0
    const mobile = await hc.attach({ terminal, cols: 61, rows: 19 }, {
      data(b) { delivered += b.length; received = (received + Buffer.from(b).toString()).slice(-8192) }, close() {},
    })
    let live = ''
    const observer = await hc.attach({ terminal, cols: 61, rows: 19, observe: true }, {
      data(b) { live = (live + Buffer.from(b).toString()).slice(-8192) }, close() {},
    })
    await sleep(100) // drain the initial paint and its credit before measuring the same offered workload
    mobile.pause(paused)
    counts.set(mobile.channel, { rx: 0, credit: 0, credits: 0 })
    delivered = 0; received = ''; events.length = 0
    writeFileSync(go, '')
    await until('other viewer reaches final output', () => live.includes('SYNTHETIC FLOOD COMPLETE'))
    await until('independent status event', () => events.some((e) => e.params?.terminal === terminal
      && e.params.type === 'status' && e.params.data?.status?.state === 'done'))
    const started = performance.now()
    const record = await hc.call('terminal.get', { terminal })
    const rpcMs = performance.now() - started
    assert.deepEqual([record.cols, record.rows], [61, 19], 'paused viewer must keep width, not detach to desktop')
    assert(rpcMs < 1000, `same-connection RPC stalled: ${rpcMs}ms`)
    await sleep(100)
    const before = { ...count(mobile.channel), delivered, rpcMs: Math.round(rpcMs * 100) / 100 }
    if (paused) {
      assert(before.rx > 0 && before.rx <= 256 * 1024, `exceeded remaining credit window: ${before.rx}`)
      assert.equal(before.credit, 0)
      assert.equal(before.delivered, 0)
      mobile.pause(false)
    } else assert(before.rx >= 8 << 20, `foreground control missed output: ${before.rx}`)
    await until('mobile resumes to final output', () => received.includes('SYNTHETIC FLOOD COMPLETE'))
    console.log('TERMINAL_CREDIT', JSON.stringify({ paused, offered: 8 << 20, before,
      after: count(mobile.channel), width: [record.cols, record.rows], statusEvent: true, otherViewerComplete: true }))
    if (paused) {
      assert(count(mobile.channel).rx < 6 << 20, 'core should repaint, not replay the whole 8 MiB backlog')
      mobile.pause(true)
      await hc.call('terminal.kill', { terminal })
      mobile.pause(false)
    } else await hc.call('terminal.kill', { terminal })
    mobile.detach(); desktop.detach(); observer.detach()
  }
  console.log('PASS: paused output is bounded; width, independent events, RPC, other viewers and resume survive')
} finally {
  socket?.destroy()
  try { cli('server', 'stop') } finally { rmSync(root, { recursive: true, force: true }) }
}
