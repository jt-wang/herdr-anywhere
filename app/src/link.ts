// The phone's link to one computer: pairing with the companion (`herdr-anywhere pair` prints `<url>/#pair=<code>`, or,
// under `serve --tailcat`, a TailPocket invite), the token it gives, kept in the app's storage, and the WebSocket pipe
// to herdr-core it opens: at the companion's address, or through TailPocket's forwarder on 127.0.0.1.
import { host as nativeHost } from 'nsh'
import type { Pipe } from './hc/client.ts'
import { SocketPipe, socketUrl, type Enc } from './hc/ws.ts'
import { Signal } from './store.ts'
import type { FolderApi, Listing, Missing } from './model/folders.ts'
import { t as tr, type Key } from './strings/index.ts'
import type { ChatAnswer, ChatApi, ChatChange, ChatPicture, ChatRead, ChatRun, ChatSetup } from './model/chat.ts'

/** `via: 'tailpocket'`: the computer is reached through TailPocket, which keeps its own link (with the phone's key).
 *  `via: 'demo'`: Try it's made-up computer (demo.ts). */
export type Link = { url: string; token: string; device: string; name: string; enc: Enc; via?: 'tailpocket' | 'demo' }
export type PairLink = { url: string; code: string }

export function parsePairLink(text: string): PairLink | null {
  const m = /^(https?:\/\/[^\s/#?]+)[^\s#]*#pair=([A-Za-z0-9_-]+)$/.exec(text.trim())
  return m ? { url: m[1], code: m[2] } : null
}

const host = (url: string) => url.replace(/^https?:\/\//, '')

export async function pairWith(p: PairLink, device: string, fetcher: typeof fetch = fetch): Promise<Link> {
  let r: Response
  try {
    r = await fetcher(`${p.url}/api/pair`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ code: p.code, device }) })
  } catch {
    throw new Error(tr('cantReachHost', { host: host(p.url) }))
  }
  if (r.status === 401) throw new Error(tr('codeWrong'))
  if (r.status === 410) throw new Error(tr('codeExpired'))
  if (r.status === 429) throw new Error(tr('tooManyCodes'))
  const body: any = await r.json().catch(() => ({}))
  if (!r.ok || typeof body.token !== 'string') throw new Error(body.error ? tr('refusedBecause', { reason: body.error }) : tr('refusedStatus', { status: r.status }))
  return { url: p.url, token: body.token, device: body.device ?? '', name: body.name ?? host(p.url), enc: 'bin' }
}

/** TailPocket on the phone (src/tunnel.ts; a fake in tests). Failures carry TailPocket's code (its docs/BRIDGE.md). */
/** How the tunnel reaches the computer now, as Tailcat measured it; `unknown` when it could not tell. */
export type Route = { route: 'direct' } | { route: 'relay'; relay?: string } | { route: 'unknown' }

export type Tunnel = {
  /** Pairs with the computer that showed `invite` and keeps its link; resolves with the computer's answer. */
  pair: (invite: string, device: string) => Promise<string>
  /** The text of a QR code, from the camera. */
  scan: () => Promise<string>
  /** Opens the tunnel to the paired computer: `baseUrl` is `http://127.0.0.1:<port>`, which ends at the companion. */
  connect: () => Promise<Opened>
  forget: () => void
}

/** The invite `herdr-anywhere pair` shows under `serve --tailcat` (companion/internal/tailcat Prefix), within `text`. */
export function parseInvite(text: string): string | null {
  return /(?:^|\s)(herdr1:[A-Za-z0-9_-]+)(?:\s|$)/.exec(text)?.[1] ?? null
}

/** TailPocket's failure code: its `code`, or, from the native bridge through NSH's host.call, the message. */
export const tunnelCode = (e: any): string => (/^[a-z-]+$/.test(e?.code ?? '') ? e.code : String(e?.message ?? e))

const TUNNEL_REFUSED: Record<string, Key> = {
  expired: 'inviteExpired',
  unreachable: 'tunnelUnreachable',
  'invalid-invite': 'inviteInvalid',
  denied: 'tunnelDenied',
}

/** Pairs through TailPocket: the companion's answer (its Payload) is what `POST /api/pair` returns. */
export async function pairThrough(tunnel: Tunnel, invite: string, device: string): Promise<Link> {
  let answer: string
  try {
    answer = await tunnel.pair(invite, device)
  } catch (e) {
    const code = tunnelCode(e)
    throw new Error(TUNNEL_REFUSED[code] ? tr(TUNNEL_REFUSED[code]) : tr('tunnelFailed', { code }))
  }
  let body: any = null
  try { body = JSON.parse(answer) } catch {}
  if (typeof body?.token !== 'string') throw new Error(tr('noToken'))
  return { url: 'tailpocket:', via: 'tailpocket', token: body.token, device: body.device ?? '', name: body.name ?? tr('computer'), enc: 'bin' }
}

export type LinkerIO = {
  load: (k: string) => Promise<string | null>
  save: (k: string, v: string) => void
  remove: (k: string) => void
  socket?: (url: string, enc: Enc) => Pipe
  tunnel?: Tunnel
}

type Opened = { baseUrl: string; close: () => void; path?: () => Promise<Route> }
/** A tunnel being opened, and once open, the tunnel; `answered` once a socket on it heard the computer. */
type Tunnelling = { opening: Promise<Opened>; open?: Opened; answered?: boolean }

export class Linker {
  readonly link = new Signal<Link | null>(null)
  /** The TailPocket tunnel every pipe uses, until its forwarder stops answering. */
  private tunnel?: Tunnelling
  constructor(private io: LinkerIO) {}

  async load() {
    const v = await this.io.load('link')
    if (!v) return
    try {
      const l = JSON.parse(v)
      if (typeof l.url === 'string' && typeof l.token === 'string') this.link.set({ enc: 'bin', device: '', name: host(l.url), ...l })
    } catch {}
  }

  set(l: Link) {
    this.dropTunnel(this.tunnel)
    this.io.save('link', JSON.stringify(l))
    this.link.set(l)
  }

  forget() {
    this.dropTunnel(this.tunnel)
    if (this.link.get()?.via === 'tailpocket') this.io.tunnel?.forget()
    this.io.remove('link')
    this.link.set(null)
  }

  /** A pipe to herdr-core through the companion; binary that does not get through moves the link to base64. */
  pipe(): Pipe {
    const l = this.link.get()
    if (!l) throw new Error(tr('notPaired'))
    const open = this.io.socket ?? ((url, enc) => new SocketPipe(url, enc))
    const outer: Pipe = { send: () => {}, close: () => {} }
    const closed = (r: string) => {
      if (r === 'binary-unsupported' && l.enc === 'bin' && this.link.get() === l) this.set({ ...l, enc: 'b64' })
      outer.onClose?.(r)
    }
    if (l.via !== 'tailpocket') {
      const inner = open(socketUrl(l.url, l.token, l.enc), l.enc)
      inner.onBytes = (b) => outer.onBytes?.(b)
      inner.onClose = closed
      outer.send = (b) => inner.send(b)
      outer.close = () => inner.close()
      return outer
    }
    // Through TailPocket: the socket opens once the tunnel is up, and bytes sent before then wait. A socket that closes
    // unanswered means the forwarder no longer reaches the computer (the phone slept, or the computer restarted its
    // node): that tunnel is closed, and the store's retry opens a new one, as TailPocket advises.
    if (!this.io.tunnel) throw new Error(tr('noTunnel'))
    const t = (this.tunnel ??= this.tunnelling(this.io.tunnel))
    let inner: Pipe | undefined
    let ended = false
    const queue: Uint8Array[] = []
    outer.send = (b) => { if (inner) inner.send(b); else if (!ended) queue.push(b) }
    outer.close = () => {
      if (inner) return inner.close()
      if (!ended) { ended = true; outer.onClose?.('closed') }
    }
    t.opening.then((c) => {
      if (ended) return
      let heard = false
      const s = (inner = open(socketUrl(c.baseUrl, l.token, l.enc), l.enc))
      s.onBytes = (b) => { heard = t.answered = true; outer.onBytes?.(b) }
      // Unanswered, it is all the phone can know: Companion stopped and this phone taken off the computer look the
      // same through the tunnel (link.test.ts). Unless the tunnel answered before: then it is the tunnel that stopped
      // (the phone was suspended in the background), and a new one may well reach the computer: `stale`.
      s.onClose = (r) => {
        const silent = r === 'network' && !heard
        if (silent) this.dropTunnel(t)
        closed(!silent ? r : t.answered ? 'stale' : 'no-answer')
      }
      for (const b of queue.splice(0)) s.send(b)
    }, () => {
      if (this.tunnel === t) this.tunnel = undefined
      if (!ended) { ended = true; outer.onClose?.('tunnel') }
    })
    return outer
  }

  /** How the tunnel travels right now; null for a link with no tunnel. Never a guess: `unknown` when it cannot tell. */
  async path(): Promise<Route | null> {
    const l = this.link.get()
    if (!l || l.via !== 'tailpocket') return null
    if (!this.io.tunnel) return { route: 'unknown' }
    const t = (this.tunnel ??= this.tunnelling(this.io.tunnel))
    try {
      const r = await (await t.opening).path?.()
      if (r?.route === 'direct') return { route: 'direct' }
      if (r?.route === 'relay') return r.relay ? { route: 'relay', relay: r.relay } : { route: 'relay' }
    } catch {
      if (!t.open && this.tunnel === t) this.tunnel = undefined
    }
    return { route: 'unknown' }
  }

  /** The companion's address for its http calls: the link's, or the tunnel's that the pipes use. */
  async baseUrl(): Promise<string> {
    const l = this.link.get()
    if (!l) throw new Error(tr('notPaired'))
    if (l.via !== 'tailpocket') return l.url
    if (!this.io.tunnel) throw new Error(tr('noTunnel'))
    const t = (this.tunnel ??= this.tunnelling(this.io.tunnel))
    try {
      return (await t.opening).baseUrl
    } catch (e) {
      if (this.tunnel === t) this.tunnel = undefined
      throw e
    }
  }

  /** Drops the tunnel, so the next pipe opens a new one: a line on it stopped answering (Store.foreground). */
  renew() {
    this.dropTunnel(this.tunnel)
  }

  private tunnelling(tunnel: Tunnel): Tunnelling {
    const t: Tunnelling = { opening: tunnel.connect() }
    t.opening.then((c) => { if (this.tunnel === t) t.open = c; else c.close() }, () => {})
    return t
  }

  private dropTunnel(t: Tunnelling | undefined) {
    if (!t || t !== this.tunnel) return
    this.tunnel = undefined
    t.open?.close()
  }
}

/** The companion's own version, from its `/api/info`; '' when it cannot say. */
export async function companionVersion(linker: Linker, fetcher: typeof fetch = fetch): Promise<string> {
  try {
    const r = await fetcher(`${await linker.baseUrl()}/api/info`)
    const body: any = r.ok ? await r.json() : {}
    return typeof body.version === 'string' ? body.version : ''
  } catch {
    return ''
  }
}

/** The companion's folder calls (companion/README.md): a folder's subfolders, and making one after the person asked. */
export function folderApi(linker: Linker, fetcher: typeof fetch = fetch): FolderApi {
  const call = async (path: string, init?: RequestInit) => {
    const token = linker.link.get()?.token ?? ''
    let r: Response
    try {
      r = await fetcher(`${await linker.baseUrl()}${path}`, { ...init, headers: { authorization: `Bearer ${token}`, ...(init?.body ? { 'content-type': 'application/json' } : {}) } })
    } catch {
      throw new Error(tr('cantReachComputer'))
    }
    const body: any = await r.json().catch(() => ({}))
    return { r, body }
  }
  return {
    async list(path, hidden) {
      const { r, body } = await call(`/api/folders?path=${encodeURIComponent(path)}${hidden ? '&hidden=1' : ''}`)
      if (r.status === 404 && body.missing) return { missing: true, path: body.path, existing: body.existing } as Missing
      if (!r.ok) throw new Error(body.error ?? tr('refusedStatus', { status: r.status }))
      return body as Listing
    },
    async create(path) {
      const { r, body } = await call('/api/folders', { method: 'POST', body: JSON.stringify({ path }) })
      if (!r.ok) throw new Error(body.error ?? tr('refusedStatus', { status: r.status }))
      return body.path
    },
  }
}

/** The chat view's calls (GET /api/chat/{terminal}, companion/internal/server/chat.go). A companion from before them
 *  answers 404: no terminal has a chat there. A network failure throws, so an open view can say it is offline. */
/** The images of a turn or step as sent: { id, source }, or an id alone from a Companion from before sources. */
const pictures = (x: unknown): ChatPicture[] => !Array.isArray(x) ? [] : x.flatMap((p: any): ChatPicture[] =>
  typeof p === 'string' ? [{ id: p }]
    : typeof p?.id === 'string' ? [{ id: p.id, ...(typeof p.source === 'string' && p.source ? { source: p.source } : {}) }] : [])

export function chatApi(linker: Linker, fetcher: typeof fetch = fetch): ChatApi {
  const call = async (path: string) => {
    const r = await fetcher(`${await linker.baseUrl()}${path}`, { headers: { authorization: `Bearer ${linker.link.get()?.token ?? ''}` } })
    return { r, body: (await r.json().catch(() => ({}))) as any }
  }
  return {
    async get(address) {
      const { r, body } = await call(`/api/chat/${encodeURIComponent(address)}`)
      if (!r.ok || !body || typeof body.chat !== 'boolean') return { chat: false }
      if (Array.isArray(body.turns)) for (const turn of body.turns) if (turn?.images) turn.images = pictures(turn.images)
      return body as ChatAnswer
    },
    async step(address, id) {
      const { r, body } = await call(`/api/chat/${encodeURIComponent(address)}/steps/${encodeURIComponent(id)}`)
      if (!r.ok || typeof body?.input !== 'string') return null
      const images = pictures(body.images), run = runOf(body.run), change = changeOf(body.change), read = readOf(body.read)
      return { input: body.input, output: String(body.output ?? ''), ...(images.length ? { images } : {}), ...(run ? { run } : {}), ...(change ? { change } : {}), ...(read ? { read } : {}) }
    },
    async image(address, id) {
      const { r, body } = await call(`/api/chat/${encodeURIComponent(address)}/images/${encodeURIComponent(id)}`)
      return r.ok && typeof body?.url === 'string' && body.url.startsWith('data:image/') ? body.url : null
    },
    async files(address, paths) {
      if (!paths.length) return []
      const r = await fetcher(`${await linker.baseUrl()}/api/file/check`, { method: 'POST', body: JSON.stringify({ terminal: address, paths }),
        headers: { authorization: `Bearer ${linker.link.get()?.token ?? ''}`, 'content-type': 'application/json' } })
      const body = (await r.json().catch(() => ({}))) as any
      if (!r.ok || !Array.isArray(body?.files)) return []
      const kinds = ['video', 'pdf', 'text', 'image', 'other']
      return body.files.filter((f: any) => f && !f.why && typeof f.asked === 'string' && typeof f.path === 'string' && typeof f.name === 'string' && kinds.includes(f.kind) && typeof f.size === 'number')
        .map((f: any) => ({ asked: f.asked, path: f.path, name: f.name, kind: f.kind, size: f.size }))
    },
    async fileLink(address, path) {
      const base = await linker.baseUrl()
      const r = await fetcher(`${base}/api/file/link`, { method: 'POST', body: JSON.stringify({ terminal: address, path }),
        headers: { authorization: `Bearer ${linker.link.get()?.token ?? ''}`, 'content-type': 'application/json' } })
      const body = (await r.json().catch(() => ({}))) as any
      return r.ok && typeof body?.url === 'string' && body.url.startsWith('/api/file?') ? base + body.url : null
    },
    async fileText(address, path, max) {
      const r = await fetcher(`${await linker.baseUrl()}/api/file?path=${encodeURIComponent(path)}&terminal=${encodeURIComponent(address)}`,
        { headers: { authorization: `Bearer ${linker.link.get()?.token ?? ''}`, range: `bytes=0-${max - 1}` } })
      return r.ok ? await r.text() : null
    },
    async setups() {
      const { r, body } = await call('/api/chat-setups')
      return r.ok && Array.isArray(body?.setups) ? body.setups.map(setupOf).filter((x: ChatSetup | null): x is ChatSetup => !!x) : []
    },
    async setSetup(agent, on, force) {
      const r = await fetcher(`${await linker.baseUrl()}/api/chat-setups/${encodeURIComponent(agent)}${force ? '?force=1' : ''}`,
        { method: on ? 'PUT' : 'DELETE', headers: { authorization: `Bearer ${linker.link.get()?.token ?? ''}` } })
      const setup = setupOf(await r.json().catch(() => null)) ?? undefined
      if (r.ok) return { result: 'ok', setup }
      if (r.status === 409) return { result: setup?.state === 'no-agent' ? 'no-agent' : 'changed', setup }
      return { result: 'failed', setup }
    },
  }
}

const SETUP_STATES = ['no-agent', 'absent', 'installed', 'old', 'changed']
/** A setup as the computer sent it; a state this app doesn't know reads as one it can't act on. */
function setupOf(x: any): ChatSetup | null {
  if (!x || typeof x.agent !== 'string' || !x.agent || typeof x.file !== 'string') return null
  return { agent: x.agent, name: typeof x.name === 'string' && x.name ? x.name : x.agent, file: x.file,
    ...(typeof x.reload === 'string' && x.reload ? { reload: x.reload } : {}), version: Number(x.version) || 0,
    state: SETUP_STATES.includes(x.state) ? x.state : 'no-agent' }
}

/** A picture a terminal shows, read on the computer (GET /api/image): `path` is where it is there. */
export type Picture = { path: string; url: string }
// The computer's reasons (image.go `why`) said in the phone's language; the rest come down to "can't read it".
const PICTURE_WHY: Record<string, Key> = { missing: 'pictureMissing', outside: 'pictureOutside', not_image: 'pictureNotImage', too_big: 'pictureTooBig', remote: 'pictureRemote', unreachable: 'pictureUnreachable' }
// The limit the computer used, in whole MB (12 here, 8 from another machine: image.go maxImage, maxThere).
function pictureWhy(body: any): string {
  const why = typeof body.why === 'string' && Object.prototype.hasOwnProperty.call(PICTURE_WHY, body.why) ? body.why : ''
  if (why === 'too_big') {
    const mb = typeof body.limit === 'number' && body.limit > 0 ? Math.floor(body.limit / (1 << 20)) : 0
    return mb > 0 ? tr('pictureTooBig', { mb }) : tr('pictureTooBigNoLimit')
  }
  return tr(why ? PICTURE_WHY[why] : 'pictureCantRead')
}
export type ImageApi = {
  read(path: string, terminal: string): Promise<Picture>
  /** Which of the paths a terminal shows are pictures there, by path as asked; null: the computer is too old to say. */
  check(terminal: string, paths: string[]): Promise<Record<string, boolean> | null>
}

/** The picture at a path a terminal shows: the computer finds the file, says whether it may be read and is a picture
 *  (companion/internal/server/image.go). Nothing is kept: the sheet holds it while it is open. */
export function imageApi(linker: Linker, fetcher: typeof fetch = fetch): ImageApi {
  return {
    async read(path, terminal) {
      const token = linker.link.get()?.token ?? ''
      let r: Response
      try {
        r = await fetcher(`${await linker.baseUrl()}/api/image?path=${encodeURIComponent(path)}&terminal=${encodeURIComponent(terminal)}`, { headers: { authorization: `Bearer ${token}` } })
      } catch {
        throw new Error(tr('cantReachComputer'))
      }
      const body: any = await r.json().catch(() => ({}))
      if (!r.ok) throw new Error(pictureWhy(body))
      if (typeof body.path !== 'string' || typeof body.url !== 'string' || !/^data:image\/(png|jpeg|gif|webp);base64,/.test(body.url)) throw new Error(tr('pictureCantRead'))
      return { path: body.path, url: body.url }
    },
    async check(terminal, paths) {
      let r: Response
      try {
        r = await fetcher(`${await linker.baseUrl()}/api/image/check`, { method: 'POST', body: JSON.stringify({ terminal, paths }),
          headers: { authorization: `Bearer ${linker.link.get()?.token ?? ''}`, 'content-type': 'application/json' } })
      } catch {
        throw new Error(tr('cantReachComputer'))
      }
      // A Companion from before the check has no such route.
      if (r.status === 404 || r.status === 405) return null
      const body: any = await r.json().catch(() => ({}))
      if (!r.ok) throw new Error(pictureWhy(body))
      const out: Record<string, boolean> = {}
      for (const a of Array.isArray(body.paths) ? body.paths : []) if (typeof a?.asked === 'string') out[a.asked] = a.picture === true
      return out
    },
  }
}

/** The largest file the phone sends (companion/internal/server/files.go maxUpload). */
export const MAX_FILE = 2 * 1024 * 1024 * 1024

/** What page.pickFiles gives: a file the host holds, sent later by page.uploadFile. */
export type PickedFile = { id: string; name: string; size: number }

/** What the composer attaches goes over plain HTTP to the companion (POST /api/files), never over the socket to
 *  herdr-core, whose one message can hold 8 MiB at most. `on` is the machine the terminal is on. */
export type FileApi = {
  sendPhoto(file: File, on?: string): Promise<string>
  pick(): Promise<PickedFile[]>
  sendPicked(file: PickedFile, on?: string): Promise<string>
}

export function fileApi(linker: Linker, fetcher: typeof fetch = fetch): FileApi {
  const url = async (name: string, on?: string) => `${await linker.baseUrl()}/api/files?name=${encodeURIComponent(name)}${on ? `&on=${encodeURIComponent(on)}` : ''}`
  const landed = (status: number, body: any, name: string): string => {
    if (status === 413) throw new Error(tr('fileTooBig', { name }))
    if (status === 404) throw new Error(tr('attachNeedsUpdate'))
    if (status < 200 || status >= 300 || typeof body?.path !== 'string') throw new Error(body?.error ?? tr('refusedStatus', { status }))
    return body.path
  }
  return {
    async sendPhoto(file, on) {
      let r: Response
      try {
        r = await fetcher(await url(file.name, on), { method: 'POST', body: file, headers: { authorization: `Bearer ${linker.link.get()?.token ?? ''}`, 'content-type': 'application/octet-stream' } })
      } catch {
        throw new Error(tr('cantReachComputer'))
      }
      return landed(r.status, await r.json().catch(() => ({})), file.name)
    },
    pick: () => nativeHost.call('page.pickFiles', { limit: 9 }, { timeout: 0 }) as Promise<PickedFile[]>,
    async sendPicked(file, on) {
      const link = linker.link.get()
      if (link?.via === 'demo') return `~/.cache/herdr-anywhere/uploads/demo/${file.name}`
      const r = await nativeHost.call('page.uploadFile', { id: file.id, url: await url(file.name, on), token: link?.token ?? '' }, { timeout: 0 }) as { status: number; body: string }
      let body: any = {}
      try { body = JSON.parse(r.body) } catch {}
      return landed(r.status, body, file.name)
    },
  }
}

// A step's details as the computer could tell them (companion/internal/chat/chat.go: Run, Change, Read); anything not
// in that shape is left out, and the step shows its raw input and output instead.
const num = (x: unknown) => (typeof x === 'number' && Number.isFinite(x) ? x : undefined)
function runOf(x: any): ChatRun | undefined {
  if (!x || typeof x.command !== 'string' || typeof x.output !== 'string') return undefined
  return { command: x.command, failed: x.failed === true, ...(num(x.code) ? { code: x.code } : {}), output: x.output, lines: num(x.lines) ?? 0 }
}
function changeOf(x: any): ChatChange | undefined {
  if (!x || typeof x.file !== 'string' || !Array.isArray(x.hunks)) return undefined
  const hunks = x.hunks.filter((h: any) => h && num(h.oldStart) !== undefined && num(h.newStart) !== undefined && Array.isArray(h.lines) && h.lines.every((l: unknown) => typeof l === 'string'))
    .map((h: any) => ({ oldStart: h.oldStart, newStart: h.newStart, lines: h.lines }))
  return { file: x.file, added: num(x.added) ?? 0, deleted: num(x.deleted) ?? 0, hunks, ...(x.cut === true ? { cut: true } : {}) }
}
function readOf(x: any): ChatRead | undefined {
  if (!x || typeof x.file !== 'string' || num(x.from) === undefined || num(x.count) === undefined || num(x.total) === undefined) return undefined
  return { file: x.file, from: x.from, count: x.count, total: x.total }
}
