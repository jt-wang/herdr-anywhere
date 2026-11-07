// The web page the app runs in (web-host.js): its address, and dropping a used pairing code from it. Elsewhere, none.
import { host } from 'nsh'

export type Page = { href: string; origin: string }

export async function pageAddress(): Promise<Page | null> {
  if (!host.available()) return null
  try {
    const p: any = await host.call('page.address')
    return typeof p?.href === 'string' && typeof p?.origin === 'string' && /^https?:/.test(p.origin) ? p : null
  } catch {
    return null
  }
}

export function dropPairFromPage() {
  if (host.available()) host.call('page.dropPair').catch(() => {})
}

/** The browser's word for the system's notifications: what the person allowed, or "unsupported" (no page, no API). */
export type Permission = 'granted' | 'denied' | 'default' | 'unsupported'
const permission = (p: unknown): Permission => (p === 'granted' || p === 'denied' || p === 'default' ? p : 'unsupported')

export async function notifications(): Promise<Permission> {
  if (!host.available()) return 'unsupported'
  return host.call('page.notifications').then(permission, () => 'unsupported' as const)
}

export async function allowNotifications(): Promise<Permission> {
  if (!host.available()) return 'unsupported'
  return host.call('page.allowNotifications').then(permission, () => 'unsupported' as const)
}

/** Posts the system's notification for an alert; the page posts it only while it is not in front. A finished agent's
 *  (`done`) is the quieter kind where the host has one (Android: a channel without sound). */
/** `quiet`: new words for the notification already posted for this terminal; nothing when it was dismissed. */
export function notifyFromPage(n: { title: string; body?: string; terminal?: string; kind: 'blocked' | 'done'; quiet?: boolean }) {
  if (host.available()) host.call('page.notify', { title: n.title, body: n.body ?? null, terminal: n.terminal ?? null, kind: n.kind, quiet: !!n.quiet }).catch(() => {})
}

/** What Android does with each kind of notification, as its channel stands now (MainActivity): with a pop-up and a
 *  sound, a sound only, silent, or turned off there. `null` where there are no channels (iOS, a browser, an older app). */
export type ChannelState = 'alert' | 'sound' | 'silent' | 'off'
export type ChannelKind = 'blocked' | 'done' | 'connection'
export type Channels = Record<ChannelKind, ChannelState>
const STATES: ChannelState[] = ['alert', 'sound', 'silent', 'off']
export async function notifyChannels(): Promise<Channels | null> {
  if (!host.available()) return null
  try {
    const c: any = await host.call('page.notifyChannels')
    const ok = c && (['blocked', 'done', 'connection'] as const).every((k) => STATES.includes(c[k]))
    return ok ? { blocked: c.blocked, done: c.done, connection: c.connection } : null
  } catch {
    return null
  }
}
/** Opens the page in Android's settings for one kind, or for the whole app (`app`). */
export function openChannel(kind: ChannelKind | 'app') {
  if (host.available()) host.call('page.openChannel', { kind }).catch(() => {})
}
/** Calls `back` each time the app comes back in front, as from Android's settings. Returns the stop. */
export function onFront(back: () => void): () => void {
  if (!host.available()) return () => {}
  return host.on('page.front', (d: any) => { if (d?.front === true) back() })
}

/** Follows whether the page is in front (`front`), and taps on its notifications (`open`). Returns the stop. */
export function followPage(on: { front: (front: boolean) => void; open: (terminal: string) => void }): () => void {
  if (!host.available()) return () => {}
  host.call('page.front').then((f) => { if (typeof f === 'boolean') on.front(f) }, () => {})
  const a = host.on('page.front', (d: any) => { if (typeof d?.front === 'boolean') on.front(d.front) })
  const b = host.on('page.open', (d: any) => { if (typeof d?.terminal === 'string') on.open(d.terminal) })
  return () => { a(); b() }
}

/** Android ends a phone's connection soon after the app leaves the screen, unless a foreground service keeps it running:
 *  while there is a computer to stay connected to, and the person wants it. */
export function wantsKeepAlive(conn: string, on: boolean): boolean {
  return on && conn !== 'idle' && conn !== 'unpaired'
}
export function keepAlive(on: boolean) {
  if (host.available()) host.call('page.keepAlive', { on }).catch(() => {})
}
/** Gives the terminal's WebView the system's focus, which the composer may hold: Android brings the keyboard up for the
 *  page only then. Elsewhere there is nothing to do (no such call). */
export function focusTerminalView(): Promise<unknown> {
  return host.available() ? host.call('page.focusTerminal').catch(() => {}) : Promise.resolve()
}

/** Asks the system for the keyboard once the page's field has the typing focus. Android shows none for a field a page
 *  focused on its own, only for one the person touched; the menu's Type in the terminal is a touch outside the page. */
export function showTerminalKeyboard() {
  if (host.available()) host.call('page.showKeyboard').catch(() => {})
}

/** Downloads the Android app and hands it to the system, which asks the person to confirm and refuses a file not signed
 *  with this app's key. "needsPermission": this app may not install yet; with ask, the system's screen for allowing that opened. */
export async function installApp(app: { url: string; digest: string; bytes: number }, progress: (done: number) => void, ask = true): Promise<'installing' | 'needsPermission'> {
  if (!host.available()) throw new Error('no host')
  const off = host.on('page.updateProgress', (d: any) => { if (typeof d?.done === 'number') progress(d.done) })
  try {
    const r: any = await host.call('page.installUpdate', { url: app.url, digest: app.digest, ask })
    return r === 'needsPermission' ? 'needsPermission' : 'installing'
  } finally {
    off()
  }
}

/** The system refused an update after the person confirmed it. Returns the stop. */
export function followInstallFailures(failed: (message: string) => void): () => void {
  if (!host.available()) return () => {}
  return host.on('page.updateFailed', (d: any) => failed(String(d?.message ?? '')))
}
