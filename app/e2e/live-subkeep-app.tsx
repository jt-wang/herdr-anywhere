// The steps of e2e/live-subkeep.test.ts: the whole app, paired with a real companion and herdr-core, with an agent
// (e2e/fake-claude) that has a sub-agent of its own. The test keeps the sub-agent as a tab from home and taps its row
// again; these steps say what herdr-core and the shared layout hold then, and what a phone that connects afresh shows.
import { mount, signal, Text, View } from 'nsh'
import { Linker, parsePairLink } from '../src/link.ts'
import { PrefStore } from '../src/prefs.ts'
import { Store } from '../src/store.ts'
import { Root } from '../src/ui/root.tsx'
import { terminalOf, panes } from '../src/model/layout.ts'
import { PAIR } from './pair.ts'

const lines = signal<string[]>([])
const say = (s: string) => lines.update((l) => [...l, s])
const until = async (cond: () => boolean, what: string, ms = 20000) => {
  const end = Date.now() + ms
  while (!cond()) {
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}`)
    await new Promise((r) => setTimeout(r, 50))
  }
}
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
const memory = new Map<string, string>()
const io = { load: async (k: string) => memory.get(k) ?? null, save: (k: string, v: string) => void memory.set(k, v), remove: (k: string) => void memory.delete(k) }
const linker = new Linker(io)
let phone: Store | undefined

/** How many tabs in the space show the terminal, by a store's model. */
const tabsWith = (s: Store, space: string, addr: string) =>
  s.model.space(space)?.tabs.filter((c) => panes(c.root).some((n) => terminalOf(n) === addr)).length ?? 0

async function run() {
  if (!parsePairLink(PAIR)) throw new Error('no pairing link')
  await until(() => phone?.conn.get() === 'ready', 'the phone to pair and connect')
  const other = () => { const l = new Linker({ load: async () => null, save: () => {}, remove: () => {} }); l.set(linker.link.get()!); return l }
  const desk = new Store({ pipe: () => other().pipe() })
  await desk.connect()
  const r = await desk.newSpace({ name: 'kids', argv: [`${desk.home}/bin/claude`, '600'] })
  const kid = await desk.call<any>('terminal.run', { argv: ['sh', '-c', 'echo helper; sleep 600'], parent: r.terminal })
  if (kid.parent !== r.terminal) throw new Error(`the sub-agent's parent is ${kid.parent}`)
  say(`kid: ${kid.address}`)
  await until(() => tabsWith(phone!, r.space, kid.address) >= 1, 'the kid kept as a tab', 60000)
  await until(() => lines().includes('tapped again'), 'the test to tap the row again and Keep if it can', 60000)
  await sleep(2000)
  const doc = (await desk.call<any>('doc.get', { name: 'herdr-app' })).value
  const inDoc = doc.spaces.find((s: any) => s.id === r.space).tabs.filter((c: any) => JSON.stringify(c.root).includes(`"terminal":"${kid.address}"`)).length
  const labels = (await desk.call<any>('terminal.get', { terminal: kid.address })).labels
  say(`phone tabs: ${tabsWith(phone!, r.space, kid.address)}, layout tabs: ${inDoc}, label tab is one of them: ${doc.spaces.find((s: any) => s.id === r.space).tabs.some((c: any) => c.id === labels['herdr.tab'])}`)
  const fresh = new Store({ pipe: () => other().pipe() })
  await fresh.connect()
  await sleep(2000)
  say(`fresh phone tabs: ${tabsWith(fresh, r.space, kid.address)}, running: ${(await desk.call<any>('terminal.get', { terminal: kid.address })).state}`)
  fresh.disconnect()
  desk.disconnect()
  // A layout an earlier version left with the sub-agent in two more tabs, written behind the phones' backs: the phones
  // connected put it back to one, and the sub-agent keeps running.
  const raw = new Store({ pipe: () => other().pipe() })
  await raw.connect()
  const d = await raw.call<any>('doc.get', { name: 'herdr-app' })
  const sp = d.value.spaces.find((s: any) => s.id === r.space)
  const mine = sp.tabs.find((c: any) => JSON.stringify(c.root).includes(`"terminal":"${kid.address}"`))
  sp.tabs.push({ ...mine, id: 'tbcopy1', root: { id: 'pcopy1', type: 'block', content: { terminal: kid.address } } },
    { ...mine, id: 'tbcopy2', root: { id: 'pcopy2', type: 'block', content: { terminal: kid.address } } })
  const n = sp.tabs.filter((c: any) => JSON.stringify(c.root).includes(`"terminal":"${kid.address}"`)).length
  const set = await raw.call<any>('doc.set', { name: 'herdr-app', version: d.version, value: d.value })
  say(`copies written: ${n}, version ${set.version > d.version}`)
  await sleep(3000)
  const after = (await raw.call<any>('doc.get', { name: 'herdr-app' })).value.spaces.find((s: any) => s.id === r.space)
  say(`copies left: phone ${tabsWith(raw, r.space, kid.address)}, layout ${after.tabs.filter((c: any) => JSON.stringify(c.root).includes(`"terminal":"${kid.address}"`)).length}, running: ${(await raw.call<any>('terminal.get', { terminal: kid.address })).state}`)
  raw.closeSpace(r.space)
  await sleep(500)
  raw.disconnect()
}

run().then(() => say('done'), (e) => say(`FAIL ${e?.message ?? e}`))
mount(() => (
  <View class="flex-1">
    <Root deps={{ linker, prefs: new PrefStore(io), device: 'e2e phone', pairLink: PAIR, makeStore: (l) => (phone = new Store({ pipe: () => l.pipe() })) }} />
    <View role="button" testID="live-next" onPress={() => say('tapped again')}><Text>next</Text></View>
    <View testID="live-steps"><Each of={lines()}>{(t) => <Text>{t()}</Text>}</Each></View>
  </View>
))
