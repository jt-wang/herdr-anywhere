// Kitty graphics in a terminal's output: the app takes the ESC _ G commands out of the stream (xterm.js 6 drops APC
// strings unseen) and keeps each terminal's images, so the page can draw them over their placeholder cells, also
// after the pane is left and opened again, when herdr-core sends one screen and no image data.
// The samples are what the phone page was given while Oh My Pi 18.18.3 drew (fixtures/kitty/README.md).
import { expect, test } from 'nsh/test'
import { KittyImages, pngSize } from './kitty.ts'
import debug from '../../fixtures/kitty/omp-18.18.3-debug.json'
import read from '../../fixtures/kitty/omp-18.18.3-read.json'

const APC = /\x1b_G[^\x1b]*\x1b\\/g
const feed = (k: KittyImages, writes: string[]) => {
  const out = { text: '', put: [] as number[], dropped: [] as (number[] | 'all')[] }
  for (const w of writes) {
    const r = k.take(w)
    out.text += r.text
    out.put.push(...r.put.map((i) => i.id))
    if (r.dropped) out.dropped.push(r.dropped)
  }
  return out
}

test('omp\'s self-test picture: sent in 18 pieces, placed 20 by 7 cells', () => {
  const k = new KittyImages()
  const r = feed(k, [...debug.open, ...debug.live])
  expect(r.put).toEqual([13180035])
  const img = k.all()[0]
  expect([img.id, img.cols, img.rows]).toEqual([13180035, 20, 7])
  expect(img.png.length).toBe(17 * 4096 + 28)
  expect(pngSize(img.png)).toEqual({ width: 192, height: 128 })
})

test('a picture omp read for the model (240 by 160, sent at 300 by 200): 48 by 16 cells', () => {
  const k = new KittyImages()
  feed(k, [...read.open, ...read.live])
  const img = k.all()[0]
  expect([img.id, img.cols, img.rows]).toEqual([12372580, 48, 16])
  expect(pngSize(img.png)).toEqual({ width: 300, height: 200 })
})

test('the terminal gets everything but the graphics commands, placeholders included', () => {
  for (const s of [debug, read]) for (const step of ['open', 'live', 'reload', 'back'] as const) {
    const all = s[step].join('')
    expect(feed(new KittyImages(), s[step]).text).toBe(all.replace(APC, ''))
  }
})

test('a command cut anywhere between two writes is read the same', () => {
  const all = [...debug.open, ...debug.live].join('')
  for (const size of [1, 2, 3, 7, 4099]) {
    const pieces: string[] = []
    for (let i = 0; i < all.length; i += size) pieces.push(all.slice(i, i + size))
    const k = new KittyImages()
    const r = feed(k, pieces)
    expect(r.text).toBe(all.replace(APC, ''))
    expect(k.all().map((i) => [i.id, i.png.length])).toEqual([[13180035, 17 * 4096 + 28]])
  }
})

test('output with no graphics passes unchanged, an escape at a write\'s end included', () => {
  const k = new KittyImages()
  const a = k.take('\x1b[31mred\x1b')
  const b = k.take('[0m plain \x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\')
  expect(a.text + b.text).toBe('\x1b[31mred\x1b[0m plain \x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\')
  expect(k.all()).toEqual([])
})

test('a delete of all images forgets them; a delete by id forgets that one', () => {
  const k = new KittyImages()
  feed(k, [...debug.open, ...debug.live])
  feed(k, [...read.live])
  expect(k.all().map((i) => i.id).sort((a, b) => a - b)).toEqual([12372580, 13180035])
  expect(k.take('\x1b_Ga=d,d=I,i=13180035,q=2\x1b\\').dropped).toEqual([13180035])
  expect(k.all().map((i) => i.id)).toEqual([12372580])
  expect(k.take('\x1b_Ga=d,d=A,q=2\x1b\\').dropped).toBe('all')
  expect(k.all()).toEqual([])
})

test('commands omp does not use are dropped quietly: queries, raw pixels, files, compressed data', () => {
  const k = new KittyImages()
  const r = feed(k, ['a\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\b', '\x1b_Ga=T,f=32,i=5,s=1,v=1;AAAAAA==\x1b\\',
    '\x1b_Ga=T,f=100,t=f,i=6;L3RtcC94LnBuZw==\x1b\\', '\x1b_Ga=T,f=100,o=z,i=7,U=1,c=1,r=1;eJw=\x1b\\', 'c'])
  expect(r.text).toBe('abc')
  expect(k.all()).toEqual([])
})

test('a picture placed again with the same id replaces the one kept, and is kept once', () => {
  const k = new KittyImages()
  feed(k, [...debug.open, ...debug.live])
  feed(k, debug.reload)
  expect(k.all().map((i) => i.id)).toEqual([13180035])
})

test('the images kept stay under the limit: the one used longest ago goes first', () => {
  const png = (debug.live.join('').match(/i=13180035,m=1;([^\x1b]*)/) as RegExpMatchArray)[1]
  const k = new KittyImages({ chars: 3 * png.length + 10, count: 10 })
  const put = (id: number) => k.take(`\x1b_Ga=T,f=100,i=${id},U=1,c=2,r=1,q=2;${png}\x1b\\`)
  put(1); put(2); put(3)
  k.take('\x1b_Ga=p,U=1,i=1,c=2,r=1,q=2\x1b\\') // 1 used again
  const r = put(4)
  expect(r.dropped).toEqual([2])
  expect(k.all().map((i) => i.id).sort((a, b) => a - b)).toEqual([1, 3, 4])
  const few = new KittyImages({ chars: 1e9, count: 2 })
  for (const id of [1, 2, 3]) few.take(`\x1b_Ga=T,f=100,i=${id},U=1,c=2,r=1;${png}\x1b\\`)
  expect(few.all().map((i) => i.id).sort((a, b) => a - b)).toEqual([2, 3])
})
