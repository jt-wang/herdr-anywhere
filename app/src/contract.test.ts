// The last release's Companion, frozen (fixtures/contract, recorded from its own source by
// companion/internal/server/contract_test.go): what it answered to every request its app sent. A computer keeps that
// Companion until its person updates it, so today's app must read those answers, and send only requests it took.
import { expect, test } from 'nsh/test'
import contract from '../fixtures/contract/v0.3.11.json'
import { chatApi, companionVersion, fileApi, folderApi, imageApi, Linker, pairWith, type Link } from './link.ts'
import { fromBase64, SocketPipe, toBase64 } from './hc/ws.ts'
import { Updates } from './updates.ts'
import { Signal, type Conn } from './store.ts'

type Call = { name: string; method: string; path: string; query?: Record<string, string>; headers?: Record<string, string>
  body?: unknown; text?: string; socket?: string; send?: string; answer: { status: number; headers?: Record<string, string>; body?: any; text?: string } }
const calls = (contract as any).calls as Call[]
const BASE = 'http://192.0.2.10:7681'

/** The old Companion, at BASE: answers a request only when it is one of the frozen ones, at its address, with every
 *  header, query and body as they were; anything else is kept in `unknown` (and answered 404, as a route it lacked).
 *  Two frozen requests alike (a setup installed, then over a changed file) answer in their order. Its /hc is a
 *  WebSocket for SocketPipe: the frozen bytes in the encoding's messages come back as its Companion sent them. */
function oldCompanion() {
  const vars: Record<string, string> = { '{code}': 'K7QD-M2XP' }
  const used = new Set<string>(), unknown: string[] = []
  const fill = (s: string) => Object.entries(vars).reduce((x, [k, v]) => x.split(k).join(v), s)
  const deep = (x: any): any => typeof x === 'string' ? fill(x) : Array.isArray(x) ? x.map(deep)
    : x && typeof x === 'object' ? Object.fromEntries(Object.entries(x).map(([k, v]) => [k, deep(v)])) : x
  const same = (a: unknown, b: unknown) => JSON.stringify(sorted(a)) === JSON.stringify(sorted(b))
  const sorted = (x: any): any => Array.isArray(x) ? x.map(sorted)
    : x && typeof x === 'object' ? Object.fromEntries(Object.keys(x).sort().map((k) => [k, sorted(x[k])])) : x
  const split = (url: string, origin: string) => {
    if (!url.startsWith(origin + '/')) return null
    const [path, q = ''] = url.slice(origin.length).split('?')
    return { path, query: Object.fromEntries(q.split('&').filter(Boolean).map((kv) => kv.split('=').map(decodeURIComponent))) }
  }
  const find = (method: string, url: string, headers: Record<string, string>, body: string | undefined, socket?: string) => {
    const at = split(url, socket ? BASE.replace(/^http/, 'ws') : BASE)
    const alike = !at ? [] : calls.filter((c) => (c.socket ?? '') === (socket ?? '') && c.method === method && c.path === at.path && same(at.query, deep(c.query ?? {}))
      && same(Object.fromEntries(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), v])), deep(c.headers ?? {}))
      && (c.body !== undefined ? body !== undefined && same(JSON.parse(body), deep(c.body)) : c.text !== undefined ? body === c.text : body === undefined))
    const c = alike.find((c) => !used.has(c.name)) ?? alike[0]
    if (!c) { unknown.push(`${method} ${url} ${JSON.stringify(headers)} ${body ?? ''}`); return null }
    used.add(c.name)
    if (c.name === 'pair') vars['{token}'] = c.answer.body.token
    if (c.name === 'chat') { vars['{etag}'] = c.answer.headers!.etag; vars['{before}'] = c.answer.body.before }
    if (c.name === 'chat setup changed') vars['{confirmation}'] = c.answer.body.confirmation
    return c
  }
  class WebSocket {
    readyState = 0
    binaryType = 'blob'
    onopen?: () => void
    onmessage?: (e: { data: unknown }) => void
    onerror?: () => void
    onclose?: (e: { code: number }) => void
    private call: Call | null
    constructor(url: string) {
      const enc = url.includes('enc=b64') ? 'b64' : 'bin'
      this.call = find('GET', url, {}, undefined, enc)
      setTimeout(() => this.call ? (this.readyState = 1, this.onopen?.()) : this.close(1006))
    }
    send(data: string | ArrayBuffer) {
      const c = this.call!
      // The old Companion closes a socket whose messages are not its encoding's (bridge.go): 1003.
      if ((c.socket === 'b64') !== (typeof data === 'string')) return this.close(1003)
      const got = typeof data === 'string' ? data : toBase64(new Uint8Array(data))
      if (got !== c.send) { unknown.push(`${c.name}: sent ${got}`); return this.close(1006) }
      const back = c.answer.text!
      setTimeout(() => this.onmessage?.({ data: c.socket === 'b64' ? back : fromBase64(back).slice().buffer }))
    }
    close(code = 1000) {
      if (this.readyState === 3) return
      this.readyState = 3
      setTimeout(() => this.onclose?.({ code }))
    }
  }
  const fetcher = (async (url: string, init?: RequestInit) => {
    const raw = init?.body
    const body = raw === undefined || raw === null ? undefined : typeof raw === 'string' ? raw : await (raw as Blob).text()
    const c = find(init?.method ?? 'GET', url, (init?.headers ?? {}) as Record<string, string>, body)
    const a = c?.answer ?? { status: 404, body: { error: 'not found' } }
    return { ok: a.status >= 200 && a.status < 300, status: a.status, headers: { get: (k: string) => a.headers?.[k.toLowerCase()] ?? null },
      json: async () => { if (a.body === undefined) throw new Error('no JSON'); return JSON.parse(JSON.stringify(a.body)) },
      text: async () => a.text ?? JSON.stringify(a.body) }
  }) as unknown as typeof fetch
  const socket = (url: string, enc: 'bin' | 'b64') => new SocketPipe(url, enc, { WebSocket: WebSocket as any, setTimeout, clearTimeout })
  return { fetcher, used, unknown, socket, answer: (name: string) => calls.find((c) => c.name === name)!.answer }
}

/** What comes back through the link's pipe for the bytes sent: the bytes, or why it closed. */
function through(linker: Linker, bytes: Uint8Array): Promise<string> {
  return new Promise((done) => {
    const pipe = linker.pipe()
    pipe.onBytes = (b) => { done(toBase64(b)); pipe.close() }
    pipe.onClose = (why) => done(`closed: ${why}`)
    pipe.send(bytes)
  })
}

test("today's app pairs with the last release's Companion and reads every answer it gave", async () => {
  const old = oldCompanion()
  const link: Link = await pairWith({ url: BASE, code: 'K7QD-M2XP' }, 'Pixel', old.fetcher)
  expect(link).toEqual({ url: BASE, token: 'contract-token', device: old.answer('pair').body.device, name: 'studio', enc: 'bin' })
  const linker = new Linker({ load: async () => null, save() {}, remove() {}, socket: old.socket })
  linker.set(link)
  expect(await companionVersion(linker, old.fetcher)).toBe('0.3.11')

  const folders = folderApi(linker, old.fetcher)
  const listing: any = await folders.list('~/proj')
  expect([listing.path, listing.parent, listing.folders]).toEqual(['/Users/sam/proj', '/Users/sam', [{ name: 'app', git: true }, { name: 'src', git: false }]])
  expect(((await folders.list('~/proj', true)) as any).folders[0]).toEqual({ name: '.cache', git: false, hidden: true })
  expect(await folders.list('~/proj/nope')).toEqual({ missing: true, path: '/Users/sam/proj/nope', existing: '/Users/sam/proj' })
  expect(await folders.create('~/proj/new')).toBe('/Users/sam/proj/new')

  const chat = chatApi(linker, old.fetcher)
  const page = await chat.get('t1')
  expect([page.chat, page.turns?.map((t) => [t.said, t.reply, t.end, t.steps.map((s) => s.id)])]).toEqual([true, [['hi', 'hello', 'done', ['toolu_1']]]])
  expect(await chat.get('t1')).toBe(page) // the second read sent the page's ETag, and the old Companion said 304
  expect(old.used.has('chat unchanged')).toBe(true)
  const earlier = await chat.get('t1', page.before)
  expect(earlier.turns?.map((t) => [t.id, t.said, t.reply])).toEqual([['older', 'earlier question', 'earlier answer']])
  expect(await chat.step('t1', 'toolu_1')).toEqual({ input: 'ls', output: 'a.txt' })
  expect(await chat.image('t1', 'toolu_1.0')).toBe('data:image/png;base64,iVBORw==')
  expect(await chat.title!('t1')).toEqual({ title: 'Fix the login redirect', session: old.answer('chat title').body.session })
  expect((await chat.files('t1', ['NOTES.md', 'shot.png'])).map((f) => [f.asked, f.kind, f.size])).toEqual([['NOTES.md', 'text', 15], ['shot.png', 'image', 121]])
  expect(await chat.fileLink('t1', 'NOTES.md')).toBe(`${BASE}${old.answer('file link').body.url}`)
  expect(await chat.fileText('t1', 'NOTES.md', 100)).toBe('# Notes\n\n- one\n')
  expect((await chat.setups()).map((s) => [s.agent, s.state, s.version])).toEqual([['pi', 'absent', 2]])
  expect((await chat.setSetup('pi', true)).result).toBe('ok')
  expect((await chat.setSetup('pi', false)).setup?.state).toBe('absent')
  // The person edited the file: the computer says so, with what to send back once the person says replace it.
  const changed = await chat.setSetup('pi', true)
  expect([changed.result, changed.setup?.state, changed.setup?.changedFiles]).toEqual(['changed', 'changed', ['/Users/sam/.pi/agent/extensions/herdr-anywhere.ts']])
  expect((await chat.setSetup('pi', true, true, changed.setup?.confirmation)).result).toBe('ok')

  const images = imageApi(linker, old.fetcher)
  expect((await images.read('shot.png', 't1')).path).toBe('/Users/sam/proj/shot.png')
  expect(await images.check('t1', ['shot.png', 'NOTES.md'])).toEqual({ 'shot.png': true, 'NOTES.md': false })
  const files = fileApi(linker, old.fetcher)
  expect(await files.sendPhoto(new File(['hello'], 'note.txt'))).toBe(old.answer('upload').body.path)
  expect(await files.sendPhoto(new File(['hello'], 'note.txt'), 'box')).toMatch(/^\/home\/sam\/\.cache\/herdr-anywhere\/uploads\/.+\/note\.txt$/)

  // The pipe to herdr-core, binary, then base64 text: the old Companion's /hc took these bytes and gave these back.
  const bytes = fromBase64(calls.find((c) => c.name === 'socket')!.send!)
  expect(await through(linker, bytes)).toBe(old.answer('socket').text)
  linker.set({ ...link, enc: 'b64' })
  expect(await through(linker, bytes)).toBe(old.answer('socket base64').text)
  expect(old.unknown).toEqual([])
  expect(calls.map((c) => c.name).filter((n) => !old.used.has(n) && !n.startsWith('update'))).toEqual([]) // the rest: the next test
})

test("today's app reads the last release's Companion's update answers, and changes its settings as it did", async () => {
  const old = oldCompanion()
  await pairWith({ url: BASE, code: 'K7QD-M2XP' }, 'Pixel', old.fetcher) // the token the calls carry
  const toasts: { title: string; body?: string }[] = []
  const u = new Updates({ baseUrl: async () => BASE, token: () => 'contract-token', conn: new Signal<Conn>('ready'), fetcher: old.fetcher,
    os: 'android', appVersion: '0.3.11', toast: (t) => toasts.push(t), install: async () => 'installing', waitMs: 5 })
  await u.refresh()
  expect(u.info.get()).toEqual({ version: '0.3.11', latest: '0.3.12', knowsReleases: true, canUpdate: true, autoUpdate: false, checks: 'ask',
    app: { url: 'https://example.com/a.apk', digest: 'sha256:ab', bytes: 5 }, plainUntil: '2026-12-01T12:00:00Z' })
  expect(u.notice()).toEqual({ kind: 'computer', version: '0.3.12' })
  await u.setAuto(true)
  expect(u.info.get()?.autoUpdate).toBe(true)
  await u.setChecks(true) // and checks now
  expect(u.checked.get()).toEqual({ latest: '0.3.12' })
  await u.updateComputer() // the fake never comes back as 0.3.12, so it says so with the version it was told
  expect(toasts.map((t) => t.body ?? '').some((b) => b.includes('0.3.12'))).toBe(true)
  expect(old.unknown).toEqual([])
  expect(calls.map((c) => c.name).filter((n) => !old.used.has(n) && n.startsWith('update'))).toEqual([])
})
