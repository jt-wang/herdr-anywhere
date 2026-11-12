// Kitty graphics (https://sw.kovidgoyal.net/kitty/graphics-protocol/) in a terminal's output, the part Oh My Pi uses
// when it takes the terminal for one that shows pictures (herdr-core sets TERM_PROGRAM=ghostty in every terminal):
// a PNG sent in pieces (a=t, f=100, m=1 until m=0), placed with Unicode placeholders (a=p, U=1, c columns by r rows),
// and deleted (a=d). The page's xterm.js drops APC strings without a look, so the app takes the commands out of the
// stream here and keeps each terminal's pictures: herdr-core sends a pane opened again in the same run as one screen
// of placeholders, without the pictures (the `back` recording in fixtures/kitty). Other commands are dropped quietly.
// Recordings of what the page was given: fixtures/kitty.

export type KittyImage = { id: number; png: string; cols: number; rows: number }
export type Taken = { text: string; put: KittyImage[]; dropped?: number[] | 'all' }
/** How much a terminal keeps: base64 characters of all its pictures together, and how many. */
export type KittyLimits = { chars: number; count: number }

/** A PNG's size, from its header (base64). */
export function pngSize(b64: string): { width: number; height: number } {
  let bin = ''
  try { bin = atob(b64.slice(0, 32)) } catch { return { width: 0, height: 0 } }
  if (bin.slice(1, 4) !== 'PNG' || bin.length < 24) return { width: 0, height: 0 }
  const u32 = (i: number) => ((bin.charCodeAt(i) << 24) >>> 0) + (bin.charCodeAt(i + 1) << 16) + (bin.charCodeAt(i + 2) << 8) + bin.charCodeAt(i + 3)
  return { width: u32(16), height: u32(20) }
}

const START = '\x1b_G'
const END = '\x1b\\'
// Placeholder cells with their marks. xterm draws a cell's combining marks past its right edge, over the next cell,
// where the page's picture does not reach, so the app sends them hidden (SGR 8, ended by SGR 28): xterm draws only
// their background, and the page still reads their colour and marks from the buffer.
const PLACEHOLDERS = /(?:\u{10EEEE}\p{M}*)+/gu
const hide = (text: string) => (text.includes('\u{10EEEE}') ? text.replace(PLACEHOLDERS, '\x1b[8m$&\x1b[28m') : text)

type Stored = KittyImage & { used: number; placed: boolean }
type Incoming = { id: number; png: boolean; parts: string[]; place?: { cols: number; rows: number } }

export class KittyImages {
  private images = new Map<number, Stored>()
  private sizes = new Map<number, { cols: number; rows: number }>()
  /** Text held back: a command not ended yet, or an ESC that may start one. */
  private held = ''
  private incoming?: Incoming
  private clock = 0
  // Two of the largest pictures omp sends (1568 by 1045 px, 4.37M base64 characters each). The page holds the open
  // terminal's pictures: in headless Chromium the first of these added 57 MB, each one after it 4 to 12 MB.
  constructor(private limits: KittyLimits = { chars: 9_000_000, count: 32 }) {}

  /** The terminal's text with the graphics commands taken out, the pictures that can be drawn now, and those gone. */
  take(chunk: string): Taken {
    let s = this.held + chunk
    this.held = ''
    const out: Taken = { text: '', put: [] }
    const changed = new Map<number, KittyImage>()
    let from = 0
    for (;;) {
      const at = s.indexOf(START, from)
      if (at < 0) {
        // A last ESC, or ESC _, may begin a command the next write ends.
        const tail = s.endsWith('\x1b') ? 1 : s.endsWith('\x1b_') ? 2 : 0
        out.text += s.slice(from, s.length - tail)
        this.held = s.slice(s.length - tail)
        break
      }
      const end = s.indexOf(END, at + START.length)
      out.text += s.slice(from, at)
      if (end < 0) { this.held = s.slice(at); break }
      this.command(s.slice(at + START.length, end), changed, out)
      from = end + END.length
    }
    for (const img of changed.values()) if (this.images.get(img.id) === img) out.put.push(img)
    out.text = hide(out.text)
    return out
  }

  /** The pictures kept, oldest used first. */
  all(): KittyImage[] {
    return [...this.images.values()].filter((i) => i.placed).sort((a, b) => a.used - b.used).map(({ id, png, cols, rows }) => ({ id, png, cols, rows }))
  }

  private command(body: string, changed: Map<number, KittyImage>, out: Taken) {
    const semi = body.indexOf(';')
    const keys: Record<string, string> = {}
    for (const kv of (semi < 0 ? body : body.slice(0, semi)).split(',')) {
      const eq = kv.indexOf('=')
      if (eq > 0) keys[kv.slice(0, eq)] = kv.slice(eq + 1)
    }
    const data = semi < 0 ? '' : body.slice(semi + 1)
    const num = (k: string) => { const n = Number(keys[k]); return Number.isInteger(n) && n >= 0 ? n : 0 }
    // A piece after the first carries only m= (and q=): it goes on with the picture being sent.
    if (this.incoming && !keys.a && !keys.i) {
      this.incoming.parts.push(data)
      if (keys.m !== '1') this.received(this.incoming, changed, out)
      return
    }
    this.incoming = undefined
    const a = keys.a || 't'
    const id = num('i')
    if (a === 't' || a === 'T') {
      if (!id) return
      const inc: Incoming = { id, parts: [data], png: (keys.f ?? '32') === '100' && (keys.t ?? 'd') === 'd' && !keys.o }
      if (a === 'T' && keys.U === '1' && num('c') && num('r')) inc.place = { cols: num('c'), rows: num('r') }
      if (keys.m === '1') this.incoming = inc
      else this.received(inc, changed, out)
    } else if (a === 'p') {
      if (!id || keys.U !== '1' || !num('c') || !num('r')) return
      this.sizes.set(id, { cols: num('c'), rows: num('r') })
      const img = this.images.get(id)
      if (img) {
        img.cols = num('c'); img.rows = num('r'); img.placed = true; img.used = ++this.clock
        changed.set(id, img)
      }
    } else if (a === 'd') {
      const d = keys.d || 'a'
      if (d === 'a' || d === 'A') {
        this.images.clear(); this.sizes.clear(); changed.clear()
        out.dropped = 'all'
      } else if ((d === 'i' || d === 'I') && id) {
        this.forget(id, out)
        changed.delete(id)
      }
    }
  }

  private received(inc: Incoming, changed: Map<number, KittyImage>, out: Taken) {
    this.incoming = undefined
    if (!inc.png) return
    const png = inc.parts.join('')
    if (!pngSize(png).width) return
    if (inc.place) this.sizes.set(inc.id, inc.place)
    const size = this.sizes.get(inc.id)
    const img: Stored = { id: inc.id, png, cols: size?.cols ?? 0, rows: size?.rows ?? 0, placed: !!size, used: ++this.clock }
    this.images.set(inc.id, img)
    if (img.placed) changed.set(inc.id, img)
    this.trim(inc.id, out, changed)
  }

  private forget(id: number, out: Taken) {
    this.sizes.delete(id)
    if (!this.images.delete(id)) return
    if (out.dropped !== 'all') { const ids = out.dropped ?? (out.dropped = []); ids.push(id) }
  }

  // Over the limits, the pictures used longest ago go, never the one just received.
  private trim(keep: number, out: Taken, changed: Map<number, KittyImage>) {
    let chars = 0
    for (const i of this.images.values()) chars += i.png.length
    const old = [...this.images.values()].filter((i) => i.id !== keep).sort((a, b) => a.used - b.used)
    while ((chars > this.limits.chars || this.images.size > this.limits.count) && old.length) {
      const o = old.shift()!
      chars -= o.png.length
      this.forget(o.id, out)
      changed.delete(o.id)
    }
  }
}

/** The combining marks that number a placeholder's row and column (and the high byte of its picture's id), in
 *  order: kitty's rowcolumn-diacritics.txt, as Ghostty cf5519b has it (src/terminal/kitty/graphics_unicode.zig, MIT). */
export const KITTY_MARKS = [
  0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351,
  0x0352, 0x0357, 0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369, 0x036A, 0x036B, 0x036C, 0x036D,
  0x036E, 0x036F, 0x0483, 0x0484, 0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597, 0x0598, 0x0599,
  0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1, 0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611,
  0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658, 0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6,
  0x06D7, 0x06D8, 0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2, 0x06E4, 0x06E7, 0x06E8, 0x06EB,
  0x06EC, 0x0730, 0x0732, 0x0733, 0x0735, 0x0736, 0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743, 0x0745, 0x0747,
  0x0749, 0x074A, 0x07EB, 0x07EC, 0x07ED, 0x07EE, 0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817, 0x0818, 0x0819,
  0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822, 0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A,
  0x082B, 0x082C, 0x082D, 0x0951, 0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87, 0x135D, 0x135E, 0x135F, 0x17DD,
  0x193A, 0x1A17, 0x1A75, 0x1A76, 0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D, 0x1B6E, 0x1B6F,
  0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1, 0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4,
  0x1DC5, 0x1DC6, 0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1, 0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5, 0x1DD6, 0x1DD7,
  0x1DD8, 0x1DD9, 0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1, 0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5,
  0x1DE6, 0x1DFE, 0x20D0, 0x20D1, 0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7, 0x20E9, 0x20F0,
  0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2, 0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA,
  0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2, 0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8,
  0x2DF9, 0x2DFA, 0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D, 0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1,
  0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5, 0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED, 0xA8EE, 0xA8EF,
  0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2, 0xAAB3, 0xAAB7, 0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23,
  0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187, 0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC,
  0x1D1AD, 0x1D242, 0x1D243, 0x1D244
]
