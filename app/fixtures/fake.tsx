// The app over an in-memory herdr-core (src/testing/fakecore.ts), already paired, for the screen tests.
// `host.params.scene`: "empty" (a fresh computer), "busy" (two spaces, agents in every state), "twins" (two Claude
// Codes in one space), "unpaired",
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
import { Root } from '../src/ui/root.tsx'
import { CLAUDE_EDIT_MENU, FakeCore, type FakeTab } from '../src/testing/fakecore.ts'
import { FakeFolders } from '../src/testing/fake-folders.ts'

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
// "unpaired": the computer refuses the saved token; a token from pairing again gets through.
let forgot = 0
const pipe = (l: Linker) => () => {
  const forgotten = (scene === 'unpaired' && l.link.get()?.token === 'synthetic-token') || ((scene === 'link-forgotten' || scene === 'relaunch-forgotten') && !forgot++)
  if (forgotten) { const c = new FakeCore(); c.send = () => c.drop('unpaired'); return c }
  return core
}

const routes = (params.routes ?? 'direct').split(',')
let updatePosts = 0
const fakeTunnel: Tunnel = {
  pair: async () => '', scan: async () => '', forget: () => {},
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

mount(() => (
  <Root deps={{
    linker: new Linker(params.via === 'tailpocket' ? { ...io, tunnel: fakeTunnel } : io),
    opensSettings: params.open === 'settings',
    prefs: new PrefStore(io),
    makeStore: (l) => new Store({ pipe: pipe(l) }),
    device: 'Test phone',
    pairLink: opened || scene === 'relaunch-forgotten' ? String((host.params as any)?.link) : undefined,
    // Recorded where a test can read it: the app's own storage.
    pairLinkUsed: () => void Storage.setItem('pair-link-used', '1'),
    fetcher: (async (url: string, init: any) => {
      if (url.includes('/api/files')) return answer(200, { path: landed(new URLSearchParams(url.split('?')[1]), init.body.size) })
      if (url.includes('/api/folders')) return folders(url, init)
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
