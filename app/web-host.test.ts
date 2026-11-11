// The page's side of alerts (web-host.js) over a stand-in browser: whether the page is in front, and the system's
// notifications it posts while it is not.
import { expect, test } from 'nsh/test'
import { createHost } from './web-host.js'

function browser(permission = 'granted') {
  const listeners = new Map<string, (() => void)[]>()
  const on = (type: string, f: () => void) => listeners.set(type, [...(listeners.get(type) ?? []), f])
  const fire = (type: string) => { for (const f of listeners.get(type) ?? []) f() }
  const posted: any[] = []
  const page = { visibilityState: 'visible', focused: true }
  class FakeNotification {
    static permission = permission
    static requestPermission = async () => { FakeNotification.permission = 'granted'; return 'granted' }
    onclick: (() => void) | null = null
    closed = false
    constructor(public title: string, public options: any) { posted.push(this) }
    close() { this.closed = true }
  }
  const g = globalThis as any
  g.document = { get visibilityState() { return page.visibilityState }, hasFocus: () => page.focused, addEventListener: on }
  g.addEventListener = on
  g.focus = () => {}
  g.Notification = FakeNotification
  const events: { name: string; data: any }[] = []
  const host = createHost({ hostEvent: (name: string, data: any) => events.push({ name, data }) })
  return { host, page, fire, posted, events, Notification: FakeNotification }
}

test('the page says whether it is in front, and tells the app when that changes', () => {
  const b = browser()
  expect(b.host.call('page.front')).toBe(true)
  b.page.visibilityState = 'hidden'
  b.fire('visibilitychange')
  b.fire('blur')
  b.page.visibilityState = 'visible'
  b.fire('visibilitychange')
  expect(b.events).toEqual([{ name: 'page.front', data: { front: false } }, { name: 'page.front', data: { front: true } }])
})

test('a page in another tab posts the notification; tapping it brings the page and the agent back', () => {
  const b = browser()
  b.page.visibilityState = 'hidden'
  expect(b.host.call('page.notify', { title: 'Claude Code needs you', body: 'api / fix login', terminal: 't1' })).toBe(true)
  expect(b.posted.map((n) => [n.title, n.options.body, n.options.tag])).toEqual([['Claude Code needs you', 'api / fix login', 't1']])
  b.posted[0].onclick()
  expect(b.posted[0].closed).toBe(true)
  expect(b.events).toEqual([{ name: 'page.open', data: { terminal: 't1' } }])
})

test('a page in front, or one the person did not allow, posts nothing', () => {
  const front = browser()
  expect(front.host.call('page.notify', { title: 'x', terminal: 't1' })).toBe(false)
  expect(front.posted).toEqual([])
  const denied = browser('denied')
  denied.page.focused = false
  expect(denied.host.call('page.notify', { title: 'x', terminal: 't1' })).toBe(false)
  expect(denied.posted).toEqual([])
})

test('the app reads and asks for the permission through the page', async () => {
  const b = browser('default')
  expect(b.host.call('page.notifications')).toBe('default')
  expect(await b.host.call('page.allowNotifications')).toBe('granted')
  expect(b.host.call('page.notifications')).toBe('granted')
  delete (globalThis as any).Notification
  expect(b.host.call('page.notifications')).toBe('unsupported')
})

test('new words for a posted notification replace it without a sound; a closed one stays closed', () => {
  const b = browser()
  b.page.visibilityState = 'hidden'
  b.host.call('page.notify', { title: 'Claude Code finished', body: 'Sky color reply', terminal: 't1' })
  expect(b.host.call('page.notify', { title: 'Claude Code finished', body: 'replied', terminal: 't1', quiet: true })).toBe(true)
  expect(b.posted.map((n) => [n.options.body, n.options.tag, !!n.options.silent])).toEqual([['Sky color reply', 't1', false], ['replied', 't1', true]])
  b.posted[1].onclick()
  expect(b.host.call('page.notify', { title: 'Claude Code finished', body: 'later', terminal: 't1', quiet: true })).toBe(false)
  expect(b.posted.length).toBe(2)
})

// page.copyImage (src/page.ts copyPicture): the browser's own clipboard, as NSH 0.3.0's writeImage writes nothing in a
// browser. Its bytes as a Blob: the page's CSP blocks fetching a data: URL.
function clipboard(o: { supports?: (t: string) => boolean; canvas?: boolean } = {}) {
  const written: { types: string[]; blob: any }[] = []
  const g = globalThis as any
  g.ClipboardItem = class { static supports = o.supports; constructor(public items: Record<string, any>) {} }
  g.navigator = { clipboard: { write: async (items: any[]) => { for (const i of items) for (const [type, blob] of Object.entries(i.items)) written.push({ types: [type], blob }) } } }
  g.createImageBitmap = o.canvas ? async () => ({ width: 3, height: 2, close() {} }) : undefined
  g.OffscreenCanvas = o.canvas ? class { constructor(public width: number, public height: number) {} getContext() { return { drawImage() {} } } async convertToBlob(x: any) { return new Blob([new Uint8Array([1, 2, 3])], { type: x.type }) } } : undefined
  return { written, host: browser().host }
}
const PNG = 'data:image/png;base64,iVBORw0KGgo='

test('a picture goes on the clipboard as PNG, from its own bytes', async () => {
  const c = clipboard()
  expect(await c.host.call('page.copyImage', { url: PNG })).toBe(true)
  expect(c.written.map((w) => w.types[0])).toEqual(['image/png'])
  expect(c.written[0].blob.size).toBe(8)
  expect(c.written[0].blob.type).toBe('image/png')
})

test('a JPEG goes as it is where the browser takes it, else drawn as PNG, else not at all', async () => {
  const jpeg = 'data:image/jpeg;base64,/9j/4AAQ'
  const takes = clipboard({ supports: (t) => t === 'image/jpeg' })
  expect(await takes.host.call('page.copyImage', { url: jpeg })).toBe(true)
  expect(takes.written.map((w) => w.types[0])).toEqual(['image/jpeg'])
  const drawn = clipboard({ supports: (t) => t === 'image/png', canvas: true })
  expect(await drawn.host.call('page.copyImage', { url: jpeg })).toBe(true)
  expect(drawn.written.map((w) => w.types[0])).toEqual(['image/png'])
  const neither = clipboard({ supports: (t) => t === 'image/png' })
  expect(await neither.host.call('page.copyImage', { url: jpeg })).toBe(false)
})

test('no picture, or a browser without the clipboard, copies nothing', async () => {
  const c = clipboard()
  expect(await c.host.call('page.copyImage', { url: 'https://example.com/a.png' })).toBe(false)
  expect(await c.host.call('page.copyImage', {})).toBe(false)
  ;(globalThis as any).navigator = {}
  expect(await c.host.call('page.copyImage', { url: PNG })).toBe(false)
  expect(c.written).toEqual([])
})
