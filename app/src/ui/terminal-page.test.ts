// The terminal's web page and the app's side of it.
import { expect, test } from 'nsh/test'
import { PageChannel, TERMINAL_FONTS, ctrlKey, readPageMessage, terminalPage, webViewScrollable, xtermTheme } from './terminal-page.ts'
import { KittyImages } from '../model/kitty.ts'
import debug from '../../fixtures/kitty/omp-18.18.3-debug.json'

test('page messages are read strictly', () => {
  expect(readPageMessage('{"t":"ready","cols":80,"rows":24}')).toEqual({ t: 'ready', cols: 80, rows: 24 })
  expect(readPageMessage('{"t":"size","cols":0,"rows":24}')).toBe(undefined)
  expect(readPageMessage('{"t":"input","data":"ls"}')).toEqual({ t: 'input', data: 'ls' })
  expect(readPageMessage('{"t":"copied","text":"abc"}')).toEqual({ t: 'copied', text: 'abc' })
  expect(readPageMessage('{"t":"swipe","dir":"next"}')).toEqual({ t: 'swipe', dir: 'next' })
  expect(readPageMessage('{"t":"swipe","dir":"up"}')).toBe(undefined)
  expect(readPageMessage('{"t":"link","url":"https://example.com/a"}')).toEqual({ t: 'link', url: 'https://example.com/a' })
  expect(readPageMessage('{"t":"link","url":"javascript:alert(1)"}')).toBe(undefined)
  expect(readPageMessage('{"t":"image","path":"shots/a b.png","id":4}')).toEqual({ t: 'image', path: 'shots/a b.png', id: 4 })
  expect(readPageMessage('{"t":"paths","paths":["a.png","b/c.png"]}')).toEqual({ t: 'paths', paths: ['a.png', 'b/c.png'] })
  expect(readPageMessage(JSON.stringify({ t: 'paths', paths: Array.from({ length: 65 }, (_, i) => i + '.png') }))).toBe(undefined)
  expect(readPageMessage('{"t":"paths","paths":[""]}')).toBe(undefined)
  expect(readPageMessage('{"t":"image","path":""}')).toBe(undefined)
  expect(readPageMessage('{"t":"image","path":1}')).toBe(undefined)
  expect(readPageMessage(JSON.stringify({ t: 'image', path: '/' + 'a'.repeat(1024) + '.png' }))).toBe(undefined)
  expect(readPageMessage('{"t":"tap"}')).toEqual({ t: 'tap' })
  expect(readPageMessage('nope')).toBe(undefined)
})

test('output waits for the page, comes as one message per flush, and a split UTF-8 character arrives whole', () => {
  const posted: string[] = []
  const timers: (() => void)[] = []
  const ch = new PageChannel((m) => posted.push(m), (f) => { timers.push(f); return 0 })
  const euro = new TextEncoder().encode('€!')
  ch.write(euro.slice(0, 2))
  ch.write(euro.slice(2))
  expect(posted).toEqual([])
  ch.ready()
  timers.shift()!()
  expect(posted.map((p) => JSON.parse(p))).toEqual([{ t: 'write', data: '€!' }])
})

test('a lost page drops what was queued and waits for ready again', () => {
  const posted: string[] = []
  const timers: (() => void)[] = []
  const ch = new PageChannel((m) => posted.push(m), (f) => { timers.push(f); return 0 })
  ch.ready()
  ch.write(new TextEncoder().encode('gone'))
  ch.lost()
  ch.write(new TextEncoder().encode('x'))
  for (const f of timers.splice(0)) f()
  expect(posted).toEqual([])
  ch.ready()
  for (const f of timers.splice(0)) f()
  expect(posted.map((p) => JSON.parse(p).data)).toEqual(['x'])
})

test('Ctrl from the key bar turns the next letter into its control character', () => {
  expect(ctrlKey('c')).toBe('\x03')
  expect(ctrlKey('C')).toBe('\x03')
  expect(ctrlKey('[')).toBe('\x1b')
  expect(ctrlKey(' ')).toBe('\x00')
  expect(ctrlKey('ab')).toBe('ab')
})

test("the terminal takes herdr's palette: graphite in the dark, paper in the light", () => {
  expect(xtermTheme(true).background).toBe('#17191d')
  // xterm's default is white: a bright line down a dark terminal's edge once it has been resized.
  expect(xtermTheme(true).overviewRulerBorder).toBe('#17191d')
  expect(xtermTheme(false).overviewRulerBorder).toBe('#ffffff')
  expect(xtermTheme(true).blue).toBe('#96b2d7')
  expect(xtermTheme(true).black).toBe('#45484f')
  expect(xtermTheme(false).background).toBe('#ffffff')
  expect(xtermTheme(false).brightWhite).toBe('#111214')
})

test('the page is self-contained: xterm inline, nothing loaded from anywhere', () => {
  const html = terminalPage({ dark: true, fontSize: 13, scrollback: 2000 })
  expect(/<script[^>]*\ssrc=|<link\b|@import|<iframe|<img/i.test(html)).toBe(false)
  expect(html.includes('"fontSize":13')).toBe(true)
})

test("the page draws as Herdr's terminal: JetBrains Mono medium with the Nerd symbols, line height 1.15, padding 14 by 12", () => {
  const html = terminalPage({ dark: true, fontSize: 13, scrollback: 2000 })
  expect(html.includes("@font-face{font-family:'JetBrains Mono';src:url(data:font/woff;base64,")).toBe(true)
  expect(html.includes("@font-face{font-family:'Symbols Nerd Font';src:url(data:font/woff;base64,")).toBe(true)
  expect(html.includes('"lineHeight":1.15')).toBe(true)
  expect(html.includes('"fontWeight":500')).toBe(true)
  expect(html.includes('#t{position:absolute;inset:12px 14px}')).toBe(true)
  // ⏺ ⏸ ✔ ⚠ and the like are text by default: drawn from Noto Sans Symbols 2 in the terminal's colours, not as
  // Android's colour emoji. Characters that are emoji by default (✅ ⏳) are left out of it and stay emoji.
  expect(html.includes("@font-face{font-family:'Noto Sans Symbols 2';src:url(data:font/woff;base64,")).toBe(true)
  expect(TERMINAL_FONTS.startsWith("'JetBrains Mono', 'Symbols Nerd Font', 'Noto Sans Symbols 2', ")).toBe(true)
  expect(html.includes('fontFamily: cfg.fonts')).toBe(true)
  // iOS 26 has no ⎿ or Braille of its own: these come with the page (e2e/ios-glyphs.sh draws them on a simulator).
  expect(html.includes("@font-face{font-family:'Noto Sans Symbols';src:url(data:font/woff;base64,")).toBe(true)
  expect(html.includes("'Noto Sans Symbols'\", '\\u23bf')")).toBe(true)
  expect(html.includes("@font-face{font-family:'Noto Sans Mono';src:url(data:font/woff;base64,")).toBe(true)
  expect(html.includes("'Noto Sans Mono'\", '\\u23ba')")).toBe(true)
  // Box drawing and blocks are drawn by the WebGL renderer, so they join across rows at that line height.
  expect(html.includes('WebglAddon')).toBe(true)
})

test('the keyboard does not come up when the page opens (e2e/page-touch.sh: nor on a tap or a drag)', () => {
  const html = terminalPage({ dark: true, fontSize: 13, scrollback: 2000 })
  expect(html.includes("setAttribute('inputmode','none')")).toBe(true)
})

test('a phone gives the terminal the whole width; a wide pane keeps the padding', () => {
  expect(terminalPage({ dark: true, fontSize: 13, scrollback: 2000, phone: true }).includes('#t{position:absolute;inset:12px 0}')).toBe(true)
  expect(terminalPage({ dark: true, fontSize: 13, scrollback: 2000 }).includes('#t{position:absolute;inset:12px 14px}')).toBe(true)
})

test("on Android the WebView is left scrollable, so the page gets finger moves: drag and selection need them", () => {
  // NSH's Android WebView swallows every move while it is not scrollable; the page saw touchstart and touchend only.
  expect(webViewScrollable('android')).toBe(true)
  expect(webViewScrollable('ios')).toBe(false)
})

// Oh My Pi's pictures (fixtures/kitty): the page gets each picture as a message of its own, before the text whose
// placeholder cells show it, and the terminal's pictures again whenever the page starts over.
test('a picture reaches the page before the placeholders that show it, and no graphics command reaches xterm', () => {
  const posted: any[] = []
  const timers: (() => void)[] = []
  const flush = () => { for (const f of timers.splice(0)) f() }
  const ch = new PageChannel((m) => posted.push(JSON.parse(m)), (f) => { timers.push(f); return 0 }, new KittyImages())
  ch.ready()
  for (const w of [...debug.open, ...debug.live]) { ch.write(new TextEncoder().encode(w)); flush() }
  const pic = posted.findIndex((m) => m.t === 'kitty')
  const shown = posted.findIndex((m) => m.t === 'write' && m.data.includes('\u{10EEEE}'))
  expect([posted[pic].t, posted[pic].id, posted[pic].cols, posted[pic].rows]).toEqual(['kitty', 13180035, 20, 7])
  expect(posted[pic].png.startsWith('iVBORw0KGgo')).toBe(true)
  expect(pic >= 0 && pic < shown).toBe(true)
  expect(posted.some((m) => m.t === 'write' && m.data.includes('\x1b_G'))).toBe(false)
})

test('a pane opened again gets its pictures after the reset, before the one screen herdr-core sends', () => {
  const posted: any[] = []
  const timers: (() => void)[] = []
  const flush = () => { for (const f of timers.splice(0)) f() }
  const ch = new PageChannel((m) => posted.push(JSON.parse(m)), (f) => { timers.push(f); return 0 }, new KittyImages())
  ch.ready()
  for (const w of [...debug.open, ...debug.live]) ch.write(new TextEncoder().encode(w))
  flush()
  posted.length = 0
  ch.reset()
  for (const w of debug.back) ch.write(new TextEncoder().encode(w))
  flush()
  expect(posted.map((m) => m.t)).toEqual(['reset', 'kitty', 'write'])
  expect(posted[1].id).toBe(13180035)
  // A page loaded again starts with the pictures too.
  posted.length = 0
  ch.lost()
  ch.ready()
  flush()
  expect(posted.map((m) => [m.t, m.id])).toEqual([['kitty', 13180035]])
})

test('a deleted picture is deleted on the page too', () => {
  const posted: any[] = []
  const timers: (() => void)[] = []
  const ch = new PageChannel((m) => posted.push(JSON.parse(m)), (f) => { timers.push(f); return 0 }, new KittyImages())
  ch.ready()
  for (const w of [...debug.open, ...debug.live]) ch.write(new TextEncoder().encode(w))
  ch.write(new TextEncoder().encode('\x1b_Ga=d,d=A,q=2\x1b\\'))
  for (const f of timers.splice(0)) f()
  expect(posted.filter((m) => m.t.startsWith('kitty')).map((m) => m.t)).toEqual(['kitty-drop', 'kitty', 'kitty-drop']) // omp starts by deleting all
  expect(posted.at(-1)).toEqual({ t: 'kitty-drop' })
})
