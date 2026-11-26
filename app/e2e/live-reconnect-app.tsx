// The steps of e2e/live-reconnect.test.ts, run inside an app so they get the live network: pairing refused for a
// wrong and for an expired code, the line coming back by itself with the same pairing when the companion stops and
// starts again (e2e/run.sh restarts it on the same port and state), and a phone the computer revoked pairing again.
// Each step adds a line; the test reads them.
import { mount, signal, Text, View } from 'nsh'
import { Linker, pairWith, parsePairLink } from '../src/link.ts'
import { Store } from '../src/store.ts'
import { t as tr } from '../src/strings/index.ts'
import { PAIR } from './pair.ts'

const lines = signal<string[]>([])
const say = (s: string) => lines.update((l) => [...l, s])
const until = async (cond: () => boolean, what: string, ms = 10000) => {
  const end = Date.now() + ms
  while (!cond()) {
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}`)
    await new Promise((r) => setTimeout(r, 50))
  }
}
const memory = new Map<string, string>()
const linker = () => new Linker({ load: async (k) => memory.get(k) ?? null, save: (k, v) => memory.set(k, v), remove: (k) => memory.delete(k) })
const refused = (p: { url: string; code: string }) => pairWith(p, 'e2e phone').then(() => 'paired', (e) => e.message)

async function run() {
  const p = parsePairLink(PAIR)
  if (!p) throw new Error('no pairing link')
  const wrong = { ...p, code: p.code.slice(0, -1) + (p.code.endsWith('A') ? 'B' : 'A') }
  say(`wrong code: ${await refused(wrong)}`)
  const link = await pairWith(p, 'e2e phone')
  const l = linker()
  l.set(link)
  const store = new Store({ pipe: () => l.pipe() })
  await store.connect()
  say(`connected: ${store.conn.get()}`)
  // A code the computer gives now for a second, used after it ran out; and one for pairing again further down.
  const code = async (ttl: string) => {
    const out = (await store.exec(`"$HM_LIVE_BIN" pair --url "$HM_LIVE_URL" --ttl ${ttl}`)).stdout
    const q = parsePairLink(/http:\/\/\S*#pair=[A-Za-z0-9_-]+/.exec(out)?.[0] ?? '')
    if (!q) throw new Error(`pair printed no link: ${JSON.stringify(out.slice(-300))}`)
    return q
  }
  const brief = await code('1s')
  await new Promise((r) => setTimeout(r, 2000))
  say(`expired code: ${await refused(brief)}`)
  const again = await code('10m')

  const r = await store.newSpace({ name: 'restart', argv: ['sh', '-c', 'echo before-$((6*7)); exec sh'] })
  let out = ''
  const dec = new TextDecoder()
  await store.attach(r.terminal, { cols: 80, rows: 24 }, { data: (b) => { out += dec.decode(b) }, close: () => {} })
  await until(() => out.includes('before-42'), 'the command output')

  // The companion stops for 6 s: the line goes down, and the tries while it is away are refused, which the banner
  // says as the companion not running. It comes back on the same port: the phone connects again with the pairing it
  // has, and the terminal it had still answers.
  const why = new Set<string>()
  const off = store.lostWhy.sub((w) => { if (w) why.add(w) })
  const errors = new Set<string>()
  const offError = store.error.sub((e) => { if (e) errors.add(e) })
  await store.exec(`echo 6 > "$HM_LIVE_CTL/restart"`)
  await until(() => store.conn.get() === 'reconnecting', 'the line to go down')
  await until(() => store.conn.get() === 'ready', 'connected again', 30000)
  off(); offError()
  say(`while away: ${why.has('refused') ? 'refused' : [...why].join(',')}, said ${errors.has(tr('lostRefused'))}`)
  say(`back: ${store.conn.get()}, same pairing ${l.link.get()?.token === link.token}`)
  let after = ''
  const s2 = await store.attach(r.terminal, { cols: 80, rows: 24 }, { data: (b) => { after += dec.decode(b) }, close: () => {} })
  s2.write('echo after-$((40+2))\r')
  await until(() => after.includes('after-42'), 'the typed command after the restart')
  say('after restart: after-42')

  // The computer revokes this phone: the line ends as unpaired, and a new code pairs it again. The old token is no
  // longer taken; the computer's terminals are still there for the new pairing.
  void store.exec(`"$HM_LIVE_BIN" revoke ${link.device}`).catch(() => {}) // the line it came on ends
  await until(() => store.conn.get() === 'unpaired', 'unpaired after revoke', 10000)
  say(`revoked: ${store.conn.get()}, ${store.error.get()}`)
  const fresh = await pairWith(again, 'e2e phone')
  l.set(fresh)
  await store.connect()
  await until(() => store.model.spaces.some((s) => s.name === 'restart'), 'the space after pairing again')
  say(`paired again: ${store.conn.get()}, new token ${fresh.token !== link.token}, space kept`)
  const old = linker()
  old.set(link)
  const stale = new Store({ pipe: () => old.pipe() })
  await stale.connect().catch(() => {})
  await until(() => stale.conn.get() === 'unpaired', 'the old token refused')
  say(`old token: ${stale.conn.get()}`)
  store.closeSpace(r.space)
  store.disconnect()
}

run().then(() => say('done'), (e) => say(`FAIL ${e?.message ?? e}`))
mount(() => <View class="flex-1"><Each of={lines()}>{(t) => <Text>{t()}</Text>}</Each></View>)
