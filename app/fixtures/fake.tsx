// The app over an in-memory herdr-core (src/testing/fakecore.ts), already paired, for the screen tests.
// `host.params.slowSendMs`: how long a send goes unanswered before the input box says why (5 s when not set).
// `host.params.scene`: "empty" (a fresh computer), "busy" (two spaces, agents in every state), "twins" (two Claude
// Codes in one space), "unpaired", "companion-off" (with via: "tailpocket": nothing answers through the tunnel), "companion-refused" (the computer at
// `url` refuses at once),
// "link" (never paired, opened with the `#pair=` link in `host.params.link`, as the web app is), "link-forgotten" (the
// same, and the computer forgets the phone right after), "relaunch-forgotten" (paired before, opened again with that
// link, as e2e/ios-tailcat.sh's build opens every launch, and the computer forgets the phone right after).
// For Settings › Computer: `via: "tailpocket"` with `routes` ("relay:tok,direct": what the tunnel answers in turn, the
// last one for good; "throws": it fails), `url` (the link's address), `conn: "lost"` (the core stops answering) and
// `info: "none"` (the companion gives no version).
import { host, Keyboard, mount, Storage } from 'nsh'
import { Linker, type Route, type Tunnel } from '../src/link.ts'
import { PrefStore } from '../src/prefs.ts'
import { Store } from '../src/store.ts'
import { rowKeys } from '../src/ui/pane-bar.tsx'
import { Root } from '../src/ui/root.tsx'
import { CLAUDE_EDIT_MENU, FakeCore, type FakeTab } from '../src/testing/fakecore.ts'
import { FakeFolders } from '../src/testing/fake-folders.ts'
import { AGENTS, progressTree, saidOn, waitOn } from './progress-tree.ts'
import { SAMPLE_PICTURE } from './picture.ts'

// Recorded where a test can read it, as pairLinkUsed below: the test's own Keyboard is another bundle's.
const putAway = Keyboard.dismiss
let keyboardAway = 0
let checkPosts = 0
let said: { checks?: string; autoUpdate?: boolean } = {} // what the phone told the computer, as the computer would answer it
Keyboard.dismiss = () => { void Storage.setItem('keyboard-away', String(++keyboardAway)); putAway() }

const scene = String((host.params as any)?.scene ?? 'busy')
const opened = scene.startsWith('link')
const params = (host.params ?? {}) as Record<string, string | undefined>
const paired = params.via === 'tailpocket' ? { url: 'tailpocket:', via: 'tailpocket' } : { url: params.url ?? 'http://192.0.2.10:7681' }
const memory = new Map<string, string>(opened ? [] : [['link', JSON.stringify({ ...paired, token: 'synthetic-token', device: 'd1', name: 'studio', enc: 'bin' })]])
// `opened`: how many times the app opened before (the question about checking for new versions waits for the second).
// `prefs`: settings saved before, as JSON (`{"showAgentNames":false}`).
if (params.opened || params.prefs) memory.set('prefs', JSON.stringify({ ...(params.prefs ? JSON.parse(params.prefs) : {}), ...(params.opened ? { opened: Number(params.opened) } : {}) }))
// "progress": the lab panes with the progress page first, its tree answered by fixtures/progress-tree.ts.
if (scene === 'progress') memory.set('prefs', JSON.stringify({ opened: 2, progressFirst: true, ...(params.appearance ? { appearance: params.appearance } : {}), ...(params.prefs ? JSON.parse(params.prefs) : {}) }))
// Saved settings, where a test reads them (`app.storage()['fake-prefs']`).
const io = { load: async (k: string) => memory.get(k) ?? null, save: (k: string, v: string) => { memory.set(k, v); if (k === 'prefs') void Storage.setItem('fake-prefs', v) }, remove: (k: string) => void memory.delete(k) }

export const core = new FakeCore()
core.echo = true
;(globalThis as any).__core = core
const EDIT_MENU = CLAUDE_EDIT_MENU
if (scene === 'busy' || scene === 'single') {
  const doc = { spaces: [] as any[] }
  const add = (space: string, folder: string, name: string, tabs: FakeTab[]) =>
    core.addSpace(doc, space, folder, name, tabs.map((t) => ({ ...t, foreground: scene === 'single' })))
  // `scene: "single"`: one terminal running `params.argv`, silent unless `params.status` says otherwise.
  if (scene === 'single') add('sp1', '/Users/sam/src/api', '', [{ id: 'tb1', argv: [params.argv ?? 'zsh'], status: params.status }])
  else add('sp1', '/Users/sam/src/api', '', [
    { id: 'tb1', name: 'fix login', argv: ['claude'], status: 'blocked', kind: 'permission', msg: 'approve Write: /Users/sam/src/api/login.ts', screen: EDIT_MENU },
    { id: 'tb2', argv: ['codex'], status: 'working' },
  ])
  if (scene === 'busy') add('sp2', '/Users/sam/src/clients/northwind/storefront-redesign-2026/web', 'web', [{ id: 'tb3', argv: ['zsh'] }])
  core.docs.set('', { value: doc, version: 1 })
}
// `remote: "only"`: the spaces are on a server herdr-core added with `hc machine add my-server`, none on this computer;
// `remote: "both"`: the same server, and one more space on this computer.
if (params.remote) {
  core.machines = [{ name: 'my-server', host: 'my-server', state: 'connected' }]
  core.docs.set('my-server', core.docs.get('') ?? { value: { spaces: [] }, version: 1 })
  // herdr-core lists a machine's terminals as `my-server/t1`; that machine's own document says `t1`.
  for (const [a, t] of [...core.terms]) {
    core.terms.delete(a)
    t.address = `my-server/${a}`
    core.terms.set(t.address, t)
    const screen = core.screens.get(a)
    if (screen !== undefined) { core.screens.delete(a); core.screens.set(t.address, screen) }
  }
  const here = { spaces: [] as any[] }
  if (params.remote === 'both') core.addSpace(here, 'sp9', '/Users/sam/notes', 'notes', [{ id: 'tb9', argv: ['zsh'] }])
  core.docs.set('', { value: here, version: 1 })
}
// `scene: "many"`: 24 Claude Codes at work in four spaces, as a busy computer has them.
if (scene === 'many') {
  const doc = { spaces: [] as any[] }
  for (const [i, name] of ['api', 'web', 'docs', 'infra'].entries())
    core.addSpace(doc, `sp${i + 1}`, `/Users/sam/src/${name}`, '', Array.from({ length: 6 }, (_, k) => ({ id: `tb${i * 6 + k + 1}`, name: `${name} ${k + 1}`, argv: ['claude'], status: 'working' })))
  core.docs.set('', { value: doc, version: 1 })
}
// `scene: "lab"`: four agents' panes at one moment, one of each state, to set beside `herdr-anywhere track status`.
if (scene === 'lab') {
  const doc = { spaces: [] as any[] }
  core.addSpace(doc, 'sp1', '/Users/sam/acme-api', '', [{ id: 'tb1', name: 'api', argv: ['claude'], status: 'done', msg: 'cancel_order 加好了，6 个测试全过' }])
  core.addSpace(doc, 'sp2', '/Users/sam/acme-launch', '', [{ id: 'tb2', name: 'launch', argv: ['claude'], status: 'done', msg: '草稿在 posts/x-acme-1.0-draft.md' }])
  core.addSpace(doc, 'sp3', '/Users/sam/acme-docs', '', [{ id: 'tb3', name: 'docs', argv: ['claude'], status: 'blocked', kind: 'permission', msg: 'approve Bash: git commit -m "FAQ"' }])
  core.addSpace(doc, 'sp4', '/Users/sam/acme-shop', '', [{ id: 'tb4', name: 'shop', argv: ['claude'], status: 'idle' }])
  core.docs.set('', { value: doc, version: 1 })
}
// "progress": the sample scene's panes (fixtures/progress-tree.ts) at t1…t4.
if (scene === 'progress') {
  const doc = { spaces: [] as any[] }
  core.addSpace(doc, 'sp1', '/Users/sam/acme-api', '', [{ id: 'tb1', name: 'api', argv: ['claude'], status: 'idle' }])
  core.addSpace(doc, 'sp2', '/Users/sam/acme-shop', '', [{ id: 'tb2', name: 'checkout', argv: ['claude'], status: 'working' },
    { id: 'tb3', name: 'payments', argv: ['claude'], status: 'done', msg: '对比写好了，等你看' }])
  core.addSpace(doc, 'sp3', '/Users/sam/acme-app', '', [{ id: 'tb4', name: 'mobile', argv: ['claude'], status: 'idle' }])
  core.docs.set('', { value: doc, version: 1 })
  const tree = progressTree()
  const ran: string[] = []
  core.onExec = (line) => {
    if (!line.includes('herdr-anywhere')) return undefined
    ran.push(line.slice(line.indexOf('b=herdr-anywhere; "$b" ') + 23)) // after the first run of the companion
    void Storage.setItem('fake-exec', JSON.stringify(ran))
    if (line.includes(`track 'tree' '--json'`)) return { stdout: JSON.stringify(tree) }
    if (line.includes(`progress 'agents'`)) return { stdout: JSON.stringify(AGENTS) }
    return { stdout: '' }
  }
  // A test moves a task to 等你 (`{ wait: 'T4', waiting: '…' }`), leaves messages to sort (`{ queue: 2 }`), or has
  // the filing pane stop before it starts (`{ organizerWaits: 't2' }`).
  host.on('fake.progress', (d: any) => (d.organizerWaits ? Object.assign(tree, { queue: 1, organizer_waits: { addr: d.organizerWaits } })
    : d.queue ? Object.assign(tree, { queue: d.queue }) : d.said ? saidOn(tree, d.said.id, d.said.text)
    // `only`: the page with that one goal left.
    : d.only ? Object.assign(tree, { tasks: tree.tasks.filter((t) => t.id === d.only) }) : waitOn(tree, d.wait, d.waiting)))
}
// `scene: "twins"`: one space with two Claude Codes, told apart by a tab's name and an agent's title, one with a
// sub-agent, and a shell; another space with one more Claude Code.
if (scene === 'twins') {
  const doc = { spaces: [] as any[] }
  core.addSpace(doc, 'sp1', '/Users/sam/src/api', '', [
    { id: 'tb1', name: 'fix login', argv: ['claude'], status: 'working', msg: 'Running the login tests' },
    { id: 'tb2', argv: ['claude'], status: 'done', title: 'Billing refactor', msg: 'Moved invoices to the new table' },
    { id: 'tb3', argv: ['zsh'] },
  ])
  core.addSpace(doc, 'sp2', '/Users/sam/src/web', 'web', [{ id: 'tb4', argv: ['claude'], status: 'working', title: 'Checkout page', msg: 'Editing cart.tsx' }])
  core.run({ argv: ['claude'], parent: 't1', name: 'test runner' })
  core.docs.set('', { value: doc, version: 1 })
}
// `scene: "site"`: a made-up computer for the website's phone pictures (e2e/site-app.tsx): Claude
// Code at work in acme-api with a Pi helper, Kimi asking to trust billing, Pi done in worker and at work in web-app on
// a line typed from the phone (`params.typed`, in the phone's language), its colors for `params.scheme`.
if (scene === 'site') {
  const doc = { spaces: [] as any[] }
  const src = `${core.home}/src`
  core.git.set(`${src}/acme-api`, '# branch.oid 1\n# branch.head feature/login\n')
  core.git.set(`${src}/web-app`, '# branch.oid 1\n# branch.head main\n')
  core.addSpace(doc, 'sp1', `${src}/acme-api`, '', [{ id: 'tb1', argv: ['claude'], status: 'working' }])
  core.addSpace(doc, 'sp2', `${src}/billing`, '', [{ id: 'tb2', argv: ['kimi'] }])
  core.addSpace(doc, 'sp3', `${src}/worker`, '', [{ id: 'tb3', argv: ['pi'] }])
  core.addSpace(doc, 'sp4', `${src}/web-app`, '', [{ id: 'tb4', argv: ['pi'] }])
  const helper = core.run({ argv: ['pi'], parent: 't1', name: 'review', cwd: `${src}/acme-api` })
  core.setStatus(helper.address, 'working', 'pi')
  core.setStatus('t2', 'blocked', 'kimi-code', { msg: 'Workspace trust required' })
  core.terms.get('t3')!.title = 'π - worker'
  // Done shows for an agent seen working that stopped since: so Pi works a moment first.
  core.setStatus('t3', 'working', 'pi')
  setTimeout(() => core.setStatus('t3', 'done', 'pi'), 800)
  core.terms.get('t4')!.title = 'π - web-app'
  core.setStatus('t4', 'working', 'pi')
  // Pi's screen as it draws it at work: its startup lines, the line sent from the phone, its status and footer.
  const dim = (s: string) => `\x1b[38;5;244m${s}\x1b[0m`, rule = (s: string) => `\x1b[38;2;137;117;232m${s}\x1b[0m`
  const W = 44, line = '─'.repeat(W), light = params.scheme === 'light'
  const fill = light ? '48;2;221;232;240' : '48;2;31;58;74'
  // Pi's box is as wide as its rules; a CJK character takes two columns.
  const cols = (s: string) => [...s].reduce((n, c) => n + (/[\u1100-\u115f\u2e80-\ua4cf\uac00-\ud7a3\uf900-\ufaff\ufe30-\ufe4f\uff00-\uff60\uffe0-\uffe6]/.test(c) ? 2 : 1), 0)
  const box = (s: string) => `\x1b[${fill}m${s}${' '.repeat(Math.max(0, W - cols(s)))}\x1b[0m`
  core.screens.set('t4', [
    dim(' Press ctrl+o to show full startup help.'), '',
    dim(' Pi can explain its own features and look up its'), dim(' docs. Ask it how to use or extend Pi.'), '',
    box(''), box(` ${params.typed ?? 'Add a logout test'}`).replace('m ', 'm\x1b[1m '), box(''), '',
    rule(`── ⠋ \x1b[1mWorking\x1b[22m ${'─'.repeat(W - 13)}`), '\x1b[7m \x1b[27m', rule(line),
    dim('~/src/web-app (main)'), dim(`0.1%/1.0M (auto)${' '.repeat(W - 40)}deepseek-v4-flash • high`),
  ].join('\n'))
  core.docs.set('', { value: doc, version: 1 })
}
// Keys pressed in a terminal, where a test reads them (`app.storage()['fake-keys']`); an answered agent goes on working.
const pressed: [string, string[]][] = []
core.onKeys = (addr, keys) => {
  pressed.push([addr, keys])
  void Storage.setItem('fake-keys', JSON.stringify(pressed))
  const st = core.terms.get(addr)?.status
  if (st?.state === 'blocked') core.setStatus(addr, 'working', st.app)
}
// A test moves an agent to another state: `app.host.send('fake.status', { address, state })`.
host.on('fake.status', (d: any) => {
  if (typeof d.screen === 'string') core.screens.set(d.address, d.screen)
  core.setStatus(d.address, d.state, core.terms.get(d.address)?.status?.app, { kind: d.kind, msg: d.msg })
})
// Another Herdr screen pins or unpins an agent: `app.host.send('fake.label', { address, labels: { 'herdr.pinned': '1' } })`.
host.on('fake.label', (d: any) => core.label(d.address, d.labels))
// A test drops the line: `app.host.send('fake.drop', {})`; the computer then stops answering.
host.on('fake.drop', () => core.drop('network'))
// herdr-core holds a method's answers until released, as it holds terminal.send for a half-typed line:
// `app.host.send('fake.hold', { method })`, then `app.host.send('fake.release', {})`.
host.on('fake.hold', (d: any) => core.holding.add(d.method))
host.on('fake.release', () => { core.holding.clear(); core.release() })
// Text the app sent (`terminal.send`), where a test reads it (`app.storage()['fake-sent']`).
const sent: [string, string, boolean][] = []
core.onSend = (addr, text, force) => { sent.push([addr, text, force]); void Storage.setItem('fake-sent', JSON.stringify(sent)) }
// What came to POST /api/files: its name, its machine, its size.
const put: [string, string, number][] = []
const landed = (q: URLSearchParams, size: number) => {
  put.push([q.get('name') ?? '', q.get('on') ?? '', size])
  void Storage.setItem('fake-put', JSON.stringify(put))
  return `/fake/uploads/${q.get('name')}`
}
// The computer's folders for the folder picker (GET/POST /api/folders); a folder made is recorded in `fake-created`.
const disk = new FakeFolders(core.home, ['src/api', 'src/demo-app', 'src/web', 'src/clients/northwind/storefront-redesign-2026/web', 'Documents', 'notes', '.config'],
  ['src/api', 'src/demo-app', 'src/clients/northwind/storefront-redesign-2026/web'])
const answer = (status: number, body: unknown) => ({ ok: status < 300, status, json: async () => body })
async function folders(url: string, init: any) {
  if (init?.method === 'POST') {
    const path = await disk.create(JSON.parse(init.body).path)
    void Storage.setItem('fake-created', JSON.stringify(disk.created))
    return answer(200, { path, created: true })
  }
  const q = url.split('?')[1] ?? ''
  const r = await disk.list(decodeURIComponent(/(?:^|&)path=([^&]*)/.exec(q)?.[1] ?? ''), /(?:^|&)hidden=1/.test(q))
  return 'missing' in r ? answer(404, { error: 'no such folder', ...r }) : answer(200, r)
}
// GET /api/image as the computer answers it: notes.png isn't a picture, gone.png isn't there, any other is the sample
// screenshot; a relative path is in t1's folder. Each ask is written where a test reads it (`fake-pictures`).
const pictureAsks: string[] = []
function picture(url: string) {
  const q = new URLSearchParams(url.split('?')[1] ?? '')
  const path = q.get('path') ?? '', terminal = q.get('terminal') ?? ''
  pictureAsks.push(`${terminal} ${path}`)
  void Storage.setItem('fake-pictures', JSON.stringify(pictureAsks))
  const real = path.startsWith('/') ? path : path.startsWith('~/') ? `/Users/sam/${path.slice(2)}` : `/Users/sam/src/api/${path}`
  if (/notes\.png$/.test(path)) return answer(415, { error: 'not a picture', why: 'not_image', path: real })
  if (/gone\.png$/.test(path)) return answer(404, { error: 'no such file', why: 'missing', path: real })
  return answer(200, { path: real, url: SAMPLE_PICTURE })
}
// `params.picture`: a path tapped in t1 once the app is up, opening its sheet.
const tapPicture = (store: Store) => { if (params.picture) setTimeout(() => store.picture.set({ path: params.picture!, terminal: 't1' }), 50); return store }
// "unpaired": the computer refuses the saved token; a token from pairing again gets through.
let forgot = 0
const pipe = (l: Linker) => () => {
  const forgotten = (scene === 'unpaired' && l.link.get()?.token === 'synthetic-token') || ((scene === 'link-forgotten' || scene === 'relaunch-forgotten') && !forgot++)
  if (forgotten) { const c = new FakeCore(); c.send = () => c.drop('unpaired'); return c }
  // "companion-off": through the tunnel nothing answers the saved link; a link from pairing again gets through.
  if (scene === 'companion-off' && l.link.get()?.token === 'synthetic-token') { const c = new FakeCore(); c.send = () => c.drop('no-answer'); return c }
  // "companion-refused": the computer at `url` refuses at once (nothing listens on the port).
  if (scene === 'companion-refused' && l.link.get()?.token === 'synthetic-token') { const c = new FakeCore(); c.send = () => c.drop('network'); return c }
  return core
}

const routes = (params.routes ?? 'direct').split(',')
let updatePosts = 0
const fakeTunnel: Tunnel = {
  pair: async () => JSON.stringify({ token: 'new-token', device: 'd2', name: 'studio' }), scan: async () => '', forget: () => {},
  connect: async () => ({
    baseUrl: 'http://127.0.0.1:1', close: () => {},
    path: async () => {
      const next = routes.length > 1 ? routes.shift()! : routes[0]
      if (next === 'throws') throw new Error('unreachable')
      const [route, relay] = next.split(':')
      return (route === 'relay' ? { route, relay } : { route }) as Route
    },
  }),
}
if (params.conn === 'lost') setTimeout(() => core.drop('network'), 0)

// How many times the key row's keys were drawn, where a test reads it (`app.storage()['fake-row-draws']`): each draw
// reads its key's label. A key drawn again while a finger is on it loses that press.
let rowDraws = 0
for (const k of rowKeys('keys')) {
  const label = k.label
  Object.defineProperty(k, 'label', { get: () => { void Storage.setItem('fake-row-draws', String(++rowDraws)); return label } })
}

// How many times the agents list was worked out, where a test reads it (`app.storage()['fake-views']`): the screens
// should share one per change, however many rows show it.
function counted(store: Store) {
  let views = 0
  const view = store.agentList.view.bind(store.agentList)
  store.agentList.view = (...a) => { void Storage.setItem('fake-views', String(++views)); return view(...a) }
  return store
}

mount(() => (
  <Root deps={{
    linker: new Linker(params.via === 'tailpocket' ? { ...io, tunnel: fakeTunnel } : io),
    opensSettings: params.open === 'settings',
    prefs: new PrefStore(io),
    makeStore: (l) => tapPicture(counted(new Store({ pipe: pipe(l), tunnelled: () => l.link.get()?.via === 'tailpocket', slowSendMs: params.slowSendMs ? Number(params.slowSendMs) : undefined }))),
    device: 'Test phone',
    tunnel: params.via === 'tailpocket' ? fakeTunnel : undefined,
    pairLink: opened || scene === 'relaunch-forgotten' ? String((host.params as any)?.link) : undefined,
    // Recorded where a test can read it: the app's own storage.
    pairLinkUsed: () => void Storage.setItem('pair-link-used', '1'),
    fetcher: (async (url: string, init: any) => {
      if (url.includes('/api/files')) return answer(200, { path: landed(new URLSearchParams(url.split('?')[1]), init.body.size) })
      if (url.includes('/api/folders')) return folders(url, init)
      // POST /api/image/check: the same files as picture(): notes.png isn't a picture, gone.png isn't there.
      if (url.includes('/api/image/check')) { const { paths } = JSON.parse(init.body); return answer(200, { paths: paths.map((p: string) => ({ asked: p, picture: !/(notes|gone)\.png$/.test(p), ...(/notes\.png$/.test(p) ? { why: 'not_image' } : /gone\.png$/.test(p) ? { why: 'missing' } : {}) })) }) }
      if (url.includes('/api/image')) return picture(url)
      if (url.includes('/api/update/auto')) { const on = JSON.parse(init.body).on; said = { ...said, autoUpdate: on, ...(on ? { checks: 'on' } : {}) }; void Storage.setItem('update-auto', String(init.body)); return answer(200, {}) }
      if (url.includes('/api/update/checks')) { const on = JSON.parse(init.body).on; said = { ...said, checks: on ? 'on' : 'off', ...(on ? {} : { autoUpdate: false }) }; void Storage.setItem('update-checks', String(init.body)); return answer(200, { checks: JSON.parse(init.body).on ? 'on' : 'off' }) }
      if (url.includes('/api/update/check')) { void Storage.setItem('update-check-posts', String(++checkPosts)); return answer(200, { latest: '9.0.0' }) }
      if (url.includes('/api/update')) {
        void Storage.setItem('update-posts', String(++updatePosts))
        return answer(502, { error: 'the test computer says no' })
      }
      if (url.includes('/api/info')) return { ok: params.info !== 'none', json: async () => (params.info === 'none' ? {} : params.info ? { ...JSON.parse(params.info), ...said } : { version: '9.9.9' }) }
      const body = JSON.parse(init.body)
      const ok = body.code === 'GOOD-CODE'
      return { ok, status: ok ? 200 : 401, json: async () => (ok ? { token: 'new-token', device: 'd2', name: 'studio' } : { error: 'unknown pairing code' }) }
    }) as any,
  }} />
))
