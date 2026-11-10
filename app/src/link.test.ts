// Pairing with the companion (`herdr-anywhere pair` prints `<url>/#pair=<code>`) and the saved link.
import { expect, test } from 'nsh/test'
import { companionVersion, folderApi, Linker, pairThrough, parseInvite, parsePairLink, pairWith, type Tunnel } from './link.ts'

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
