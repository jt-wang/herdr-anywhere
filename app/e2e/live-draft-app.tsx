// The steps of e2e/live-draft.test.ts: the whole app, paired with a real companion, sending into a terminal where
// someone has half typed a line (herdr-core holds a send until that line is sent or cleared). A second screen plays
// the person at the computer; each of its steps adds a line under the app, which the test reads.
import { mount, signal, Text, View } from 'nsh'
import { Linker, parsePairLink } from '../src/link.ts'
import { PrefStore } from '../src/prefs.ts'
import { Store } from '../src/store.ts'
import { Root } from '../src/ui/root.tsx'
import { COMPANION, PAIR } from './pair.ts'

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

// Lines the shell printed that are exactly `word`: what `echo word` gives once it ran.
const printed = (out: string, word: string) => out.split(/\r?\n/).filter((l) => l.replace(/\x1b\[[0-9;?]*[A-Za-z]/g, '').trim() === word).length

async function run() {
  if (!parsePairLink(PAIR)) throw new Error('no pairing link')
  await until(() => phone?.conn.get() === 'ready', 'the phone to pair and connect')
  const other = new Linker({ load: async () => null, save: () => {}, remove: () => {} })
  other.set(linker.link.get()!)
  const desk = new Store({ pipe: () => other.pipe() })
  await desk.connect()
  const r = await desk.newSpace({ name: 'draft', argv: ['sh'] })
  let out = ''
  const dec = new TextDecoder()
  const stream = await desk.attach(r.terminal, { cols: 80, rows: 24 }, { data: (b) => { out += dec.decode(b) }, close: () => {} })
  await sleep(1500)
  // Half a line, as someone at the computer leaves it.
  stream.write('echo hal')
  await until(() => out.includes('echo hal'), 'the half-typed line')
  say(`space: ${r.space}`)
  await until(() => phone!.sending.get().some((s) => s.held), 'the send to be held', 30000)
  say(`held: ${phone!.sending.get().map((s) => s.held).join()}`)
  await sleep(3000)
  stream.write('\x15') // ctrl+u: the line cleared
  await until(() => !phone!.sending.get().length, 'the send to arrive')
  await sleep(2000)
  say(`arrived: ${printed(out, 'phone-1')}`)

  // Again, and the phone's line drops while herdr-core holds it: the companion's link for the phone ends.
  stream.write('echo hal')
  await until(() => phone!.sending.get().some((s) => s.held && s.text === 'echo phone-2'), 'the second send to be held', 60000)
  const before = (await desk.exec(`pgrep -P ${COMPANION}`)).stdout.trim().split('\n')
  await desk.exec(`kill -KILL $(pgrep -o -P ${COMPANION})`)
  await until(() => phone!.sending.get().some((s) => s.failed) || !phone!.sending.get().length, 'the phone to hear the line went')
  say(`links before: ${before.length}, desk: ${desk.conn.get()}`)
  await until(() => phone!.toasts.get().length > 0, 'what the phone says about it', 5000)
  say(`phone says: ${phone!.toasts.get().map((t) => [t.title, t.body].filter(Boolean).join(' / ')).join(' // ')}`)
  await sleep(1000)
  stream.write('\x15')
  await sleep(4000)
  say(`after the drop: ${printed(out, 'phone-2')} arrived, phone ${phone!.conn.get()}`)
  // Sent again from the box, and this time nobody touches the half line: herdr-core waits out its draft wait (60 s by
  // default) and adds the text after it. The phone keeps the line all that time, and waits for it.
  stream.write('echo hal')
  await sleep(1000)
  say('typed again')
  const line: string[] = []
  const off = phone!.conn.sub((c) => line.push(c))
  await until(() => phone!.sending.get().some((s) => !s.failed && s.text === 'echo phone-2'), 'the send again', 30000)
  const t0 = Date.now()
  await until(() => !phone!.sending.get().length, 'the long send to end', 100000)
  await sleep(2000)
  off()
  say(`long: ${Math.round((Date.now() - t0) / 1000)} s, line ${line.join('>') || 'steady'}, printed ${printed(out, 'halecho phone-2')}`)
  desk.closeSpace(r.space)
  desk.disconnect()
}

run().then(() => say('done'), (e) => say(`FAIL ${e?.message ?? e}`))
mount(() => (
  <View class="flex-1">
    <Root deps={{ linker, prefs: new PrefStore(io), device: 'e2e phone', pairLink: PAIR, makeStore: (l) => (phone = new Store({ pipe: () => l.pipe() })) }} />
    <View testID="live-steps"><Each of={lines()}>{(t) => <Text>{t()}</Text>}</Each></View>
  </View>
))
