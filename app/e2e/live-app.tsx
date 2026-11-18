// The steps of e2e/live.test.ts, run inside an app so they get the live network (a test file's own fetch has none).
// Each step adds a line; the test reads them.
import { mount, signal, Text, View } from 'nsh'
import { Linker, pairWith, parsePairLink } from '../src/link.ts'
import { Store } from '../src/store.ts'
import { COMPANION, PAIR } from './pair.ts'

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
const storeFor = (l: Linker) => new Store({ pipe: () => l.pipe() })

async function run() {
  const p = parsePairLink(PAIR)
  if (!p) throw new Error('no pairing link')
  const link = await pairWith(p, 'e2e phone')
  say(`paired: token ${link.token.length > 20 ? 'long' : 'short'}`)
  say(`second use: ${await pairWith(p, 'e2e phone').then(() => 'paired', (e) => e.message)}`)

  const l = linker()
  l.set(link)
  const store = storeFor(l)
  await store.connect()
  say(`connected: ${store.conn.get()} generation ${store.hello!.generation}`)
  const r = await store.newSpace({ name: 'e2e', argv: ['sh', '-c', 'echo hm-e2e-$((6*7)); exec sh'] })
  let out = ''
  const dec = new TextDecoder()
  const stream = await store.attach(r.terminal, { cols: 80, rows: 24 }, { data: (b) => { out += dec.decode(b) }, close: () => {} })
  await until(() => out.includes('hm-e2e-42'), 'the command output').catch((e) => { throw new Error(`${e.message}; got ${JSON.stringify(out.slice(-300))}; term ${JSON.stringify(store.terms.get(r.terminal))}`) })
  say('ran: hm-e2e-42')
  stream.write('echo typed-$((40+2))\r')
  await until(() => out.includes('typed-42'), 'the typed command output')
  say('typed: typed-42')
  // Far past what the socket carries (one message: 8 MiB), over HTTP through the real companion, and the line stays up.
  const body = new Uint8Array(30_000_000).fill(7)
  const up = await fetch(`${await l.baseUrl()}/api/files?name=${encodeURIComponent('report 1.pdf')}`, { method: 'POST', body, headers: { authorization: `Bearer ${l.link.get()!.token}` } })
  const landed = (await up.json()).path as string
  const seen = await store.exec(`wc -c < '${landed}'`)
  say(`upload: ${up.status} ${landed.split('/').pop()} ${seen.stdout.trim()}, line ${store.conn.get()}`)
  // The line goes quiet and stays open, as when the close never reaches the phone: only this test companion's own
  // `hc connect` stops. The phone notices by itself and connects again.
  void store.exec(`kill -STOP $(pgrep -P ${COMPANION})`).catch(() => {}) // its answer can't come back: that is the point
  await until(() => store.conn.get() === 'reconnecting', 'the quiet line noticed', 55000)
  await until(() => store.conn.get() === 'ready', 'connected again', 25000)
  say(`quiet line: ${store.conn.get()}`)
  const other = storeFor(l)
  await other.connect()
  await until(() => other.model.spaces.some((s) => s.name === 'e2e'), 'the space on another screen')
  say('other screen: e2e')
  // Pins live on herdr-core as the label herdr.pinned, which the desktop reads and writes too. A made-up `claude`
  // (e2e/fake-claude, which run.sh builds into $HOME/bin) is an agent to pin. It is a program of its own: `sleep`
  // linked under that name exits at once on Ubuntu 26.04, whose coreutils refuse to run under another name.
  const ag = await store.newSpace({ name: 'pins', argv: [`${store.home}/bin/claude`, '600'] })
  const agentOn = (s: Store) => s.agents().find((a) => a.address === ag.terminal)
  const why = (s: Store) => {
    const t = s.terms.get(ag.terminal)
    return `pane ${s.model.paneOf(ag.terminal) ?? '-'} state ${t?.state ?? '-'} program ${s.program(ag.terminal).key} argv ${JSON.stringify(t?.foreground?.argv ?? t?.argv)}`
  }
  // Until herdr-core reports what runs in front, the app knows the agent by the argv it started: the stand-in must
  // still be running then, or the agent turns into a shell partway through the steps below.
  await until(() => !!store.terms.get(ag.terminal)?.foreground, 'the program in front of the made-up agent')
  await until(() => !!agentOn(store) && !!agentOn(other), 'the made-up agent on both screens').catch((e) => {
    throw new Error(`${e.message}; phone ${agentOn(store) ? 'has it' : why(store)}; other ${agentOn(other) ? 'has it' : why(other)}`)
  })
  say(`agent: pinned ${agentOn(store)!.pinned}`)
  await store.pin(ag.terminal, true)
  await until(() => !!agentOn(other)?.pinned, 'the pin on the other screen')
  const labels = () => other.terms.get(ag.terminal)!.labels
  say(`phone pinned: other screen ${agentOn(other)!.pinned}, label ${labels()['herdr.pinned']}, space kept ${labels()['herdr.space'] === ag.space}`)
  await (other as any).call('terminal.label', { terminal: ag.terminal, labels: { 'herdr.pinned': '' } })
  await until(() => agentOn(store)?.pinned === false, 'the unpin on the phone')
  say(`unpinned elsewhere: phone ${agentOn(store)!.pinned}, label ${'herdr.pinned' in store.terms.get(ag.terminal)!.labels ? 'kept' : 'gone'}`)
  store.closeSpace(ag.space)
  await store.exec('mkdir -p "$HOME/proj" && cd "$HOME/proj" && git init -q && git checkout -qb feature/x && git -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m init')
  const g = await store.newSpace({ folder: `${store.home}/proj` })
  await until(() => !!store.model.folderInfo.get(g.terminal)?.branch, 'the git branch', 8000).catch((e) => {
    throw new Error(`${e.message}; term ${JSON.stringify(store.terms.get(g.terminal)?.foreground)} pane ${store.model.paneOf(g.terminal)}`)
  })
  say(`git: ${store.model.folderInfo.get(g.terminal)!.branch}`)
  // A terminal started outside the app, in a space of its own: adopted, and its branch shows too.
  const ext = await (store as any).call('terminal.run', { argv: [], cwd: `${store.home}/proj`, keep: true, labels: { 'herdr.space': 'spext', 'herdr.tab': 'tbext' } })
  await until(() => !!store.model.paneOf(ext.address), 'adoption')
  await until(() => !!store.model.folderInfo.get(ext.address)?.branch, 'the adopted branch', 8000).catch((e) => {
    throw new Error(`${e.message}; term ${JSON.stringify(store.terms.get(ext.address)?.foreground)} cwd ${store.terms.get(ext.address)?.cwd}`)
  })
  say(`adopted git: ${store.model.folderInfo.get(ext.address)!.branch}`)
  // The same terminal seen by a screen that connects after it started (the demo's case).
  const late = storeFor(l)
  await late.connect()
  await until(() => !!late.model.folderInfo.get(ext.address)?.branch, 'the branch on a late screen', 8000).catch((e) => {
    throw new Error(`${e.message}; pane ${late.model.paneOf(ext.address)} term ${JSON.stringify(late.terms.get(ext.address)?.foreground)}`)
  })
  say(`late git: ${late.model.folderInfo.get(ext.address)!.branch}`)
  late.disconnect()
  store.closeSpace('spext')
  store.closeSpace(g.space)
  store.closeSpace(r.space)
  await until(() => store.terms.get(r.terminal)?.state !== 'live', 'the terminal to end')
  say(`closed: ${store.terms.get(r.terminal)?.state}`)
  store.disconnect()
  other.disconnect()

  const bad = linker()
  bad.set({ ...link, token: 'not-a-token' })
  const s3 = storeFor(bad)
  await s3.connect().catch(() => {})
  await until(() => s3.conn.get() === 'unpaired', 'unpaired')
  say(`bad token: ${s3.error.get()}`)
}

run().then(() => say('done'), (e) => say(`FAIL ${e?.message ?? e}`))
mount(() => <View class="flex-1"><Each of={lines()}>{(t) => <Text>{t()}</Text>}</Each></View>)
