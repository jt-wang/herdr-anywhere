// Oh My Pi's pictures on the terminal page, in Chromium, from what the phone page was given while omp 18.18.3 drew
// (fixtures/kitty): the app's side (PageChannel with the terminal's pictures) feeds the page as the app does. The
// picture must be drawn over its placeholder cells, where and as large as they say; after the pane is left and opened
// again, from the one screen herdr-core sends; and output without pictures must draw nothing more.
// Screenshots go to e2e/out/kitty/. BEFORE=<file>: a terminal-page.ts from before (CI: main's) for the same shots.
// Run with `e2e/page-kitty.sh`.
import { createRequire } from 'node:module'
import { mkdirSync, readFileSync, readdirSync } from 'node:fs'
import { inflateSync } from 'node:zlib'
import { PageChannel, terminalPage } from '../src/ui/terminal-page.ts'
import { KittyImages } from '../src/model/kitty.ts'

const require = createRequire(process.env.PLAYWRIGHT_DIR + '/')
const { chromium } = require('./index.js')
const fixture = (name) => JSON.parse(readFileSync(new URL(`../fixtures/kitty/omp-18.18.3-${name}.json`, import.meta.url), 'utf8'))
const debug = fixture('debug'), read = fixture('read')
const shots = new URL('out/kitty/', import.meta.url).pathname
mkdirSync(shots, { recursive: true })

const browser = await chromium.launch()
const failures = []
const check = (name, ok, seen) => { console.log(`${ok ? 'ok' : 'FAIL'}: ${name}${seen === undefined ? '' : ` (${JSON.stringify(seen)})`}`); if (!ok) failures.push(name) }

// A phone page as the recordings had it (390 CSS px wide, 13 px text: 50 columns), fed by a PageChannel.
async function open(page_ = terminalPage, { dark = true, images } = {}) {
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true, deviceScaleFactor: 3 })
  const page = await context.newPage()
  await page.addInitScript(() => { window.posts = []; window.nsh = { postMessage: (m) => window.posts.push(JSON.parse(m)) }
    let T; Object.defineProperty(window, 'Terminal', { configurable: true, get: () => T, set: (v) => { T = class extends v { constructor(...a) { super(...a); window.term = this } } } }) })
  await page.route('http://terminal.test/', (r) => r.fulfill({ contentType: 'text/html', body: page_({ dark, fontSize: 13, scrollback: 2000, phone: true }) }))
  await page.goto('http://terminal.test/')
  await page.waitForFunction(() => window.posts.some((m) => m.t === 'ready'))
  const sent = []
  const ch = new PageChannel((m) => sent.push(m), (f) => setTimeout(f, 0), images)
  const flush = async () => {
    await new Promise((r) => setTimeout(r, 5))
    for (const m of sent.splice(0)) await page.evaluate((m) => window.postMessage(m, '*'), m)
    await page.waitForTimeout(400)
  }
  ch.ready()
  const feed = async (writes) => { for (const w of writes) ch.write(new TextEncoder().encode(w)); await flush() }
  return { page, ch, feed, flush, cols: () => page.evaluate(() => window.term.cols), close: () => context.close() }
}

// Where the placeholder cells are on screen (CSS px), from xterm's buffer: the block of rows that hold U+10EEEE.
const block = (page) => page.evaluate(() => {
  const t = window.term, b = t.buffer.active, s = document.querySelector('.xterm-screen').getBoundingClientRect()
  const cw = s.width / t.cols, ch = s.height / t.rows
  let top = -1, bottom = -1, left = 1e9, right = -1
  for (let y = 0; y < t.rows; y++) {
    const l = b.getLine(b.viewportY + y)
    for (let x = 0; l && x < t.cols; x++) { const c = l.getCell(x); if (c && c.getChars().codePointAt(0) === 0x10eeee) { if (top < 0) top = y; bottom = y; left = Math.min(left, x); right = Math.max(right, x) } }
  }
  return top < 0 ? null : { x: s.left + left * cw, y: s.top + top * ch, width: (right - left + 1) * cw, height: (bottom - top + 1) * ch, cols: right - left + 1, rows: bottom - top + 1 }
})

// The screenshot's pixels in a rectangle, as RGB (an 8-bit RGB or RGBA PNG, as Chromium writes them, at scale 'css').
async function pixels(page, r) {
  const png = await page.screenshot({ clip: r, scale: 'css' })
  let at = 8, w = 0, h = 0, n = 4, idat = []
  while (at < png.length) {
    const len = png.readUInt32BE(at), type = png.toString('ascii', at + 4, at + 8)
    if (type === 'IHDR') { w = png.readUInt32BE(at + 8); h = png.readUInt32BE(at + 12); n = png[at + 17] === 6 ? 4 : 3 }
    if (type === 'IDAT') idat.push(png.subarray(at + 8, at + 8 + len))
    at += 12 + len
  }
  const row = w * n, raw = inflateSync(Buffer.concat(idat)), out = new Uint8Array(row * h)
  for (let y = 0; y < h; y++) {
    const f = raw[y * (row + 1)]
    for (let i = 0; i < row; i++) {
      const a = i >= n ? out[y * row + i - n] : 0, b = y ? out[(y - 1) * row + i] : 0, c = y && i >= n ? out[(y - 1) * row + i - n] : 0
      const p = a + b - c, pa = Math.abs(p - a), pb = Math.abs(p - b), pc = Math.abs(p - c)
      const pred = f === 1 ? a : f === 2 ? b : f === 3 ? (a + b) >> 1 : f === 4 ? (pa <= pb && pa <= pc ? a : pb <= pc ? b : c) : 0
      out[y * row + i] = (raw[y * (row + 1) + 1 + i] + pred) & 255
    }
  }
  return { w, h, at: (x, y) => [out[y * row + x * n], out[y * row + x * n + 1], out[y * row + x * n + 2]] }
}

// The picture as the page should show it: fitted into the block keeping its shape, centred. Points on a 7 by 7 grid
// inside it, each compared with the PNG's own pixel there (the browser draws the PNG in the page to read it).
async function looksLike(page, r, png) {
  const shot = await pixels(page, r)
  const want = await page.evaluate(async ({ png, pts }) => {
    const img = new Image(); img.src = 'data:image/png;base64,' + png; await img.decode()
    const c = document.createElement('canvas'); c.width = img.naturalWidth; c.height = img.naturalHeight
    const g = c.getContext('2d'); g.drawImage(img, 0, 0)
    return { w: img.naturalWidth, h: img.naturalHeight, px: pts.map(([u, v]) => [...g.getImageData(Math.floor(u * img.naturalWidth), Math.floor(v * img.naturalHeight), 1, 1).data].slice(0, 3)) }
  }, { png, pts: Array.from({ length: 49 }, (_, i) => [((i % 7) + 0.5) / 7, (Math.floor(i / 7) + 0.5) / 7]) })
  const s = Math.min(shot.w / want.w, shot.h / want.h), dw = want.w * s, dh = want.h * s, ox = (shot.w - dw) / 2, oy = (shot.h - dh) / 2
  let same = 0
  want.px.forEach((p, i) => {
    const q = shot.at(Math.floor(ox + (((i % 7) + 0.5) / 7) * dw), Math.floor(oy + ((Math.floor(i / 7) + 0.5) / 7) * dh))
    if (p.every((v, k) => Math.abs(v - q[k]) <= 40)) same++
  })
  return same
}
const pngOf = (s) => s.live.join('').match(/\x1b_Ga=t,f=100,q=2,i=\d+,m=1;([^\x1b]*)/)[1] + s.live.join('').match(/\x1b_Gq=2,m=1;([^\x1b]*)/g).map((m) => m.slice(m.indexOf(';') + 1)).join('') + (s.live.join('').match(/\x1b_Gq=2,m=0;([^\x1b]*)/) || ['', ''])[1]

// Before: the page as it was, given the same output.
if (process.env.BEFORE) {
  const { terminalPage: before } = await import(process.env.BEFORE)
  for (const [name, s, dark] of [['debug', debug, true], ['debug', debug, false], ['read', read, true]]) {
    const t = await open(before, { dark })
    await t.feed([...s.open, ...s.live])
    await t.page.screenshot({ path: `${shots}before-${name}-${dark ? 'dark' : 'light'}.png` })
    await t.close()
  }
}

// omp's self-test picture, while omp draws it, dark and light.
for (const dark of [true, false]) {
  const t = await open(terminalPage, { dark, images: new KittyImages() })
  check('the recording\'s 50 columns', (await t.cols()) === 50, await t.cols())
  await t.feed([...debug.open, ...debug.live])
  const r = await block(t.page)
  check('omp drew a block of 20 by 7 placeholder cells', r && r.cols === 20 && r.rows === 7, r)
  const same = r ? await looksLike(t.page, r, pngOf(debug)) : 0
  check(`${dark ? 'dark' : 'light'}: the self-test picture is drawn over its cells (49 points compared)`, same >= 44, same)
  await t.page.screenshot({ path: `${shots}after-debug-${dark ? 'dark' : 'light'}.png` })
  await t.close()
}

// A picture omp read for the model, 48 by 16 cells.
{
  const t = await open(terminalPage, { images: new KittyImages() })
  await t.feed([...read.open, ...read.live])
  const r = await block(t.page)
  check('the read picture takes 48 by 16 cells', r && r.cols === 48 && r.rows === 16, r)
  const same = r ? await looksLike(t.page, r, pngOf(read)) : 0
  check('the read picture is drawn over its cells', same >= 44, same)
  await t.page.screenshot({ path: `${shots}after-read-dark.png` })
  await t.close()
}

// Left and opened again: a new page, the terminal's pictures kept by the app, and the one screen herdr-core sends.
{
  const images = new KittyImages()
  const first = await open(terminalPage, { images })
  await first.feed([...debug.open, ...debug.live])
  await first.close()
  const t = await open(terminalPage, { images })
  t.ch.reset()
  await t.feed(debug.back)
  const r = await block(t.page)
  const same = r ? await looksLike(t.page, r, pngOf(debug)) : 0
  check('opened again, the picture is still drawn from the screen herdr-core sends', same >= 44, same)
  await t.page.screenshot({ path: `${shots}after-back-dark.png` })
  await t.close()
}

// Placeholders whose picture the app no longer has: the cells show the terminal's background, not the marks on them.
{
  const t = await open(terminalPage, { images: new KittyImages() })
  await t.feed(debug.back)
  const r = await block(t.page)
  const shot = r ? await pixels(t.page, r) : null
  let bg = 0, n = 0
  for (let y = 2; shot && y < shot.h; y += 3) for (let x = 2; x < shot.w; x += 3) { n++; const p = shot.at(x, y); if (Math.abs(p[0] - 0x17) + Math.abs(p[1] - 0x19) + Math.abs(p[2] - 0x1d) <= 6) bg++ }
  check('placeholders without their picture show no marks', n > 0 && bg === n, { bg, n })
  await t.page.screenshot({ path: `${shots}after-missing-dark.png` })
  await t.close()
}

// Output without pictures: the page adds nothing to the screen (real agents' recordings, e2e/tui-paths).
{
  const t = await open(terminalPage, { images: new KittyImages() })
  for (const f of readdirSync(new URL('tui-paths/', import.meta.url)).filter((f) => f.endsWith('.ansi')).slice(0, 6)) {
    await t.feed([readFileSync(new URL(`tui-paths/${f}`, import.meta.url), 'utf8')])
  }
  const extra = await t.page.evaluate(() => document.querySelectorAll('.kitty-pictures *').length)
  check('agents that draw no pictures get no picture layer', extra === 0, extra)
  await t.close()
}

await browser.close()
if (failures.length) { console.error(`${failures.length} failed`); process.exit(1) }
