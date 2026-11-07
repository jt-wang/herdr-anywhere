// The terminal page in a real browser with real touch events (Chromium over CDP, so a tap makes the same mouse events
// a phone's WebView makes, which is what xterm focuses on). Run with `e2e/page-touch.sh`.
import { createRequire } from 'node:module'
import { terminalPage } from '../src/ui/terminal-page.ts'

const require = createRequire(process.env.PLAYWRIGHT_DIR + '/')
const { chromium } = require('./index.js')

const browser = await chromium.launch()
const failures = []
const check = (name, ok, seen) => { console.log(`${ok ? 'ok' : 'FAIL'}: ${name}${seen === undefined ? '' : ` (${JSON.stringify(seen)})`}`); if (!ok) failures.push(name) }

async function open(opts) {
  const context = await browser.newContext({ viewport: { width: 390, height: 700 }, hasTouch: true, isMobile: true, deviceScaleFactor: 3 })
  const page = await context.newPage()
  await page.addInitScript(() => { window.posts = []; window.nsh = { postMessage: (m) => window.posts.push(JSON.parse(m)) }; window.addEventListener('error', (e) => { window.pageError = e.message })
    // The page keeps its terminal in a closure; a test that needs the view's exact line takes it as xterm's bundle sets window.Terminal.
    let T; Object.defineProperty(window, 'Terminal', { configurable: true, get: () => T, set: (v) => { T = class extends v { constructor(...a) { super(...a); window.term = this } } } }) })
  await page.route('http://terminal.test/', (r) => r.fulfill({ contentType: 'text/html', body: terminalPage({ dark: true, fontSize: 13, scrollback: 2000, ...opts }) }))
  await page.goto('http://terminal.test/')
  await page.waitForFunction(() => window.posts.some((m) => m.t === 'ready'))
  const cdp = await context.newCDPSession(page)
  const touch = (type, x, y) => cdp.send('Input.dispatchTouchEvent', { type, touchPoints: type === 'touchEnd' ? [] : [{ x, y }] })
  return {
    page,
    touch,
    tap: async (x, y) => { await touch('touchStart', x, y); await touch('touchEnd'); await page.waitForTimeout(400) },
    drag: async (x, y, dy) => {
      await touch('touchStart', x, y)
      for (let i = 1; i <= 6; i++) await touch('touchMove', x, y + (dy * i) / 6)
      await touch('touchEnd')
      await page.waitForTimeout(400)
    },
    send: (m) => page.evaluate((m) => window.postMessage(JSON.stringify(m), '*'), m),
    asks: () => page.evaluate(() => {
      const ta = document.querySelector('.xterm-helper-textarea')
      return document.activeElement === ta && ta.getAttribute('inputmode') !== 'none'
    }),
    size: () => page.evaluate(() => {
      const s = document.querySelector('.xterm-screen').getBoundingClientRect()
      return { left: s.left, right: innerWidth - s.right, cols: [...window.posts].reverse().find((m) => m.cols).cols }
    }),
    away: () => page.evaluate(() => window.posts.filter((m) => m.t === 'away').at(-1)?.away ?? false),
  }
}

// Scrolling keeps going after the finger lifts, slower and slower. The scrollbar's position is the page's only
// visible record of where the view is: it must change while nothing touches the screen.
{
  const t = await open({ phone: true })
  await t.send({ t: 'write', data: Array.from({ length: 1000 }, (_, i) => `line ${i}`).join('\r\n') })
  await t.page.waitForTimeout(300)
  const y = () => t.page.evaluate(() => parseFloat(document.querySelector('.scrollbar.vertical .slider').style.top))
  // The last move comes right before the caller lifts: the page glides only when the finger lifts within 60 ms of its
  // last move, which a wait after it plus a slow machine (GitHub's macOS runner, 2026-11-06) can pass.
  const stroke = async (from, step, gap = 16, n = 8) => {
    await t.touch('touchStart', 195, from)
    for (let i = 1; i <= n; i++) { if (i > 1) await t.page.waitForTimeout(gap); await t.touch('touchMove', 195, from + i * step) }
  }
  const watch = async (ms, every = 100) => {
    const out = [await y()]
    for (let at = 0; at < ms; at += every) { await t.page.waitForTimeout(every); out.push(await y()) }
    return out
  }

  await stroke(150, 40)
  await t.touch('touchEnd')
  const flick = await watch(3000)
  const steps = flick.slice(1).map((v, i) => Math.abs(v - flick[i]))
  const moving = steps.filter((s) => s > 0)
  check('a flick keeps scrolling after the finger lifts', flick[0] !== flick[flick.length - 1], flick.join(' '))
  const moved = (a, b) => steps.slice(a, b).reduce((n, v) => n + v, 0)
  // The scrollbar moves in whole pixels, so two steps say little: compare the first half second with the next.
  check('it slows down as it goes', moving.length >= 4 && moved(0, 5) > moved(5, 10), steps.join(' '))
  check('and comes to rest', steps.slice(-5).every((s) => s === 0), steps.join(' '))

  await stroke(150, 20)
  await t.page.waitForTimeout(250)
  await t.touch('touchEnd')
  const held = await watch(500)
  check('a finger that stopped before lifting leaves the view where it is', new Set(held).size === 1, held.join(' '))

  await stroke(150, -40)
  await t.touch('touchEnd')
  await t.page.waitForTimeout(100)
  const posts = await t.page.evaluate(() => window.posts.length)
  await t.touch('touchStart', 195, 300)
  await t.page.waitForTimeout(50)
  const stopped = await watch(300)
  await t.touch('touchEnd')
  await t.page.waitForTimeout(100)
  check('a touch stops it where it is', new Set(stopped).size === 1, stopped.join(' '))
  check('and that touch is no tap', !(await t.page.evaluate((n) => window.posts.slice(n).some((m) => m.t === 'tap'), posts)))

  await stroke(150, 40)
  await t.touch('touchEnd')
  await t.send({ t: 'bottom' })
  await t.page.waitForTimeout(600)
  check('going to the bottom ends it there', !(await t.away()))

  await t.touch('touchStart', 195, 300)
  await t.touch('touchEnd')
  await t.page.waitForTimeout(100)
  await t.send({ t: 'write', data: '\x1b[?1049h\x1b[2J\x1b[Halt screen' })
  await t.page.waitForTimeout(100)
  await stroke(150, 40)
  await t.touch('touchEnd')
  await t.page.waitForTimeout(300)
  check('nothing to scroll: no error', !(await t.page.evaluate(() => window.pageError)))
}

// A full-screen program has no scrollback for the page to move (Pi 1.4.0 runs so, measured 2026-11-06): a drag has to
// reach the program, as a wheel when it follows the mouse and as arrow keys when it doesn't.
{
  const t = await open({ phone: true })
  const input = () => t.page.evaluate(() => { const d = window.posts.filter((m) => m.t === 'input').map((m) => m.data).join(''); window.posts = window.posts.filter((m) => m.t !== 'input'); return d })
  // A finger that stops before it lifts, so no flick follows.
  const run = async (dy) => {
    await t.page.waitForTimeout(100); await input()
    await t.touch('touchStart', 195, 300)
    for (let i = 1; i <= 6; i++) await t.touch('touchMove', 195, 300 + (dy * i) / 6)
    await t.page.waitForTimeout(150)
    await t.touch('touchEnd')
    await t.page.waitForTimeout(100)
    return input()
  }
  const count = (s, seq) => s.split(seq).length - 1
  await t.send({ t: 'write', data: '\x1b[?1049h\x1b[?1000h\x1b[?1003h\x1b[?1006h\x1b[2J\x1b[Hfull screen' })
  await t.page.waitForTimeout(100)
  const back = await run(200)
  check('a drag down asks a mouse program for what is above', /\x1b\[<64;\d+;\d+M/.test(back) && !/\x1b\[<65;/.test(back), back)
  const on = await run(-200)
  check('a drag up asks it for what is below', /\x1b\[<65;\d+;\d+M/.test(on) && !/\x1b\[<64;/.test(on), on)
  const wheels = count(back, '\x1b[<64;')
  const bit = await run(60)
  check('how far it goes follows the finger', count(bit, '\x1b[<64;') < wheels, [count(bit, '\x1b[<64;'), wheels])
  await t.page.waitForTimeout(100)
  await input()
  await t.touch('touchStart', 195, 150)
  for (let i = 1; i <= 8; i++) { await t.touch('touchMove', 195, 150 + i * 40); await t.page.waitForTimeout(16) }
  await t.touch('touchEnd')
  const during = await input()
  await t.page.waitForTimeout(1500)
  const after = await input()
  check('a flick keeps going after the finger lifts', count(after, '\x1b[<64;') > 0, [count(during, '\x1b[<64;'), count(after, '\x1b[<64;')])
  await t.page.waitForTimeout(1500)
  check('and comes to rest', (await input()) === '')
  await t.tap(195, 300)
  check('a tap is no wheel in the program', !/\x1b\[<6[45];/.test(await input()))
  check('the page never counts as scrolled back', !(await t.away()))

  await t.send({ t: 'write', data: '\x1b[?1003l\x1b[?1000l\x1b[?1006l' })
  await t.page.waitForTimeout(100)
  const arrows = await run(120)
  check('a full-screen program without a mouse gets arrow keys', count(arrows, '\x1b[A') > 1 && !arrows.includes('\x1b[<'), arrows)
  check('one for each row', Math.abs(count(arrows, '\x1b[A') - 7) <= 1, count(arrows, '\x1b[A'))
  await t.send({ t: 'write', data: '\x1b[?1h' })
  await t.page.waitForTimeout(100)
  check('in the form it asked for', count(await run(-120), '\x1bOB') > 1)
  await t.send({ t: 'write', data: '\x1b[?1l\x1b[?1049l' })
  await t.page.waitForTimeout(100)
  await t.send({ t: 'write', data: Array.from({ length: 300 }, (_, i) => `line ${i}`).join('\r\n') + '\x1b[?1000h\x1b[?1006h' })
  await t.page.waitForTimeout(200)
  check('on the main screen a program that follows the mouse gets it too', /\x1b\[<64;/.test(await run(200)))
  await t.send({ t: 'write', data: '\x1b[?1000l\x1b[?1006l' })
  await t.page.waitForTimeout(100)
  check('and without one nothing is sent', (await run(200)) === '')
  check('the drag moves the page back through its history', await t.away())
  await t.page.context().close()
}

// A program that follows the mouse gets wheel reports at the rate xterm.js sends them for a Mac trackpad: one per ~3
// rows of travel (consumeWheelEvent scales small pixel wheels by 0.3). OpenCode 1.19.0 moves 3 rows per report
// (scroll_speed's default); at one report per row of finger its content ran 3 times as far as the finger (36 rows
// for a 12.4-row drag, measured 2026-11-07). Every report of a gesture goes to the cell where
// the finger went down: that cell picks the panel, and OpenCode ignores a wheel over its prompt box, so a drag that
// ended low on the screen used to stop. Touch times are set (CDP timestamps become the events' timeStamp), so a
// flick's speed is the same on every run.
{
  const at = (t, i, gap) => ({ timestamp: t + (i * gap) / 1000 })
  const timed = (cdp, type, x, y, ts) => cdp.send('Input.dispatchTouchEvent', { type, touchPoints: type === 'touchEnd' ? [] : [{ x, y }], ...ts })
  // A finger from (x, y): moves of `step` px `gap` ms apart, then held `hold` ms (no glide) or lifted at speed.
  async function gesture(t, cdp, x, y, steps, gap, hold) {
    const t0 = Date.now() / 1000
    await timed(cdp, 'touchStart', x, y, at(t0, 0, gap))
    let at_y = y, i = 0
    for (const s of steps) { i++; at_y += s; await timed(cdp, 'touchMove', x, at_y, at(t0, i, gap)) }
    if (hold) { await t.page.waitForTimeout(hold); await timed(cdp, 'touchEnd', x, at_y, { timestamp: Date.now() / 1000 }) }
    else await timed(cdp, 'touchEnd', x, at_y, at(t0, i, gap))
  }
  const reports = (s) => [...s.matchAll(/\x1b\[<(6[45]);(\d+);(\d+)M/g)].map((m) => ({ up: m[1] === '64', col: +m[2], row: +m[3] }))

  const m = await open({ phone: true })
  const mcdp = await m.page.context().newCDPSession(m.page)
  const input = () => m.page.evaluate(() => { const d = window.posts.filter((p) => p.t === 'input').map((p) => p.data).join(''); window.posts = window.posts.filter((p) => p.t !== 'input'); return d })
  await m.send({ t: 'write', data: '\x1b[?1049h\x1b[?1000h\x1b[?1003h\x1b[?1006h\x1b[2J\x1b[Hfull screen' })
  await m.page.waitForTimeout(100)
  const h = await m.page.evaluate(() => { const r = document.querySelector('.xterm-screen').getBoundingClientRect(); return r.height / window.posts.find((p) => p.cols).rows })
  const top = await m.page.evaluate(() => document.querySelector('.xterm-screen').getBoundingClientRect().top)
  const rowAt = (y) => Math.floor((y - top) / h) + 1

  // A drag of 12.5 rows, held before lifting.
  await input()
  await gesture(m, mcdp, 120, 150, Array(10).fill((12.5 * h) / 10), 16, 300)
  await m.page.waitForTimeout(100)
  const drag = reports(await input())
  check('a drag of 12.5 rows sends a mouse program one report per 3 rows', drag.length >= 3 && drag.length <= 5 && drag.every((r) => r.up), drag.length)
  check('every report goes to the cell where the finger went down', drag.length > 0 && drag.every((r) => r.row === rowAt(150) && r.col === drag[0].col), drag.map((r) => `${r.col},${r.row}`).join(' '))

  // Leftover travel is kept within a gesture, both ways: 2 rows down and back up is no report.
  await gesture(m, mcdp, 120, 300, [h, h, -h, -h], 16, 300)
  await m.page.waitForTimeout(100)
  check('2 rows down and back up sends nothing', reports(await input()).length === 0)
  // ...and 2 rows, then 2 more in the same drag, is one report.
  await gesture(m, mcdp, 120, 300, [h, h, h, h], 16, 300)
  await m.page.waitForTimeout(100)
  check('4 rows in small moves send one report', reports(await input()).length === 1)

  // A flick at 2.5 px/ms ending low on the screen: the glide's reports stay at the start cell, and it goes as far
  // as the same flick in a shell's history.
  const s = await open({ phone: true })
  const scdp = await s.page.context().newCDPSession(s.page)
  await s.send({ t: 'write', data: Array.from({ length: 2000 }, (_, i) => `line ${i}`).join('\r\n') })
  await s.page.waitForTimeout(300)
  const viewY = () => s.page.evaluate(() => window.term.buffer.active.viewportY)
  const from = await viewY()
  await gesture(s, scdp, 195, 150, Array(8).fill(40), 16, 0)
  for (let last = -1, now = await viewY(); now !== last; last = now, now = await viewY()) await s.page.waitForTimeout(400)
  const shellRows = from - (await viewY())
  await input()
  await gesture(m, mcdp, 195, 150, Array(8).fill(40), 16, 0)
  await m.page.waitForTimeout(3000)
  const flick = reports(await input())
  const ratio = (flick.length * 3) / shellRows
  check('a flick in a mouse program goes as far as in a shell (3 rows a report)', ratio >= 0.85 && ratio <= 1.15, { reports: flick.length, shellRows: Math.round(shellRows), ratio: +ratio.toFixed(2) })
  check('its glide reports stay at the start cell, above the bottom rows', flick.length > 0 && flick.every((r) => r.row === rowAt(150)), [...new Set(flick.map((r) => r.row))])
  // The shell's own flick is as before: 2.5 px/ms halving about every 225 ms carries ~810 px, plus the 320 px drawn.
  check("the shell's flick is unchanged", Math.abs(shellRows * h - 1130) < 113, Math.round(shellRows * h))
  await s.page.context().close()
  await m.page.context().close()
}

// A full-screen program that follows the mouse has buttons only a click reaches: OpenCode's update dialog runs Skip and
// Update on a mouse release (dialog-update.tsx, onMouseUp; it turns on modes 1000, 1002, 1003 and 1006, measured
// 2026-11-07 with OpenCode 1.19.0 through herdr-core 0.1.18). A tap there is a click on the cell under the finger.
{
  const t = await open({ phone: true })
  const input = () => t.page.evaluate(() => { const d = window.posts.filter((m) => m.t === 'input').map((m) => m.data).join(''); window.posts = window.posts.filter((m) => m.t !== 'input'); return d })
  const at = (row, col) => t.page.evaluate(([row, col]) => {
    const r = document.querySelector('.xterm-screen').getBoundingClientRect(), ready = window.posts.find((m) => m.t === 'ready')
    return { x: r.left + (col + 0.5) * (r.width / ready.cols), y: r.top + (row + 0.5) * (r.height / ready.rows) }
  }, [row, col])
  const dialog = '\x1b[?1049h\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h\x1b[2J\x1b[12;4HUpdate available\x1b[18;30HSkip  Update'
  await t.send({ t: 'write', data: dialog })
  await t.page.waitForTimeout(150)
  await input()
  // "Skip" starts at row 18, column 30 (1-based): its second letter is the cell at row 17, column 30 counted from 0.
  let p = await at(17, 30)
  await t.tap(p.x, p.y)
  const click = await input()
  check('a tap on a button is a press and a release on its cell', click === '\x1b[<0;31;18M\x1b[<0;31;18m', click)
  check('the tap asks for no keyboard', !(await t.asks()))

  await t.touch('touchStart', p.x, p.y)
  for (let i = 1; i <= 6; i++) await t.touch('touchMove', p.x, p.y + i * 20)
  await t.page.waitForTimeout(150)
  await t.touch('touchEnd')
  await t.page.waitForTimeout(150)
  check('a drag presses no button', !/\x1b\[<[0-2];/.test(await input()))

  // The keyboard up lifts the screen: the click still lands on the cell the finger is on.
  await t.page.setViewportSize({ width: 390, height: 400 })
  await t.page.waitForTimeout(250)
  await input()
  p = await at(17, 36)
  await t.tap(p.x, p.y)
  const lifted = await input()
  check('under a lifted screen the click is on the cell shown under the finger', lifted === '\x1b[<0;37;18M\x1b[<0;37;18m', lifted)
  await t.page.setViewportSize({ width: 390, height: 700 })
  await t.page.waitForTimeout(250)

  // A program that asked for the oldest encoding gets it.
  await t.send({ t: 'write', data: '\x1b[?1006l' })
  await t.page.waitForTimeout(60)
  await input()
  p = await at(17, 30)
  await t.tap(p.x, p.y)
  const x10 = await input()
  check('without SGR the click comes in the X10 form', x10 === '\x1b[M' + String.fromCharCode(32, 32 + 31, 32 + 18) + '\x1b[M' + String.fromCharCode(32 + 3, 32 + 31, 32 + 18), JSON.stringify(x10))

  // A program that doesn't follow the mouse gets nothing from a tap: the shell, a pager.
  await t.send({ t: 'write', data: '\x1b[?1003l\x1b[?1002l\x1b[?1000l' })
  await t.page.waitForTimeout(60)
  await input()
  await t.tap(p.x, p.y)
  check('a program without the mouse gets nothing from a tap', (await input()) === '')
  await t.page.context().close()
}

// Every column that fits is used: a TUI cuts what doesn't fit, and on a phone its status line often ends at the last one.
for (const [width, scale] of [[360, 3], [384, 2.8125], [393, 3], [411, 2.625], [412, 2.625], [430, 3], [320, 2]]) {
  const context = await browser.newContext({ viewport: { width, height: 700 }, hasTouch: true, isMobile: true, deviceScaleFactor: scale })
  const page = await context.newPage()
  await page.addInitScript(() => { window.posts = []; window.nsh = { postMessage: (m) => window.posts.push(JSON.parse(m)) } })
  await page.route('http://terminal.test/', (r) => r.fulfill({ contentType: 'text/html', body: terminalPage({ dark: true, fontSize: 13, scrollback: 2000, phone: true }) }))
  await page.goto('http://terminal.test/')
  await page.waitForFunction(() => window.posts.some((m) => m.t === 'ready'))
  await page.waitForTimeout(100)
  const m = await page.evaluate(() => {
    const r = document.querySelector('.xterm-screen').getBoundingClientRect(), cols = [...window.posts].reverse().find((p) => p.cols).cols
    return { cols, cell: r.width / cols, wide: r.width, inner: innerWidth, posted: window.posts.filter((p) => p.t === 'ready' || p.t === 'size').length }
  })
  check(`${width} px at ${scale}: the columns fit, and no more would`, m.wide <= m.inner + 0.01 && m.inner - m.wide < m.cell - 0.01, m)
  await context.close()
}

// The terminal's height is a whole number of rows; what is left over, and the page's padding, sit under the last row.
// They show the terminal's own background, what the program paints in its last row: OpenCode paints its rows
// rgb(10,10,10) on the default dark theme's #17191d, and a 20 px band of the page's colour showed under them on a
// 411 dp phone (measured 2026-11-07).
{
  const zlib = await import('node:zlib')
  // The colour of one CSS pixel, from a screenshot (an 8-bit RGBA PNG, as Chromium writes them).
  const pixel = async (page, x, y) => {
    const png = await page.screenshot({ clip: { x, y, width: 1, height: 1 }, scale: 'css' })
    let at = 8, w = 0, idat = []
    while (at < png.length) {
      const len = png.readUInt32BE(at), type = png.toString('ascii', at + 4, at + 8)
      if (type === 'IHDR') w = png.readUInt32BE(at + 8)
      if (type === 'IDAT') idat.push(png.subarray(at + 8, at + 8 + len))
      at += 12 + len
    }
    const raw = zlib.inflateSync(Buffer.concat(idat))
    // The first pixel of the first row: after the filter byte, as is under every filter (nothing above it or left of it).
    if (w !== 1) throw new Error('unexpected PNG width ' + w)
    return [raw[1], raw[2], raw[3]]
  }
  const near = (a, b) => a.every((v, i) => Math.abs(v - b[i]) <= 2)
  const band = async (t) => {
    await t.page.waitForTimeout(300)
    const g = await t.page.evaluate(() => { const s = document.querySelector('.xterm-screen').getBoundingClientRect(); return { bottom: s.bottom, top: s.top, cellH: s.height / window.term.rows, right: s.right, h: innerHeight } })
    return {
      lastRow: await pixel(t.page, g.right - 4, g.bottom - g.cellH / 2),
      below: await pixel(t.page, g.right - 4, (g.bottom + g.h) / 2),
      bottomEdge: await pixel(t.page, g.right - 4, g.h - 1),
      firstRow: await pixel(t.page, g.right - 4, g.top + g.cellH / 2),
      above: await pixel(t.page, g.right - 4, g.top / 2),
    }
  }
  const program = (rgb) => `\x1b[?1049h\x1b[48;2;${rgb.join(';')}m\x1b[2J\x1b[H Build · Model\r\n\x1b[999;1H  ctrl+p commands`
  for (const dark of [true, false]) {
    const look = dark ? 'dark' : 'light'
    const t = await open({ phone: true, dark })
    const theme = dark ? [0x17, 0x19, 0x1d] : [0xff, 0xff, 0xff]
    await t.send({ t: 'write', data: 'Line 1 of the answer\r\n$ ' })
    let b = await band(t)
    check(`a shell (${look}): under the last row is its background, the theme's`, near(b.below, theme) && near(b.bottomEdge, theme) && near(b.lastRow, theme), b)
    const own = dark ? [10, 10, 10] : [250, 248, 240]
    await t.send({ t: 'write', data: program(own) })
    b = await band(t)
    check(`a program that paints its rows (${look}): under the last row is the colour of its last row`, near(b.lastRow, own) && near(b.below, own) && near(b.bottomEdge, own), b)
    check(`… and above the first row the colour of its first row`, near(b.firstRow, own) && near(b.above, own), b)
    // A palette colour (256-colour index 236 is rgb 48,48,48).
    await t.send({ t: 'write', data: '\x1b[48;5;236m\x1b[2J' })
    b = await band(t)
    check(`a palette background (${look}) fills the band too`, near(b.below, [48, 48, 48]), b)
    await t.send({ t: 'write', data: '\x1b[0m\x1b[?1049l' })
    b = await band(t)
    check(`the program gone (${look}), the band is the theme's again`, near(b.below, theme) && near(b.lastRow, theme), b)
    await t.send({ t: 'write', data: program(own) })
    await t.send({ t: 'theme', dark: !dark })
    b = await band(t)
    check(`a new theme (${look} → other) keeps the program's colour under its rows`, near(b.below, own), b)
    await t.page.context().close()
  }
  // A first row in another colour (a title bar): the top edge takes it, the rest of the page the last row's.
  {
    const t = await open({ phone: true })
    await t.send({ t: 'write', data: program([10, 10, 10]) + '\x1b[1;1H\x1b[48;2;40;60;90m\x1b[2K Title' })
    const b = await band(t)
    check('a title bar in the first row: above it its colour, under the last row the last row\'s', near(b.above, [40, 60, 90]) && near(b.below, [10, 10, 10]), b)
    await t.page.context().close()
  }
  // The wide layout (tablet, computer) has padding at the sides too.
  {
    const t = await open({})
    await t.send({ t: 'write', data: program([10, 10, 10]) })
    await t.page.waitForTimeout(300)
    const g = await t.page.evaluate(() => { const s = document.querySelector('.xterm-screen').getBoundingClientRect(); return { left: s.left, mid: (s.top + s.bottom) / 2, h: innerHeight } })
    const side = await pixel(t.page, g.left / 2, g.mid), under = await pixel(t.page, g.left / 2, g.h - 2)
    check('the wide layout: the side padding and the band show the program\'s colour', near(side, [10, 10, 10]) && near(under, [10, 10, 10]), { side, under })
    await t.page.context().close()
  }
}

// A pane that loses height to a split takes its rows from the new height; the keyboard's height never does this.
{
  const t = await open({ phone: true })
  const rows = () => t.page.evaluate(() => [...window.posts].reverse().find((m) => m.rows).rows)
  const before = await rows()
  await t.page.setViewportSize({ width: 390, height: 400 })
  await t.page.waitForTimeout(200)
  check('less height alone keeps the rows', (await rows()) === before, await rows())
  await t.send({ t: 'refit' })
  await t.page.waitForTimeout(400)
  check('asked to refit, the rows follow the new height', (await rows()) < before, await rows())
}

{
  const t = await open({ phone: true })
  await t.send({ t: 'write', data: Array.from({ length: 300 }, (_, i) => `line ${i}`).join('\r\n') })
  await t.page.waitForTimeout(200)

  await t.tap(195, 300)
  check('a tap on the terminal asks for no keyboard', !(await t.asks()))
  await t.drag(195, 200, 200)
  check('a drag scrolls back', await t.away())
  check('a drag asks for no keyboard', !(await t.asks()))
  await t.drag(195, 300, 4)
  check('a small drag asks for no keyboard', !(await t.asks()))

  await t.send({ t: 'focus' })
  await t.page.waitForTimeout(100)
  check('the app can still ask for the keyboard', await t.asks())
  // Android shows no keyboard for a field the page focused on its own: the page says it has, and the app asks.
  check('the page says the terminal has the typing focus', await t.page.evaluate(() => window.posts.some((p) => p.t === 'focused')))
  await t.page.evaluate(() => document.querySelector('.xterm-helper-textarea').blur())
  await t.tap(195, 300)
  check('after the keyboard went away a tap does not bring it back', !(await t.asks()))

  const s = await t.size()
  check('a phone has no gap at the left', s.left === 0, s)
  check('a phone leaves less than a cell at the right', s.right < 8.5, s)
  await t.page.context().close()
}
{
  // The keyboard takes height from the page. A TUI asked to redraw for it prints what it shows again.
  const t = await open({ phone: true })
  const sizes = () => t.page.evaluate(() => window.posts.filter((m) => m.t === 'size').length)
  const bottomOf = (word) => t.page.evaluate((word) => {
    const screen = document.querySelector('.xterm-screen').getBoundingClientRect()
    const rows = [...document.querySelectorAll('.xterm-rows > div')]
    window.term0 = rows.length
    return { top: screen.top, height: screen.height, inner: innerHeight }
  }, word)
  const before = await sizes()
  await t.send({ t: 'write', data: Array.from({ length: 100 }, (_, i) => `line ${i}`).join('\r\n') })
  await t.page.waitForTimeout(150)
  await t.page.setViewportSize({ width: 390, height: 400 })
  await t.page.waitForTimeout(250)
  check('the keyboard going up does not resize the terminal', (await sizes()) === before, await sizes())
  let r = await bottomOf()
  check('with the screen full its last rows stay on screen', Math.abs(r.top + r.height - r.inner) < 30, r)
  await t.page.setViewportSize({ width: 390, height: 700 })
  await t.page.waitForTimeout(250)
  check('the keyboard going down does not resize the terminal', (await sizes()) === before, await sizes())
  r = await bottomOf()
  check('the keyboard gone, the screen is back at the top', r.top === 12, r)
  await t.page.setViewportSize({ width: 500, height: 700 })
  await t.page.waitForTimeout(250)
  check('a new width does resize', (await sizes()) === before + 1, await sizes())
  await t.page.context().close()
}
{
  // A short screen: what it shows sits at the top, so the keyboard must not move it away.
  const t = await open({ phone: true })
  await t.send({ t: 'write', data: 'one\r\ntwo\r\n' })
  await t.page.waitForTimeout(150)
  await t.page.setViewportSize({ width: 390, height: 400 })
  await t.page.waitForTimeout(250)
  const top = await t.page.evaluate(() => document.querySelector('.xterm-screen').getBoundingClientRect().top)
  check('a short screen stays where it is under the keyboard', top === 12, top)
  await t.page.context().close()
}
{
  // A TUI that drew less than before (Claude Code after a tool's output folds up) leaves empty rows under its last
  // line. The view ends at that line, with the history above it on screen instead of the empty rows.
  const t = await open({ phone: true })
  const rows = await t.page.evaluate(() => window.posts.find((m) => m.t === 'ready').rows)
  const shown = async () => {
    await t.page.evaluate(() => { window.posts = window.posts.filter((m) => m.t !== 'copied') })
    await t.send({ t: 'copy' })
    await t.page.waitForTimeout(60)
    return (await t.page.evaluate(() => window.posts.find((m) => m.t === 'copied').text)).split('\n')
  }
  await t.send({ t: 'write', data: Array.from({ length: 100 }, (_, i) => `line ${i}`).join('\r\n') + '\r\n' + 'drawn\r\n'.repeat(12) })
  await t.send({ t: 'write', data: '\x1b[12A\x1b[Jprompt' })
  await t.page.waitForTimeout(150)
  let s = await shown()
  check('empty rows under the last line give way to history', s.length === rows && s.at(-1) === 'prompt' && s.at(-2) === 'line 99', { rows, last: s.slice(-2), n: s.length })
  check('the view at the last line is the live bottom', !(await t.away()))
  await t.send({ t: 'write', data: '\r\nmore' })
  await t.page.waitForTimeout(150)
  s = await shown()
  check('new lines keep the view at the last line', s.length === rows && s.at(-1) === 'more' && s.at(-2) === 'prompt', { n: s.length, last: s.slice(-2) })
  await t.drag(200, 200, 200)
  check('scrolling back from there is away', await t.away())
  await t.send({ t: 'bottom' })
  await t.page.waitForTimeout(100)
  s = await shown()
  check('latest goes back to the last line', s.length === rows && s.at(-1) === 'more' && !(await t.away()), { n: s.length, last: s.slice(-1) })
  // A full-screen program owns every row: nothing of the history shows.
  await t.send({ t: 'write', data: '\x1b[?1049h\x1b[Hfull' })
  await t.page.waitForTimeout(150)
  s = await shown()
  check('a full-screen program keeps its empty rows', s.length === 1 && s[0] === 'full', s.slice(0, 2))
  await t.page.context().close()
}
{
  const t = await open({ phone: true })
  const pressed = async (m) => {
    await t.page.evaluate(() => { window.posts = [] })
    await t.send({ t: 'key', ...m })
    await t.page.waitForTimeout(60)
    return t.page.evaluate(() => window.posts.filter((p) => p.t === 'input').map((p) => p.data).join(''))
  }
  check('enter', (await pressed({ name: 'enter' })) === '\r')
  check('ctrl+enter for a program that asked for nothing', (await pressed({ name: 'enter', ctrl: true })) === '\x1b[27;5;13~', await pressed({ name: 'enter', ctrl: true }))
  check('ctrl+left', (await pressed({ name: 'left', ctrl: true })) === '\x1b[1;5D')
  check('ctrl+tab', (await pressed({ name: 'tab', ctrl: true })) === '\x1b[27;5;9~')
  check('shift+tab is back-tab', (await pressed({ name: 'tab', shift: true })) === '\x1b[Z', await pressed({ name: 'tab', shift: true }))
  check('shift+up', (await pressed({ name: 'up', shift: true })) === '\x1b[1;2A')
  check('shift+enter for a program that asked for nothing', (await pressed({ name: 'enter', shift: true })) === '\x1b[27;2;13~')
  check('ctrl+shift+left', (await pressed({ name: 'left', ctrl: true, shift: true })) === '\x1b[1;6D')
  await t.send({ t: 'write', data: '\x1b[>5u' })
  await t.page.waitForTimeout(60)
  check('ctrl+enter for a program that asked for kitty keys', (await pressed({ name: 'enter', ctrl: true })) === '\x1b[13;5u')
  check('plain enter stays plain under kitty keys', (await pressed({ name: 'enter' })) === '\r')
  check('shift+enter under kitty keys', (await pressed({ name: 'enter', shift: true })) === '\x1b[13;2u')
  await t.send({ t: 'write', data: '\x1b[<u' })
  await t.page.waitForTimeout(60)
  check('and back after it pops them', (await pressed({ name: 'enter', ctrl: true })) === '\x1b[27;5;13~')
  await t.send({ t: 'write', data: '\x1b[=5;1u' })
  await t.page.waitForTimeout(60)
  check('set directly as well', (await pressed({ name: 'enter', ctrl: true })) === '\x1b[13;5u')
  await t.send({ t: 'reset' })
  await t.page.waitForTimeout(60)
  check('a reset forgets them', (await pressed({ name: 'enter', ctrl: true })) === '\x1b[27;5;13~')
  await t.page.evaluate(() => { window.posts = [] })
  await t.tap(195, 300)
  check('a touch on the page says so', await t.page.evaluate(() => window.posts.some((m) => m.t === 'touched')))
  await t.page.context().close()
}
{
  // Wide characters take the widths Claude Code and herdr-core count, or everything after them on the line lands in
  // the wrong column. The page itself says where the cursor is (DSR).
  const t = await open({ phone: true })
  const column = async (text) => {
    await t.send({ t: 'reset' })
    await t.page.evaluate(() => { window.posts = [] })
    await t.send({ t: 'write', data: text + '\x1b[6n' })
    await t.page.waitForTimeout(80)
    const r = await t.page.evaluate(() => window.posts.filter((p) => p.t === 'input').map((p) => p.data).join(''))
    return Number(/;(\d+)R/.exec(r)?.[1])
  }
  for (const [name, text, col] of [['ascii', 'ab', 3], ['Chinese', '中', 3], ['check mark', '✅', 3], ['thumbs up', '👍', 3],
    ['warning with its selector', '⚠️', 3], ['flag', '🇯🇵', 3], ['joined emoji', '👨‍💻', 3], ['rocket', '🚀', 3], ['plain warning', '⚠', 2], ['Claude Code record dot', '⏺', 2]]) {
    const got = await column(text)
    check(`${name} ends at column ${col}`, got === col, got)
  }
  await t.page.context().close()
}
{
  // Two fingers change nothing: the text size is in Settings.
  const t = await open({ phone: true })
  const cdp = await t.page.context().newCDPSession(t.page)
  const fingers = (type, gap) => cdp.send('Input.dispatchTouchEvent', { type, touchPoints: type === 'touchEnd' ? [] : [{ x: 195 - gap, y: 300, id: 0 }, { x: 195 + gap, y: 300, id: 1 }] })
  const before = (await t.size()).cols
  await fingers('touchStart', 40)
  for (const g of [60, 90, 120, 150]) await fingers('touchMove', g)
  await fingers('touchEnd')
  await t.page.waitForTimeout(300)
  check('two fingers do not change the text size', (await t.size()).cols === before, (await t.size()).cols)
  check('and tell the app nothing', !(await t.page.evaluate(() => window.posts.some((m) => m.t === 'zoom'))))
  await t.page.context().close()
}
{
  // Links, as agents print them: on one line, wrapped by the terminal at a phone's width, wrapped by the TUI itself with
  // its indent, and hidden behind text (OSC 8).
  const t = await open({ phone: true })
  const cell = await t.page.evaluate(() => { const r = document.querySelector('.xterm-screen').getBoundingClientRect(); return { w: r.width / window.posts.find((m) => m.t === 'ready').cols, h: r.height / window.posts.find((m) => m.t === 'ready').rows, x: r.left, y: r.top } })
  const cols = (await t.size()).cols
  const tapRow = async (text, row, col) => {
    await t.send({ t: 'reset' })
    await t.send({ t: 'write', data: text })
    await t.page.waitForTimeout(100)
    await t.page.evaluate(() => { window.posts = [] })
    await t.tap(cell.x + (col + 0.5) * cell.w, cell.y + (row + 0.5) * cell.h)
    return t.page.evaluate(() => window.posts.filter((m) => m.t === 'link').map((m) => m.url).join(' '))
  }
  const long = 'https://example.com/a/very/long/path/that/does/not/fit/on/one/row/of/a/phone/screen?x=1'
  check('a link on one line opens', (await tapRow('see https://example.com/a now', 0, 8)) === 'https://example.com/a')
  check('a tap beside a link opens nothing', (await tapRow('see https://example.com/a now', 0, 1)) === '')
  check('a link the terminal wrapped opens whole, from its first row', (await tapRow(long, 0, 5)) === long, await tapRow(long, 0, 5))
  check('and from its last row', (await tapRow(long, 1, 3)) === long, await tapRow(long, 1, 3))
  const a = long.slice(0, 45), b = long.slice(45)
  check('a link the TUI wrapped with its indent opens whole', (await tapRow(`  ${a}\r\n  ${b}\r\n`, 1, 5)) === long, await tapRow(`  ${a}\r\n  ${b}\r\n`, 1, 5))
  check('a link behind text opens its target', (await tapRow('\x1b]8;;https://example.com/hidden\x07click here\x1b]8;;\x07', 0, 3)) === 'https://example.com/hidden', await tapRow('\x1b]8;;https://example.com/hidden\x07click here\x1b]8;;\x07', 0, 3))
  check('a link after Chinese opens where it is shown', (await tapRow('看这里 https://example.com/zh。然后', 0, 12)) === 'https://example.com/zh', await tapRow('看这里 https://example.com/zh。然后', 0, 12))
  check('text after a wrapped link on the next line stays out of it', (await tapRow(`${'x'.repeat(10)} https://example.com/a\r\nnext line\r\n`, 0, 15)) === 'https://example.com/a')
  // Chinese takes two cells a character: the row is full although its text is shorter than the row.
  const post = 'https://x.com/acme/status/1234567890123456789', zh = '  （看这条帖子 '
  const head = post.slice(0, cols - 2 - 2 * 7 - 1), tail = post.slice(head.length)
  const zhRows = `${zh}${head}\r\n  ${tail} ）\r\n`
  check('a link after Chinese that the TUI wrapped opens whole', (await tapRow(zhRows, 0, cols - 3)) === post, await tapRow(zhRows, 0, cols - 3))
  check('and from its second row', (await tapRow(zhRows, 1, 4)) === post, await tapRow(zhRows, 1, 4))
  // Selecting: hold, drag; what is selected goes to the app when the finger lifts.
  await t.send({ t: 'reset' })
  await t.send({ t: 'write', data: 'hello world again\r\nsecond line here\r\n' })
  await t.page.waitForTimeout(100)
  await t.page.evaluate(() => { window.posts = [] })
  const cdp2 = await t.page.context().newCDPSession(t.page)
  const at = (r, c) => [{ x: cell.x + (c + 0.5) * cell.w, y: cell.y + (r + 0.5) * cell.h }]
  await cdp2.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: at(0, 7) })
  await t.page.waitForTimeout(600)
  const hold = await t.page.evaluate(() => document.querySelector('.xterm-selection')?.children.length ?? 0)
  await cdp2.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: at(1, 5) })
  await cdp2.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  await t.page.waitForTimeout(200)
  const sel = await t.page.evaluate(() => window.posts.filter((m) => m.t === 'selected').map((m) => m.text).join('|'))
  check('holding and dragging across two rows gives their text', sel === 'world again\nsecond', sel)
  // An agent's TUI wraps a paragraph itself, at the phone's width: its rows end in a newline although the text is one
  // line. Copied, the paragraph is one line again; lines the agent ended stay lines.
  const selectRows = async (text, from, to) => {
    await t.send({ t: 'reset' })
    await t.send({ t: 'write', data: text })
    await t.page.waitForTimeout(100)
    await t.page.evaluate(() => { window.posts = [] })
    await cdp2.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: at(...from) })
    await t.page.waitForTimeout(600)
    await cdp2.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: at(...to) })
    await cdp2.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
    await t.page.waitForTimeout(200)
    return t.page.evaluate(() => window.posts.filter((m) => m.t === 'selected').map((m) => m.text).join('|'))
  }
  const words = (n) => Array.from({ length: n }, (_, i) => `word${i % 10}`)
  const fill = (w, width) => { const out = []; let line = ''; for (const x of w) { if (line && line.length + 1 + x.length > width) { out.push(line); line = x } else line = line ? `${line} ${x}` : x } out.push(line); return out }
  const para = fill(words(20), cols - 2)
  const paraRows = `⏺ ${para.join('\r\n  ')}\r\n\r\n  next paragraph\r\n`
  const one = await selectRows(paraRows, [0, 2], [para.length - 1, para[para.length - 1].length + 1])
  check('copying a paragraph the TUI wrapped gives one line', one === words(20).join(' '), one)
  const zhPara = '这是一段很长的中文回答，手机上会被终端里的程序自己折成好几行，复制出来应该还是一整段话才对，不要每行都多一个回车。'
  const zhWidth = Math.floor((cols - 2) / 2), zhLines = []
  for (let i = 0; i < zhPara.length; i += zhWidth) zhLines.push(zhPara.slice(i, i + zhWidth))
  const zhCopy = await selectRows(`⏺ ${zhLines.join('\r\n  ')}\r\n`, [0, 2], [zhLines.length - 1, 2 + 2 * zhLines[zhLines.length - 1].length - 1])
  check('and a Chinese one, with no spaces added', zhCopy === zhPara, zhCopy)
  const list = `  - ${'item '.repeat(Math.floor((cols - 4) / 5)).trim()}\r\n  - second item\r\n`
  const listCopy = await selectRows(list, [0, 4], [1, 14])
  check('a new list item stays on its own line', listCopy.split('\n').length === 2 && listCopy.endsWith('- second item'), listCopy)
  await t.page.context().close()
}
{
  const t = await open({})
  const s = await t.size()
  check('a wide pane keeps the desktop padding', s.left === 14, s)
  await t.page.context().close()
}
{
  // xterm's overview ruler outlines itself in white by default, and draws that once the terminal is resized.
  const t = await open({ phone: true })
  await t.page.setViewportSize({ width: 184, height: 560 })
  await t.page.waitForTimeout(300)
  const ruler = await t.page.evaluate(() => {
    const c = document.querySelector('.xterm-decoration-overview-ruler')
    if (!c || !c.width) return { none: true }
    const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data
    let brightest = 0
    for (let i = 0; i < d.length; i += 4) if (d[i + 3] > 0) brightest = Math.max(brightest, d[i], d[i + 1], d[i + 2])
    return { brightest }
  })
  check('a resized dark terminal has no bright line at its edge', !ruler.none && ruler.brightest < 64, ruler)
  await t.page.context().close()
}

await browser.close()
if (failures.length) { console.error(`${failures.length} failed`); process.exit(1) }
