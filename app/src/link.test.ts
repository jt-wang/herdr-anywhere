// Pairing with the companion (`herdr-anywhere pair` prints `<url>/#pair=<code>`) and the saved link.
import { expect, test } from 'nsh/test'
import { chatApi, companionVersion, folderApi, imageApi, Linker, pairThrough, parseInvite, parsePairLink, pairWith, type Tunnel } from './link.ts'

test('a pairing link names the computer and the code; anything else is not one', () => {
  expect(parsePairLink(' http://192.168.1.20:7681/#pair=K7QD-M2XP ')).toEqual({ url: 'http://192.168.1.20:7681', code: 'K7QD-M2XP' })
  expect(parsePairLink('https://box.tail1234.ts.net/#pair=AB12')).toEqual({ url: 'https://box.tail1234.ts.net', code: 'AB12' })
  expect(parsePairLink('http://127.0.0.1:7690/#pair=WF-5t_eO2bI7Ey20YAYp')).toEqual({ url: 'http://127.0.0.1:7690', code: 'WF-5t_eO2bI7Ey20YAYp' })
  expect(parsePairLink('http://192.168.1.20:7681/')).toBe(null)
  expect(parsePairLink('K7QD-M2XP')).toBe(null)
})

const reply = (status: number, body: unknown) => async () => ({ ok: status < 300, status, json: async () => body }) as any

test('pairing posts the code with the phone name and keeps the token it gets', async () => {
  let sent: any
  const fetch = (async (url: string, init: any) => { sent = { url, ...init }; return reply(200, { token: 'tk', device: 'd1', name: 'studio' })() }) as any
  const link = await pairWith({ url: 'http://192.168.1.20:7681', code: 'AB12' }, 'Pixel', fetch)
  expect(sent.url).toBe('http://192.168.1.20:7681/api/pair')
  expect(sent.method).toBe('POST')
  expect(JSON.parse(sent.body)).toEqual({ code: 'AB12', device: 'Pixel' })
  expect(link).toEqual({ url: 'http://192.168.1.20:7681', token: 'tk', device: 'd1', name: 'studio', enc: 'bin' })
})

test('a refused pairing says why, in words a person can act on', async () => {
  const p = { url: 'http://192.168.1.20:7681', code: 'AB12' }
  const why = (f: any) => pairWith(p, 'Pixel', f).then(() => 'paired', (e) => e.message)
  expect(await why(reply(401, { error: 'unknown pairing code' }))).toBe('That code is not right. Check it, or run herdr-anywhere pair again.')
  expect(await why(reply(410, {}))).toBe('That code expired or was already used. Run herdr-anywhere pair again.')
  expect(await why(reply(429, {}))).toBe('Too many wrong codes. Wait a minute and try again.')
  expect(await why(async () => { throw new Error('connect ECONNREFUSED') })).toBe(
    "Can't reach 192.168.1.20:7681. Is herdr-anywhere serve running, and is this phone on the same network?")
})

test('the link survives a restart, and binary that does not get through switches it to base64 for good', async () => {
  const saved = new Map<string, string>()
  const made: { url: string; enc: string; pipe: any }[] = []
  const fakePipe = (url: string, enc: string) => {
    const pipe: any = { send() {}, close() {}, onClose: undefined }
    made.push({ url, enc, pipe })
    return pipe
  }
  const a = new Linker({ load: async (k) => saved.get(k) ?? null, save: (k, v) => saved.set(k, v), remove: (k) => saved.delete(k), socket: fakePipe as any })
  await a.load()
  expect(a.link.get()).toBe(null)
  a.set({ url: 'http://h:1', token: 't', device: 'd', name: 'studio', enc: 'bin' })
  const pipe = a.pipe()
  const reasons: string[] = []
  pipe.onClose = (r) => reasons.push(r)
  made[0].pipe.onClose('binary-unsupported')
  expect(reasons).toEqual(['binary-unsupported'])
  a.pipe()
  expect(made.map((m) => [m.url, m.enc])).toEqual([['ws://h:1/hc?token=t', 'bin'], ['ws://h:1/hc?token=t&enc=b64', 'b64']])
  const b = new Linker({ load: async (k) => saved.get(k) ?? null, save: (k, v) => saved.set(k, v), remove: (k) => saved.delete(k), socket: fakePipe as any })
  await b.load()
  expect(b.link.get()!.enc).toBe('b64')
  b.forget()
  expect(b.link.get()).toBe(null)
  expect(saved.has('link')).toBe(false)
})

// TailPocket (src/tunnel.ts): `herdr-anywhere pair` under `serve --tailcat` shows an invite; pairing through it gives the
// same token as /api/pair, and the phone reaches the companion through TailPocket's forwarder on 127.0.0.1.
const INVITE = 'herdr1:eyJ2IjoxLCJhZGRyZXNzIjoiZXhhbXBsZSJ9'
const settle = async () => { for (let i = 0; i < 5; i++) await Promise.resolve() }

/** A fake TailPocket: what it was asked, and each tunnel it opened. */
function fakeTunnel(o: { payload?: string; fail?: string; connectFails?: number } = {}) {
  const log: string[] = []
  let port = 40000
  let failures = o.connectFails ?? 0
  const t: Tunnel = {
    async pair(invite, device) {
      log.push(`pair ${invite} ${device}`)
      if (o.fail) throw Object.assign(new Error(o.fail), { code: 'ERR_NSH_HOST_ERROR' })
      return o.payload ?? JSON.stringify({ token: 'tk', device: 'd1', name: 'studio' })
    },
    async scan() { log.push('scan'); return INVITE },
    async connect() {
      if (failures > 0) { failures--; log.push('connect failed'); throw new Error('unreachable') }
      const p = ++port
      log.push(`connect ${p}`)
      return { baseUrl: `http://127.0.0.1:${p}`, close: () => { log.push(`close ${p}`) } }
    },
    forget() { log.push('forget') },
  }
  return { t, log }
}

test('the route comes from the open tunnel, a tunnel that cannot say is unknown, and a link without a tunnel has none', async () => {
  const saved = new Map<string, string>()
  const io = { load: async (k: string) => saved.get(k) ?? null, save: (k: string, v: string) => void saved.set(k, v), remove: (k: string) => void saved.delete(k) }
  const answers: any[] = [{ route: 'relay', relay: 'tok' }, { route: 'direct' }, new Error('unreachable'), { route: 'nonsense' }]
  let opened = 0
  const tunnel: Tunnel = { ...fakeTunnel().t, async connect() { opened++; return { baseUrl: 'http://127.0.0.1:1', close: () => {}, path: async () => { const a = answers.shift(); if (a instanceof Error) throw a; return a } } } }
  const l = new Linker({ ...io, tunnel })
  l.set({ url: 'tailpocket:', via: 'tailpocket', token: 't', device: 'd', name: 'studio', enc: 'bin' })
  expect(await l.path()).toEqual({ route: 'relay', relay: 'tok' })
  expect(await l.path()).toEqual({ route: 'direct' })
  expect(await l.path()).toEqual({ route: 'unknown' })
  expect(await l.path()).toEqual({ route: 'unknown' })
  expect(opened).toBe(1)
  const plain = new Linker(io)
  plain.set({ url: 'http://192.0.2.10:7681', token: 't', device: 'd', name: 'studio', enc: 'bin' })
  expect(await plain.path()).toBe(null)
})

test('a tunnel that cannot open has an unknown route', async () => {
  const saved = new Map<string, string>()
  const l = new Linker({ load: async (k) => saved.get(k) ?? null, save: (k, v) => saved.set(k, v), remove: (k) => saved.delete(k), tunnel: fakeTunnel({ connectFails: 1 }).t })
  l.set({ url: 'tailpocket:', via: 'tailpocket', token: 't', device: 'd', name: 'studio', enc: 'bin' })
  expect(await l.path()).toEqual({ route: 'unknown' })
})

test('the companion says its version at /api/info; a failure or an odd answer is no version', async () => {
  const linker = new Linker({ load: async () => null, save: () => {}, remove: () => {} })
  linker.set({ url: 'http://192.0.2.10:7681', token: 't', device: 'd', name: 'studio', enc: 'bin' })
  const asked: string[] = []
  const ok = (async (u: string) => { asked.push(u); return { ok: true, json: async () => ({ name: 'studio', version: '0.1.0', hc: 'hc 0.1.17' }) } }) as any
  expect(await companionVersion(linker, ok)).toBe('0.1.0')
  expect(asked).toEqual(['http://192.0.2.10:7681/api/info'])
  expect(await companionVersion(linker, (async () => { throw new Error('down') }) as any)).toBe('')
  expect(await companionVersion(linker, (async () => ({ ok: false, json: async () => ({}) })) as any)).toBe('')
  expect(await companionVersion(linker, (async () => ({ ok: true, json: async () => ({ version: 7 }) })) as any)).toBe('')
})

test('an invite is the herdr1: text, alone or inside what was scanned or pasted', () => {
  expect(parseInvite(` ${INVITE} `)).toBe(INVITE)
  expect(parseInvite(`Paste this invite into the Herdr app, or scan its QR code with the app:\n  ${INVITE}\n`)).toBe(INVITE)
  expect(parseInvite('tailpocket1:eyJ2IjoxfQ')).toBe(null)
  expect(parseInvite('http://192.168.1.20:7681/#pair=K7QD-M2XP')).toBe(null)
})

test('pairing through TailPocket keeps the token the computer put in its answer', async () => {
  const { t, log } = fakeTunnel()
  const link = await pairThrough(t, INVITE, 'iPhone')
  expect(log).toEqual([`pair ${INVITE} iPhone`])
  expect(link).toEqual({ url: 'tailpocket:', via: 'tailpocket', token: 'tk', device: 'd1', name: 'studio', enc: 'bin' })
})

test('a refused TailPocket pairing says why, in words a person can act on', async () => {
  const why = (o: any) => pairThrough(fakeTunnel(o).t, INVITE, 'iPhone').then(() => 'paired', (e) => e.message)
  expect(await why({ fail: 'expired' })).toBe('That invite expired. Run herdr-anywhere pair again.')
  expect(await why({ fail: 'unreachable' })).toBe(
    "Can't reach the computer. Is Companion running there (herdr-anywhere start), and is this phone online?")
  expect(await why({ fail: 'invalid-invite' })).toBe("That isn't an invite from herdr-anywhere pair. Scan its QR code again.")
  expect(await why({ payload: 'not json' })).toBe('The computer paired, but gave no token. Run herdr-anywhere pair again.')
})

test('a TailPocket link opens /hc on the tunnel, and a tunnel whose forwarder stops answering is opened again', async () => {
  const { t, log } = fakeTunnel()
  const made: { url: string; pipe: any }[] = []
  const socket = (url: string) => {
    const pipe: any = { sent: [] as Uint8Array[], send(b: Uint8Array) { pipe.sent.push(b) }, close() {} }
    made.push({ url, pipe })
    return pipe
  }
  const saved = new Map<string, string>()
  const l = new Linker({ load: async (k) => saved.get(k) ?? null, save: (k, v) => saved.set(k, v), remove: (k) => saved.delete(k), socket: socket as any, tunnel: t })
  l.set(await pairThrough(t, INVITE, 'iPhone'))
  const a = l.pipe()
  const reasons: string[] = []
  a.onClose = (r) => reasons.push(r)
  a.send(new Uint8Array([1]))
  await settle()
  expect(made.map((m) => m.url)).toEqual(['ws://127.0.0.1:40001/hc?token=tk'])
  expect(made[0].pipe.sent).toEqual([new Uint8Array([1])])
  // A socket that was answered and then dropped: the tunnel is kept.
  made[0].pipe.onBytes(new Uint8Array([2]))
  made[0].pipe.onClose('network')
  l.pipe()
  await settle()
  expect(made[1].url).toBe('ws://127.0.0.1:40001/hc?token=tk')
  // The dial to the forwarder failed (nothing heard): that tunnel is closed and the next pipe connects again.
  made[1].pipe.onClose('network')
  l.pipe()
  await settle()
  expect(made[2].url).toBe('ws://127.0.0.1:40002/hc?token=tk')
  expect(log.slice(1)).toEqual(['connect 40001', 'close 40001', 'connect 40002'])
  expect(reasons).toEqual(['network'])
})

// Measured over Tailcat's real relays (companion/cmd/herdr-anywhere/tailcat_lost_probe_test.go, HM_TAILCAT_PROBE=1):
// with Companion stopped and with this phone taken off the computer, the phone's handshake gets the same nothing
// back, for 10 s, with the same Tailcat log. So a socket that closes unanswered says only that: no answer.
test('through TailPocket, a socket that closes before the computer answered closes with no-answer; one that was answered, with its own reason', async () => {
  const { t } = fakeTunnel()
  const made: any[] = []
  const socket = () => { const pipe: any = { send() {}, close() {} }; made.push(pipe); return pipe }
  const l = new Linker({ load: async () => null, save() {}, remove() {}, socket: socket as any, tunnel: t })
  l.set(await pairThrough(t, INVITE, 'iPhone'))
  const reasons: string[] = []
  const a = l.pipe()
  a.onClose = (r) => reasons.push(r)
  await settle()
  made[0].onClose('network')
  const b = l.pipe()
  b.onClose = (r) => reasons.push(r)
  await settle()
  made[1].onBytes(new Uint8Array([1]))
  made[1].onClose('network')
  const c = l.pipe()
  c.onClose = (r) => reasons.push(r)
  await settle()
  made[2].onClose('unpaired')
  expect(reasons).toEqual(['no-answer', 'network', 'unpaired'])
})

test('a tunnel that cannot open closes the pipe, so the store retries; forgetting the computer forgets its TailPocket link', async () => {
  const { t, log } = fakeTunnel({ connectFails: 1 })
  const l = new Linker({ load: async () => null, save() {}, remove() {}, socket: (() => ({ send() {}, close() {} })) as any, tunnel: t })
  l.set({ url: 'tailpocket:', via: 'tailpocket', token: 'tk', device: 'd1', name: 'studio', enc: 'bin' })
  const reasons: string[] = []
  const p = l.pipe()
  p.onClose = (r) => reasons.push(r)
  await settle()
  expect(reasons).toEqual(['tunnel'])
  l.pipe()
  await settle()
  l.forget()
  expect(log).toEqual(['connect failed', 'connect 40001', 'close 40001', 'forget'])
})

// The companion's folder calls (GET/POST /api/folders), with the device's token, at the link's address or the tunnel's.
test("folders are listed and made on the computer with the phone's token; a missing one says which folder exists", async () => {
  const saved = new Map<string, string>()
  const l = new Linker({ load: async () => null, save: (k, v) => saved.set(k, v), remove() {} })
  l.set({ url: 'http://192.168.1.20:7681', token: 'tk', device: 'd1', name: 'studio', enc: 'bin' })
  const sent: any[] = []
  const answers: [number, unknown][] = [
    [200, { path: '/Users/sam/src', home: '/Users/sam', parent: '/Users/sam', git: false, folders: [{ name: 'api', git: true }] }],
    [404, { error: 'no such folder', missing: true, path: '/Users/sam/srx', existing: '/Users/sam' }],
    [200, { path: '/Users/sam/src/new', created: true }],
    [403, { error: "can't read this folder", path: '/private' }],
    [200, { path: '/Users/sam', home: '/Users/sam', parent: '/Users', git: false, folders: [{ name: '.config', git: false, hidden: true }] }],
  ]
  const fetch = (async (url: string, init: any) => { sent.push({ url, ...init }); return reply(...answers.shift()!)() }) as any
  const api = folderApi(l, fetch)
  expect((await api.list('/Users/sam/src') as any).folders).toEqual([{ name: 'api', git: true }])
  expect(sent[0].url).toBe('http://192.168.1.20:7681/api/folders?path=%2FUsers%2Fsam%2Fsrc')
  expect(sent[0].headers.authorization).toBe('Bearer tk')
  expect(await api.list('/Users/sam/srx')).toEqual({ missing: true, path: '/Users/sam/srx', existing: '/Users/sam' })
  expect(await api.create('/Users/sam/src/new')).toBe('/Users/sam/src/new')
  expect([sent[2].method, sent[2].url, JSON.parse(sent[2].body)]).toEqual(['POST', 'http://192.168.1.20:7681/api/folders', { path: '/Users/sam/src/new' }])
  expect(await api.list('/private').then(() => 'listed', (e) => e.message)).toBe("can't read this folder")
  // Show hidden asks for the dot folders too.
  await api.list('/Users/sam', true)
  expect(sent[4].url).toBe('http://192.168.1.20:7681/api/folders?path=%2FUsers%2Fsam&hidden=1')
})

// GET /api/image: the path as the terminal shows it and the terminal it is in; the computer says the real path.
test("a picture is asked for by the path a terminal shows, with the phone's token; a refusal says why", async () => {
  const l = new Linker({ load: async () => null, save() {}, remove() {} })
  l.set({ url: 'http://192.168.1.20:7681', token: 'tk', device: 'd1', name: 'studio', enc: 'bin' })
  const sent: any[] = []
  const answers: [number, unknown][] = [
    [200, { path: '/Users/sam/src/api/shots/a b.png', url: 'data:image/png;base64,iVBORw0KGgo=' }],
    [415, { error: 'not a picture', why: 'not_image', path: '/Users/sam/src/api/notes.png' }],
    [409, { error: 'something new', why: 'toString' }],
    [200, { path: 7, url: 'javascript:alert(1)' }],
  ]
  const fetch = (async (url: string, init: any) => { sent.push({ url, ...init }); return reply(...answers.shift()!)() }) as any
  const api = imageApi(l, fetch)
  expect(await api.read('shots/a b.png', 't3')).toEqual({ path: '/Users/sam/src/api/shots/a b.png', url: 'data:image/png;base64,iVBORw0KGgo=' })
  expect(sent[0].url).toBe('http://192.168.1.20:7681/api/image?path=shots%2Fa%20b.png&terminal=t3')
  expect(sent[0].headers.authorization).toBe('Bearer tk')
  // Why, in the phone's language, from the computer's code; a code it doesn't know of says it can't read it.
  expect(await api.read('notes.png', 't3').then(() => 'read', (e) => e.message)).toBe("This file isn't a picture.")
  expect(await api.read('n.png', 't3').then(() => 'read', (e) => e.message)).toBe("The computer can't read this picture.")
  // Only a picture's data: URL is shown.
  expect(await api.read('x.png', 't3').then(() => 'read', () => 'refused')).toBe('refused')
  const offline = imageApi(l, (async () => { throw new Error('down') }) as any)
  expect(await offline.read('x.png', 't3').then(() => 'read', (e) => e.message)).toBe("Can't reach the computer.")
})

// A picture on another machine comes through hc, which carries 8 MB at most: the limit said is the one the computer
// used. That machine may be out of reach while the computer is not.
test('a picture too big says the limit the computer used; a machine out of reach says so', async () => {
  const l = new Linker({ load: async () => null, save() {}, remove() {} })
  l.set({ url: 'http://192.168.1.20:7681', token: 'tk', device: 'd1', name: 'studio', enc: 'bin' })
  const answers: [number, unknown][] = [
    [413, { error: 'too big', why: 'too_big', limit: 8 << 20 }],
    [413, { error: 'too big', why: 'too_big', limit: 12 << 20 }],
    [413, { error: 'too big', why: 'too_big' }],
    [502, { error: "couldn't reach box", why: 'unreachable' }],
    [400, { error: "can't be read", why: 'remote' }],
  ]
  const api = imageApi(l, (async () => reply(...answers.shift()!)()) as any)
  const why = () => api.read('a.png', 'box/t3').then(() => 'read', (e) => e.message)
  expect(await why()).toBe('This picture is over 8 MB, too big to show.')
  expect(await why()).toBe('This picture is over 12 MB, too big to show.')
  expect(await why()).toBe('This picture is too big to show.')
  expect(await why()).toBe("Can't reach the machine this terminal is on.")
  expect(await why()).toBe("Pictures on this machine can't be read.")
})

test('through TailPocket, the folder calls go to the tunnel the pipes use', async () => {
  const { t } = fakeTunnel()
  const l = new Linker({ load: async () => null, save() {}, remove() {}, socket: (() => ({ send() {}, close() {} })) as any, tunnel: t })
  l.set(await pairThrough(t, INVITE, 'iPhone'))
  const urls: string[] = []
  const fetch = (async (url: string) => { urls.push(url); return reply(200, { path: '/Users/sam', home: '/Users/sam', parent: '/Users', git: false, folders: [] })() }) as any
  await folderApi(l, fetch).list('~')
  l.pipe()
  await folderApi(l, fetch).list('~')
  expect(urls).toEqual(['http://127.0.0.1:40001/api/folders?path=~', 'http://127.0.0.1:40001/api/folders?path=~'])
})

test('the chat calls send the token and the address; an older companion without them has no chat', async () => {
  const linker = new Linker({ load: async () => null, save: () => {}, remove: () => {} })
  linker.set({ url: 'http://192.0.2.10:7681', token: 'tk', device: 'd', name: 'studio', enc: 'bin' })
  const asked: { url: string; auth: string }[] = []
  const answer = (status: number, body: unknown) => (async (u: string, init: any) => { asked.push({ url: u, auth: init.headers.authorization }); return { ok: status < 300, status, json: async () => body } }) as any
  const turns = [{ id: 'u1', from: 'you', said: 'hi', steps: [], reply: 'hello', end: 'done' }]
  expect(await chatApi(linker, answer(200, { chat: true, turns })).get('t1')).toEqual({ chat: true, turns })
  expect(asked[0]).toEqual({ url: 'http://192.0.2.10:7681/api/chat/t1', auth: 'Bearer tk' })
  expect(await chatApi(linker, answer(404, 'not found')).get('t1')).toEqual({ chat: false })
  expect(await chatApi(linker, answer(200, { chat: true, unknown: 'behind' })).get('t1')).toEqual({ chat: true, unknown: 'behind' })
  await chatApi(linker, answer(200, { chat: false })).get('rohan/t7')
  expect(asked.at(-1)!.url).toBe('http://192.0.2.10:7681/api/chat/rohan%2Ft7')
  expect(await chatApi(linker, answer(200, { input: 'ls', output: 'a' })).step('t1', 'toolu_1')).toEqual({ input: 'ls', output: 'a' })
  expect(asked.at(-1)!.url).toBe('http://192.0.2.10:7681/api/chat/t1/steps/toolu_1')
  expect(await chatApi(linker, answer(404, { error: 'no such step' })).step('t1', 'toolu_9')).toBe(null)
  expect(await chatApi(linker, answer(200, { input: 'a.png', output: '', images: [{ id: 'toolu_1.0', source: '/w/a.png' }, { id: 7 }, 'x'] })).step('t1', 'toolu_1'))
    .toEqual({ input: 'a.png', output: '', images: [{ id: 'toolu_1.0', source: '/w/a.png' }, { id: 'x' }] })
  // A Companion from before images had sources sends their ids alone.
  expect(await chatApi(linker, answer(200, { input: 'a.png', output: '', images: ['toolu_1.0'] })).step('t1', 'toolu_1')).toEqual({ input: 'a.png', output: '', images: [{ id: 'toolu_1.0' }] })
  expect((await chatApi(linker, answer(200, { chat: true, turns: [{ id: 'u1', from: 'you', said: '', images: ['u1.0', { id: 'u1.1', source: '/s.png' }], steps: [], reply: '', end: 'done' }] })).get('t1')).turns![0].images)
    .toEqual([{ id: 'u1.0' }, { id: 'u1.1', source: '/s.png' }])
  expect(await chatApi(linker, answer(200, { url: 'data:image/png;base64,iVBO' })).image('t1', 'u1.1')).toBe('data:image/png;base64,iVBO')
  expect(asked.at(-1)!.url).toBe('http://192.0.2.10:7681/api/chat/t1/images/u1.1')
  expect(await chatApi(linker, answer(200, { url: 'https://example.com/x.png' })).image('t1', 'u1.1')).toBe(null)
  expect(await chatApi(linker, answer(404, { error: 'no such image' })).image('t1', 'u1.9')).toBe(null)
  let threw = false
  await chatApi(linker, (async () => { throw new Error('down') }) as any).get('t1').catch(() => { threw = true })
  expect(threw).toBe(true)
})

// The tap and the underline go only to what is a picture on that computer: it says so for many paths at once
// (POST /api/image/check). A computer too old to say gives null: the phone then goes by the name, as before.
test('which paths are pictures the computer says for many at once; one too old to say gives null', async () => {
  const l = new Linker({ load: async () => null, save() {}, remove() {} })
  l.set({ url: 'http://192.168.1.20:7681', token: 'tk', device: 'd1', name: 'studio', enc: 'bin' })
  const sent: any[] = []
  let answer: [number, unknown] = [200, { paths: [{ asked: 'a.png', path: '/w/a.png', picture: true }, { asked: 'gone.png', picture: false, why: 'missing' }, { asked: 'big.png', picture: true, why: 'too_big' }] }]
  const fetch = (async (url: string, init: any) => { sent.push({ url, ...init }); return reply(...answer)() }) as any
  const api = imageApi(l, fetch)
  expect(await api.check('box/t3', ['a.png', 'gone.png', 'big.png'])).toEqual({ 'a.png': true, 'gone.png': false, 'big.png': true })
  expect(sent[0].url).toBe('http://192.168.1.20:7681/api/image/check')
  expect(sent[0].method).toBe('POST')
  expect(sent[0].headers.authorization).toBe('Bearer tk')
  expect(JSON.parse(sent[0].body)).toEqual({ terminal: 'box/t3', paths: ['a.png', 'gone.png', 'big.png'] })
  answer = [404, {}]
  expect(await api.check('t1', ['a.png'])).toBe(null)
  answer = [405, {}]
  expect(await api.check('t1', ['a.png'])).toBe(null)
  answer = [502, { error: "couldn't reach box", why: 'unreachable' }]
  expect(await api.check('box/t3', ['a.png']).then(() => 'answered', (e) => e.message)).toBe("Can't reach the machine this terminal is on.")
  const offline = imageApi(l, (async () => { throw new Error('down') }) as any)
  expect(await offline.check('t1', ['a.png']).then(() => 'answered', (e) => e.message)).toBe("Can't reach the computer.")
})
