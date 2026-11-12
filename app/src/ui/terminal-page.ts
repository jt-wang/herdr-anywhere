// The terminal's web page: xterm.js draws the screen and reads the keyboard; the stream stays in the app.
// It draws as Herdr's terminal does (herdr-app src/settings.rs, src/terminal.rs): JetBrains Mono at medium weight with
// Symbols Nerd Font for prompt icons, cells 1.15 times the font's height, 14 by 12 px of padding, the theme's palette,
// and box drawing joined across rows (the WebGL renderer draws those characters itself, as the desktop's sprites do).
// A phone's terminal has no padding at the sides and no room kept for a scrollbar: it has no width to spare.
// ⏺ ⏸ ✔ ⚠ and other symbols that are text by default come from Noto Sans Symbols 2, in the terminal's colours;
// Android would otherwise draw them from its colour emoji font.
// A phone has no mouse, so the page reads fingers: the keyboard comes up only when the app asks for it, a drag
// scrolls back and a flick keeps going and slows down, a flick sideways goes to the next or previous tab, a long press
// selects a word (dragging extends it), and a tap on a link opens it. A full-screen program keeps its own scrollback:
// its drag reaches it as a mouse wheel, or as arrow keys if it doesn't follow the mouse, as Termux does. A program that
// follows the mouse gets a tap as a click on the cell under the finger: OpenCode's dialogs have buttons only a click reaches.
// Page → app (window.nsh.postMessage, JSON): ready / size {cols, rows} once laid out and on each change,
// input {data}, copied {text}, selected {text}, swipe {dir}, link {url}, tap, away {away} (scrolled back
// or at the bottom again), tail {lines} (the last lines up to the cursor, once output settles), text {base, lines} (the
// whole buffer, asked for).
// App → page (WebViewRef.postMessage, JSON): reset, write {data}, key {name} (the key bar, encoded by xterm's own
// cursor-key mode), paste {data} (bracketed when the program asked), theme {dark}, fontSize {size}, copy, focus,
// bottom, refit (a split changed this pane's height: its rows follow, which the keyboard's height never does), text, mark {line, label} (a rule above the buffer's line `line`: "New since you looked").
import { FIT_JS, GRAPHEMES_JS, WEBGL_JS, XTERM_CSS, XTERM_JS } from '../vendor/xterm.ts'
import { MONO_WOFF, NOTO_MONO_WOFF, SYMBOLS1_WOFF, SYMBOLS2_WOFF, SYMBOLS_WOFF } from '../vendor/fonts.ts'
import { KITTY_MARKS, type KittyImage, type KittyImages } from '../model/kitty.ts'

export type PageMessage = { t: 'ready' | 'size'; cols: number; rows: number } | { t: 'input'; data: string } | { t: 'copied'; text: string }
  | { t: 'selected'; text: string } | { t: 'swipe'; dir: 'next' | 'prev' } | { t: 'link'; url: string } | { t: 'image'; path: string; id: number } | { t: 'tap' } | { t: 'touched' } | { t: 'focused' }
  | { t: 'away'; away: boolean } | { t: 'tail'; lines: string[] } | { t: 'paths'; paths: string[] } | { t: 'text'; base: number; lines: string[] }
export type BarKey = 'esc' | 'tab' | 'btab' | 'enter' | 'ctrlc' | 'up' | 'down' | 'left' | 'right' | 'home' | 'end' | 'pgup' | 'pgdn'

/** Whether the terminal's WebView is left scrollable. On Android NSH 0.3.0's WebView (WebBox) swallows every finger
 *  move while it is not, and the page saw only touchstart and touchend: no drag or selection (measured
 *  on the API 35 emulator over DevTools). The page never scrolls itself: touch-action is none. iOS keeps
 *  it off, as before; whether moves reach the page there was not checked. */
export const webViewScrollable = (os: string) => os === 'android'

/** The text sizes Settings offers. */
/** The terminal's fonts, in the order a character is looked for. */
export const TERMINAL_FONTS = "'JetBrains Mono', 'Symbols Nerd Font', 'Noto Sans Symbols 2', 'Noto Sans Symbols', 'Noto Sans Mono', ui-monospace, Menlo, monospace"
export const FONT_MIN = 9
export const FONT_MAX = 22
// herdr-app settings.rs DEFAULT_FONT (weight MEDIUM, bold BOLD), terminal.rs cell height (ascent + descent) × 1.15,
// layouts terminal_padding [14, 12].
const LOOK = { fontWeight: 500, fontWeightBold: 700, lineHeight: 1.15 }

const ANSI = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white']
const NAMES = [...ANSI, ...ANSI.map((n) => 'bright' + n[0].toUpperCase() + n.slice(1))]
const hex = (n: number) => '#' + n.toString(16).padStart(6, '0')

// herdr-app src/settings.rs: GRAPHITE (the default dark) and PAPER (the default light).
const GRAPHITE = { background: 0x17191d, foreground: 0xd2d4d9, selection: 0x394454, cursor: 0xd2d4d9,
  ansi: [0x45484f, 0xdc8c95, 0xa6bd91, 0xd8be87, 0x96b2d7, 0xb5a2ce, 0x8cbdc3, 0xd2d4d9, 0x818793, 0xf0a5ad, 0xbad5a2, 0xead2a1, 0xb0cbef, 0xcbb7e6, 0xa4d5db, 0xf0f0f2] }
const PAPER = { background: 0xffffff, foreground: 0x25272c, selection: 0xcfdcf5, cursor: 0x25272c,
  ansi: [0x25272c, 0xc62a2a, 0x1a7f37, 0x8f5d00, 0x1f62d0, 0x7a45c8, 0x0f6f74, 0x5f6068, 0x7b7e87, 0xd1242f, 0x1f883d, 0x9a6700, 0x0969da, 0x8250df, 0x1b7c83, 0x111214] }

export function xtermTheme(dark: boolean): Record<string, string> {
  const p = dark ? GRAPHITE : PAPER
  const t: Record<string, string> = { background: hex(p.background), overviewRulerBorder: hex(p.background), foreground: hex(p.foreground), cursor: hex(p.cursor), cursorAccent: hex(p.background), selectionBackground: hex(p.selection) }
  p.ansi.forEach((c, i) => { t[NAMES[i]] = hex(c) })
  return t
}

/** Ctrl held from the key bar: a letter or one of @[\]^_ (and space) becomes its control character. */
export function ctrlKey(data: string): string {
  if (data.length !== 1) return data
  if (data === ' ') return '\x00'
  const c = data.toUpperCase().charCodeAt(0)
  return c >= 0x40 && c <= 0x5f ? String.fromCharCode(c & 0x1f) : data
}

const BOOT = `(function () {
  var cfg = window.__terminal;
  var box = document.getElementById('t');
  var term, fit, last = '', away = false, wide = 0, tall = 0, kitty = [];
  var URLCHAR = /[^\\s"'<>\\u2500-\\u257f\\u3000-\\u9fff\\uff00-\\uffef]/;
  function post(m) { window.nsh.postMessage(JSON.stringify(m)); }
  // On a phone only a new width changes the terminal's size. A TUI told of a new height redraws, and what it had on
  // screen ends up in the scrollback a second time (measured with Claude Code); so the keyboard and a taller composer
  // cover the page's bottom, and lift() keeps what the screen shows above them.
  function room() { return window.innerHeight - 24; }
  function lift() {
    if (!cfg.phone) return;
    var over = tall - room(), b = term.buffer.active, y = 0;
    if (over > 0) {
      y = over;
      if (!away) {
        var h = term.element.querySelector('.xterm-screen').getBoundingClientRect().height / term.rows, end = b.cursorY;
        for (var i = term.rows - 1; i > end; i--) { var l = b.getLine(b.baseY + i); if (l && l.translateToString(true).trim()) { end = i; break; } }
        y = Math.max(0, Math.min(over, (end + 1 + b.baseY - b.viewportY) * h - room()));
      }
    }
    box.style.transform = y ? 'translateY(' + (-y) + 'px)' : '';
  }
  // On a phone the view ends at the screen's last line: empty rows under it give way to the history above. A
  // full-screen program owns every row.
  function lastRow() {
    var b = term.buffer.active, end = b.cursorY;
    for (var i = term.rows - 1; i > end; i--) { var l = b.getLine(b.baseY + i); if (l && l.translateToString(true).trim()) { end = i; break; } }
    return end;
  }
  function live() {
    var b = term.buffer.active;
    if (!cfg.phone || b.type === 'alternate') return b.baseY;
    return b.baseY - Math.min(b.baseY, term.rows - 1 - lastRow());
  }
  // Scrolled back from the live view, or at it again. On a phone only a finger or Latest moves the view; output that
  // lands while the view sits above the empty rows would otherwise read as scrolling back.
  function where() {
    var b = term.buffer.active, a = b.viewportY < live();
    if (a !== away) { away = a; post({ t: 'away', away: a }); lift(); }
  }
  // The padding and the height left over under the last row show the terminal's own background: what the program paints
  // in its first and last rows (OpenCode paints its own), else the theme's. Otherwise they show as stripes.
  var COLORS = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white'], LEVELS = [0, 95, 135, 175, 215, 255], painted = '';
  function rgb(r, g, b) { return '#' + ((1 << 24) | (r << 16) | (g << 8) | b).toString(16).slice(1); }
  function palette(i) {
    var t = term.options.theme;
    if (i < 16) return t[i < 8 ? COLORS[i] : 'bright' + COLORS[i - 8][0].toUpperCase() + COLORS[i - 8].slice(1)];
    if (i < 232) { i -= 16; return rgb(LEVELS[Math.floor(i / 36)], LEVELS[Math.floor(i / 6) % 6], LEVELS[i % 6]); }
    var g = 8 + (i - 232) * 10; return rgb(g, g, g);
  }
  function cellBg(c) {
    var t = term.options.theme;
    if (c.isInverse()) return c.isFgRGB() ? rgb(c.getFgColor() >> 16, (c.getFgColor() >> 8) & 255, c.getFgColor() & 255) : c.isFgPalette() ? palette(c.getFgColor()) : t.foreground;
    return c.isBgRGB() ? rgb(c.getBgColor() >> 16, (c.getBgColor() >> 8) & 255, c.getBgColor() & 255) : c.isBgPalette() ? palette(c.getBgColor()) : t.background;
  }
  // The colour most of a row's cells have.
  function rowBg(y) {
    var l = term.buffer.active.getLine(y), n = {}, best = term.options.theme.background, most = 0, c;
    for (var x = 0; l && x < term.cols; x++) { c = l.getCell(x, c); if (!c) continue; var k = cellBg(c); n[k] = (n[k] || 0) + 1; if (n[k] > most) { most = n[k]; best = k; } }
    return best;
  }
  function edges() {
    var b = term.buffer.active, top = rowBg(b.viewportY), bottom = rowBg(b.viewportY + term.rows - 1);
    var css = top === bottom ? bottom : 'linear-gradient(' + top + ' 12px, ' + bottom + ' 12px)';
    if (css !== painted) { painted = css; document.documentElement.style.background = css; document.body.style.background = css; }
  }
  function follow() { if (!away && term.buffer.active.viewportY !== live()) term.scrollToLine(live()); }
  function size() {
    if (cfg.phone) {
      var h = room();
      if (window.innerWidth !== wide) { wide = window.innerWidth; tall = h + (tall ? 0 : cfg.raised); }
      else if (h > tall) tall = h;
      box.style.bottom = 'auto';
      box.style.height = tall + 'px';
    }
    try { fit.fit(); } catch (e) {}
    follow();
    lift();
    var now = term.cols + 'x' + term.rows;
    if (now !== last && term.cols > 0 && term.rows > 0) { var first = !last; last = now; post({ t: first ? 'ready' : 'size', cols: term.cols, rows: term.rows }); }
  }
  // Every column that fits. FitAddon keeps room for the ruler, a whole column when the screen is an exact number of them.
  function columns(cols) {
    var cell = term.element.querySelector('.xterm-screen').getBoundingClientRect().width / term.cols;
    return cell > 0 ? Math.max(cols, Math.floor((box.getBoundingClientRect().width + 0.01) / cell)) : cols;
  }
  function setFont(n) { if (n !== term.options.fontSize) { term.options.fontSize = n; size(); } }
  // Where a row's text ends, in cells (Chinese takes two), and the character there.
  function ending(l) {
    for (var x = l ? l.length - 1 : -1; x >= 0; x--) { var k = l.getCell(x); if (k && k.getChars().trim()) return { end: x + Math.max(1, k.getWidth()), wide: k.getWidth() === 2 }; }
    return { end: 0, wide: false };
  }
  function indent(l) { for (var x = 0; l && x < l.length; x++) { var k = l.getCell(x); if (k && k.getChars().trim()) return x; } return l ? l.length : 0; }
  // Did an agent's TUI break this row's line at the right edge? It writes each row on its own, so the terminal does
  // not know. It did when the next row's first word would not have fitted after this row's text, and the next row goes
  // on at the same indent or deeper, without starting a list item, a quote, a heading or a box.
  function tuiWraps(y) {
    var b = term.buffer.active, l = b.getLine(y), n = b.getLine(y + 1);
    if (!l || !n || n.isWrapped) return null;
    var e = ending(l), from = indent(n);
    if (!e.end || e.end < term.cols / 2 || from >= n.length || from < indent(l)) return null;
    var head = n.translateToString(true, from, Math.min(n.length, from + 6));
    if (/^([-*+>#|•·⏺⎿\\u2500-\\u257f]|\\d+[.)] )/.test(head)) return null;
    var first = n.getCell(from), joined = e.wide || first.getWidth() === 2, w = 0;
    if (first.getWidth() === 2) w = 2;
    else for (var x = from; x < n.length; x++) { var k = n.getCell(x); if (!k.getChars().trim() || k.getWidth() === 2) break; w++; }
    if (e.end + (joined ? 0 : 1) + w <= term.cols) return null;
    return { from: from, gap: joined ? '' : ' ' };
  }
  // What a selection copies: rows the terminal wrapped, and rows a TUI wrapped, join into the line they were.
  var span = null;
  function selectedText() {
    var all = term.getSelection();
    if (!span || !all) return all;
    var b = term.buffer.active, s = span.s, e = span.e, text = '', start = s.col;
    for (var y = s.row; y <= e.row; y++) {
      var l = b.getLine(y); if (!l) break;
      var n = y < e.row ? b.getLine(y + 1) : null, w = n ? tuiWraps(y) : null;
      var end = y === e.row ? e.col + 1 : l.length;
      text += start < end ? l.translateToString(!(n && n.isWrapped), start, end) : '';
      start = 0;
      if (!n || n.isWrapped) continue;
      if (w) {
        text = text.replace(/\\s+$/, '');
        // A link longer than a row is cut anywhere: its pieces join with nothing between them.
        text += /https?:\\/\\/\\S*$/.test(text) && ending(l).end >= term.cols ? '' : w.gap;
        start = w.from;
      } else text += '\\n';
    }
    return text;
  }
  function screenText() {
    var sel = selectedText(); if (sel) return sel;
    var b = term.buffer.active, out = [];
    for (var i = b.viewportY; i < b.viewportY + term.rows; i++) { var l = b.getLine(i); out.push(l ? l.translateToString(true) : ''); }
    return out.join('\\n').replace(/\\s+$/, '');
  }
  // The keyboard: only when the app asks. xterm focuses its textarea on every mouse event, so between two asks the
  // textarea says it wants no keyboard.
  function keyboard() { var ta = term.textarea; if (ta && ta.getAttribute('inputmode') !== 'text') { ta.blur(); ta.setAttribute('inputmode', 'text'); } forget(); term.focus(); post({ t: 'focused' }); }
  // Where a finger is, in cells of the whole buffer.
  function cell(x, y) {
    var r = term.element.querySelector('.xterm-screen').getBoundingClientRect();
    var w = r.width / term.cols, h = r.height / term.rows;
    var col = Math.max(0, Math.min(term.cols - 1, Math.floor((x - r.left) / w)));
    var row = Math.max(0, Math.min(term.rows - 1, Math.floor((y - r.top) / h)));
    return { col: col, row: row + term.buffer.active.viewportY, h: h };
  }
  // A row's text, and where each cell starts in it (a wide character takes two cells and one place in the text).
  function rowOf(y) {
    var l = term.buffer.active.getLine(y), text = '', at = [], c;
    var written = 0;
    for (var x = 0; l && x < l.length; x++) { at.push(text.length); c = l.getCell(x); text += c ? c.getChars() || (c.getWidth() ? ' ' : '') : ' '; if (c && c.getChars()) written = text.length; }
    // written: the text up to its last written cell; a space typed before the terminal's wrap is kept, an empty cell is not.
    return { text: text, at: at, wrapped: !!(l && l.isWrapped), written: written };
  }
  // Does the text of row y go on in row y + 1? The terminal says when it wrapped it; a TUI wraps by itself, with its
  // indent, at the right edge, and a link's characters are then on both sides of the break.
  function flows(y) {
    var a = rowOf(y).text.replace(/\\s+$/, ''), n = rowOf(y + 1);
    return n.wrapped || (ending(term.buffer.active.getLine(y)).end >= term.cols - 6 && URLCHAR.test(a[a.length - 1] || ' ') && URLCHAR.test(n.text.replace(/^\\s+/, '')[0] || ' '));
  }
  // The text around a tap, rows joined as they were written, and where the tap is in it (null: in a row's indent).
  function around(c) {
    var first = c.row, rows = 0;
    while (first > 0 && rows++ < 8 && flows(first - 1)) first--;
    var text = '', base = 0, y;
    for (y = first; ; y++) {
      var r = rowOf(y), piece = r.text;
      var wrapped = r.wrapped || y === first;
      if (!wrapped) {
        // Only a link or a path goes on across a TUI's break: other text keeps its lines.
        if (!/(https?:\\/\\/|[\\/\\\\])\\S*$/.test(text)) { if (y > c.row) break; text = ''; }
        piece = piece.replace(/^\\s+/, '');
      }
      if (y === c.row) {
        var lead = r.text.length - piece.length, i = r.at[c.col] === undefined ? r.text.length : r.at[c.col];
        if (i < lead) return null;
        base = text.length + i - lead;
      }
      text += rowOf(y + 1).wrapped ? piece.slice(0, Math.max(0, piece.length - (r.text.length - r.written))) : flows(y) ? piece.replace(/\\s+$/, '') : piece;
      if (y >= c.row && !flows(y)) break;
      if (y - first > 12) break;
    }
    return { text: text, base: base };
  }
  function linkInText(c) {
    var a = around(c); if (!a) return '';
    var text = a.text, base = a.base;
    var re = /https?:\\/\\/[^\\s"'<>\\u2500-\\u257f\\u3000-\\u9fff\\uff00-\\uffef]+/g, m;
    while ((m = re.exec(text))) { var url = m[0].replace(/[.,;:!?)\\]]+$/, ''); if (base >= m.index && base < m.index + url.length) return url; }
    return '';
  }
  // An image's path under a tap: only its shape is judged here, a path that ends in an image type. Which file it is
  // (a relative one is from the folder the terminal's program is in, on the machine the terminal is on) and whether it
  // may be read, the Companion says (POST /api/image/check). A name may have spaces, one at a time and none next to a
  // slash: macOS names a screenshot with its date and time, and an external disk is often named with spaces.
  // A tap asks the app whether the path is a picture on the computer before anything opens (askTap); the rows on
  // screen are looked through once a frame at most for the underline (scanPictures).
  var IMAGE_ABS = /(^|[\\s("'<\\[=:])((?:~|[A-Za-z]:)?(?:[\\/\\\\][^\\s"'<>()\\[\\]{}|\\/\\\\]+(?: [^\\s"'<>()\\[\\]{}|\\/\\\\]+)*)*[\\/\\\\][^"'<>()\\[\\]{}|\\/\\\\\\n]*?\\.(?:png|jpe?g|gif|webp))(?![\\w])/gi;
  var IMAGE_REL = /(^|[\\s("'<\\[=:])((?:[^\\s"'<>()\\[\\]{}|\\/\\\\:]+[\\/\\\\])*[^\\s"'<>()\\[\\]{}|\\/\\\\:]+\\.(?:png|jpe?g|gif|webp))(?![\\w])/gi;
  var LINKS = /https?:\\/\\/[^\\s"'<>\\u2500-\\u257f\\u3000-\\u9fff\\uff00-\\uffef]+/g;
  // A path stops at quotes, brackets and the marks and lines a TUI draws, and at two spaces.
  var NOT_PATH = /["'<>()\\[\\]{}|\\x60\\u2000-\\u2bff\\u3000-\\u303f\\uff00-\\uffef]/;
  // A row's characters, one a cell; the second cell of a wide character is ''.
  function cellsOf(y) {
    var l = term.buffer.active.getLine(y), out = [];
    for (var x = 0; l && x < l.length; x++) { var k = l.getCell(x); out.push(k ? k.getChars() || (k.getWidth() ? ' ' : '') : ' '); }
    return out;
  }
  function blank(ch) { return ch === undefined || ch === ' '; }
  // Where the text holding cell x starts: back over single spaces, up to two spaces or a character no path has.
  function runStart(cs, x) {
    while (x > 0) {
      var ch = cs[x - 1];
      if (ch === ' ') { if (x < 2 || blank(cs[x - 2]) || NOT_PATH.test(cs[x - 2])) break; }
      else if (ch && NOT_PATH.test(ch)) break;
      x--;
    }
    while (x < cs.length && cs[x] === ' ') x++;
    return x;
  }
  // Where the text from cell x ends (single spaces go on; two end it, and so does one before a mark no path holds,
  // as in Claude Code's "Phot (158.5" at 40 columns), and where text starts again after such a gap.
  function runEnd(cs, x) {
    var end = x, i;
    for (i = x; i < cs.length; i++) { if (cs[i] !== ' ') end = i + 1; else if (blank(cs[i + 1]) || NOT_PATH.test(cs[i + 1] || '')) break; }
    for (i = end; i < cs.length && blank(cs[i]); i++);
    return { end: end, next: i < cs.length && (i > end + 1 || (i === end + 1 && NOT_PATH.test(cs[i]))) ? i : -1 };
  }
  // The rows of a path a TUI cut, up from the row its type ends on (cell x of row y). A row above goes on with it when
  // its text starts at the same left edge, after a space or a mark: Claude Code 2.1.322 sends files as three columns,
  // "›"/"[image]", the path and the size (recorded in e2e/tui-paths). Or the terminal wrapped it. Or the TUI wrapped it
  // at the screen's edge, the next row with its own indent.
  function pathRows(y, x) {
    var b = term.buffer.active, cs = cellsOf(y), l = b.getLine(y), rows = [{ y: y, cs: cs, x0: l && l.isWrapped ? 0 : runStart(cs, x) }];
    for (var u = y - 1; u >= 0 && rows.length < 9; u--) {
      var top = rows[0], up = cellsOf(u), x0 = top.x0, below = b.getLine(u + 1), e;
      if (below && below.isWrapped && x0 === 0) rows.unshift({ y: u, cs: up, x0: 0, edge: true, wrap: true });
      else if (!blank(up[x0]) && up[x0] !== '' && (x0 === 0 || blank(up[x0 - 1]) || NOT_PATH.test(up[x0 - 1]))) {
        // A row whose text there ends in an image type ends a path of its own (Claude Code lists files one under another).
        if (/\\.(?:png|jpe?g|gif|webp)$/i.test(up.slice(x0, runEnd(up, x0).end).join(''))) break;
        rows.unshift({ y: u, cs: up, x0: x0 });
      }
      else if ((e = ending(b.getLine(u))).end >= term.cols - 6 && !NOT_PATH.test(up[e.end - 1] || ' ') && !blank(top.cs[x0]) && !NOT_PATH.test(top.cs[x0])) rows.unshift({ y: u, cs: up, x0: runStart(up, e.end - 1), edge: true });
      else break;
    }
    return rows;
  }
  // The rows' text joined, from row "from" on, for a column that ends at cell x1. ys/xs/ws: the cell each character came
  // from (y -1: a space a cut took out). A row that ends before the column's edge was cut at a space, which the cut
  // took out, or after a hyphen, a slash or a dot (Codex 0.167.1 cuts after hyphens; OpenCode 1.19.0 after all three,
  // as in "home-screen." then "png"). A row the
  // TUI cut near the screen's edge may have been cut at a space too (OpenCode's "/Photo" then "Archive 2TB" at 40
  // columns): "spaced" reads it so.
  function joined(rows, from, x1, spaced) {
    var j = { text: '', ys: [], xs: [], ws: [] };
    for (var k = from; k < rows.length; k++) {
      var r = rows[k], cs = r.cs, last = k === rows.length - 1, n0 = j.text.length;
      var to = r.edge ? ending(term.buffer.active.getLine(r.y)).end : last ? cs.length : Math.min(r.end, x1);
      for (var x = r.x0; x < to; x++) {
        if (cs[x] === '') continue;
        for (var i = 0; i < cs[x].length; i++) { j.text += cs[x][i]; j.ys.push(r.y); j.xs.push(x); j.ws.push(cs[x + 1] === '' ? 2 : 1); }
      }
      while (j.text.length > n0 && j.text[j.text.length - 1] === ' ') { j.text = j.text.slice(0, -1); j.ys.pop(); j.xs.pop(); j.ws.pop(); }
      if (last) break;
      var n = j.text.length, full = r.edge ? r.wrap || !spaced : n > n0 && j.xs[n - 1] + j.ws[n - 1] >= x1;
      if (!full && !/[-.\\/\\\\]/.test(j.text[n - 1] || '')) { j.text += ' '; j.ys.push(-1); j.xs.push(-1); j.ws.push(0); }
    }
    return j;
  }
  // Where the path's column ends on the right, no TUI draws: a row with nothing after it ends at or before it, text
  // after a gap starts at or after it. Each place between is a reading; so is each row a path may start on.
  function readings(rows) {
    var lo = 0, hi = term.cols, ends = [], k, x1, out = [], starts = [0];
    for (k = 0; k < rows.length; k++) {
      var r = rows[k]; if (r.edge) continue;
      var e = runEnd(r.cs, r.x0); r.end = e.end;
      if (e.next >= 0) hi = Math.min(hi, e.next);
      if (k < rows.length - 1) ends.push(e.end);
      if (k && k < rows.length - 1 && /^(?:[\\/\\\\~]|[A-Za-z]:[\\/\\\\])/.test(r.cs.slice(r.x0, r.x0 + 3).join(''))) starts.push(k);
    }
    if (rows.length > 1) starts.push(rows.length - 1);
    for (k = 0; k < ends.length; k++) if (ends[k] <= hi && ends[k] > lo) lo = ends[k];
    // Few places between: each; many (a row that tells little, far left): the two ends, and the cell before the
    // nearest column, since Claude Code starts its size column with a space (" (158.5KB)" cut to " (158.5K" and "B)").
    var xs = [];
    if (!lo) xs.push(hi);
    else if (hi - lo <= 3) for (x1 = lo; x1 <= hi; x1++) xs.push(x1);
    else xs.push(lo, hi);
    if (hi < term.cols && hi - 1 > lo && xs.indexOf(hi - 1) < 0) xs.push(hi - 1);
    var cut = rows.some(function (r) { return r.edge && !r.wrap; });
    for (k = 0; k < starts.length; k++) for (var i = 0; i < xs.length; i++) {
      out.push({ j: joined(rows, starts[k], xs[i]), rank: k * 10 + i });
      if (cut) out.push({ j: joined(rows, starts[k], xs[i], true), rank: k * 10 + i + 0.5 });
    }
    return out;
  }
  // Image paths that end on buffer rows first..last, each with the cells it is drawn on ([row, from, to]), in every
  // reading of the rows a TUI cut it over. Which reading is a picture, the Companion says. What a row gives is kept
  // while it and the 8 rows above it read the same: a TUI redraws a spinner or a clock many times a second.
  var ended = {}, endedCount = 0;
  function picturesOn(first, last) {
    var out = [], seen = {}, b = term.buffer.active, side = b.type === 'alternate' ? 'a' : 'n', texts = {}, m, y;
    first = Math.max(0, first); last = Math.min(last, b.length - 1);
    for (y = Math.max(0, first - 8); y <= last; y++) texts[y] = rowOf(y).text;
    if (endedCount > 2000) { ended = {}; endedCount = 0; }
    for (y = first; y <= last; y++) {
      var key = term.cols + '|' + [texts[y - 8], texts[y - 7], texts[y - 6], texts[y - 5], texts[y - 4], texts[y - 3], texts[y - 2], texts[y - 1], texts[y]].join('\\n');
      var kept = ended[side + y];
      if (!kept || kept.key !== key) {
        if (!kept) endedCount++;
        kept = ended[side + y] = { key: key, found: endingOn(y, texts[y], texts[y - 1]) };
      }
      for (var i = 0; i < kept.found.length; i++) { var f = kept.found[i], k = f.path + ' ' + f.cells.join(';'); if (!seen[k]) { seen[k] = true; out.push(f); } }
    }
    return out;
  }
  function endingOn(y, text, above) {
    var out = [], seen = {}, anchors = [], m, re = /\\.(?:png|jpe?g|gif|webp)(?![\\w])/gi, r;
    if (/\\./.test(text)) { r = rowOf(y); while ((m = re.exec(text))) anchors.push(r.at.indexOf(m.index)); }
    // A type cut in two: ".p" ends the row above, "ng" starts a piece of this one.
    var head = above && /[^\\s.]\\.([a-z]{0,3})\\s*$/i.exec(above);
    if (head) {
      var cs = cellsOf(y);
      for (var x = 0; x < cs.length; x++) {
        if (!/[a-z]/i.test(cs[x]) || !(x === 0 || blank(cs[x - 1]) || NOT_PATH.test(cs[x - 1]))) continue;
        var word = ''; for (var w = x; w < cs.length && /[a-z]/i.test(cs[w]); w++) word += cs[w];
        if (/^\\.(?:png|jpe?g|gif|webp)$/i.test('.' + head[1] + word)) anchors.push(x);
      }
    }
    for (var a = 0; a < anchors.length; a++) {
      if (anchors[a] < 0) continue;
      var reads = readings(pathRows(y, anchors[a]));
      for (var n = 0; n < reads.length; n++) pathsInText(reads[n].j, y, reads[n].rank, out, seen);
    }
    return out;
  }
  function pathsInText(j, y, rank, out, seen) {
    var links = [], m, res = [IMAGE_ABS, IMAGE_REL];
    for (LINKS.lastIndex = 0; (m = LINKS.exec(j.text)); ) links.push([m.index, m.index + m[0].length]);
    for (var k = 0; k < res.length; k++) {
      var re = res[k]; re.lastIndex = 0;
      while ((m = re.exec(j.text))) {
        var from = m.index + m[1].length, to = from + m[2].length, taken = j.ys[to - 1] !== y || m[2].length > 1024;
        re.lastIndex = from + 1;
        for (var i = 0; i < links.length && !taken; i++) taken = from < links[i][1] && links[i][0] < to;
        if (taken) continue;
        var cells = [];
        for (i = from; i < to; i++) {
          if (j.ys[i] < 0) continue;
          var c = cells[cells.length - 1];
          if (c && c[0] === j.ys[i]) c[2] = Math.max(c[2], j.xs[i] + j.ws[i]); else cells.push([j.ys[i], j.xs[i], j.xs[i] + j.ws[i]]);
        }
        var key = m[2] + ' ' + cells.join(';');
        if (!seen[key]) { seen[key] = true; out.push({ path: m[2], cells: cells, rank: k * 1000 + rank }); }
      }
    }
  }
  // The paths on rows first..last: those an agent linked, then those read from the text (up to "ends" for paths cut
  // over rows) where no link says what is there.
  function picturesFor(first, last, ends) {
    var linkedOnes = linkedPictures(first - 8, ends), out = linkedOnes.slice(), read = picturesOn(first, ends);
    for (var i = 0; i < read.length; i++) {
      var f = read[i], over = false;
      for (var j = 0; j < linkedOnes.length && !over; j++) over = overlaps(f.cells, linkedOnes[j].cells);
      if (!over) out.push(f);
    }
    return out;
  }
  function overlaps(a, b) {
    for (var i = 0; i < a.length; i++) for (var j = 0; j < b.length; j++) if (a[i][0] === b[j][0] && a[i][1] < b[j][2] && b[j][1] < a[i][2]) return true;
    return false;
  }
  // A tap: the path under it the computer said is a picture (the longest, if several readings are); else the likeliest
  // reading, a full path before a relative one, from the most rows.
  function imageAt(c) {
    var k = linkUnder(c); if (k && k.picture) return k.picture;
    var found = picturesFor(c.row, c.row, c.row + 8), best = null, guess = null;
    for (var i = 0; i < found.length; i++) {
      var f = found[i], on = false;
      for (var k = 0; k < f.cells.length && !on; k++) on = f.cells[k][0] === c.row && c.col >= f.cells[k][1] && c.col < f.cells[k][2];
      if (!on) continue;
      var known = pictures[f.path];
      if (known && known.picture && (!best || f.path.length > best.path.length)) best = f;
      if (!guess || f.rank < guess.rank) guess = f;
    }
    return best ? best.path : guess ? guess.path : '';
  }
  // A tap that isn't on anything of the page's own: a click for a program that follows the mouse, and the app's tap.
  function tapOn(at) {
    if (term.modes.mouseTrackingMode !== 'none') click(at.x, at.y);
    post({ t: 'tap' });
  }
  // Pictures, as the computer said (POST /api/image/check, through the app): path → { picture, at }. A tap on an
  // image path waits for the app's answer: a picture, the app opens it; not one (only mentioned, not a picture, not
  // allowed), the tap goes on as on any text. No answer within 8 s: the tap is dropped.
  var pictures = {}, pictureCount = 0, waiting = {}, nextAsk = 1;
  function knew(path, picture) {
    if (!pictures[path] && ++pictureCount > 500) { pictures = {}; pictureCount = 1; }
    pictures[path] = { picture: picture, at: Date.now() };
  }
  function askTap(path, at) {
    var id = nextAsk++;
    waiting[id] = { path: path, x: at.x, y: at.y };
    setTimeout(function () { delete waiting[id]; }, 8000);
    post({ t: 'image', path: path, id: id });
  }
  function tapAnswered(id, picture) {
    var w = waiting[id]; if (!w) return;
    delete waiting[id];
    knew(w.path, picture); scanSoon();
    if (!picture) tapOn(w);
  }
  // The underline: a line under each picture's path in the rows on screen, in a layer over the text (registerDecoration
  // draws nothing on a full-screen program's screen, the alternate buffer). Paths not known yet are asked about
  // together; one in a row written again is asked about again, if its answer is a second old (the agent may have
  // saved the file since). A path cut over rows is looked for up to 8 rows past the screen's edges, and gets a line on
  // each of its rows on screen.
  var rowsSeen = {}, rowsCount = 0, toAsk = [], asking = {}, askTimer = 0, scanFrame = 0, layer = null, drawn = '';
  function scanSoon() { if (!scanFrame) scanFrame = requestAnimationFrame(scanPictures); }
  function scanPictures() {
    scanFrame = 0;
    var b = term.buffer.active, side = b.type === 'alternate' ? 'a' : 'n', now = Date.now(), lines = [], changed = {}, top = b.viewportY;
    if (rowsCount > 4000) { rowsSeen = {}; rowsCount = 0; }
    for (var y = Math.max(0, top - 8); y < Math.min(b.length, top + term.rows + 8); y++) {
      var text = rowOf(y).text, key = side + y;
      if (rowsSeen[key] !== text) changed[y] = true;
      if (rowsSeen[key] === undefined) rowsCount++;
      rowsSeen[key] = text;
    }
    var found = picturesFor(top, top + term.rows - 1, top + term.rows - 1 + 8);
    for (var i = 0; i < found.length; i++) {
      var f = found[i], shown = false, moved = false, k;
      for (k = 0; k < f.cells.length; k++) { var c = f.cells[k]; if (c[0] >= top && c[0] < top + term.rows) shown = true; if (changed[c[0]]) moved = true; }
      if (!shown) continue;
      var known = pictures[f.path];
      if (!asking[f.path] && (!known || (moved && now - known.at >= 1000))) { asking[f.path] = true; toAsk.push(f.path); }
      if (known && known.picture) for (k = 0; k < f.cells.length; k++) { c = f.cells[k]; if (c[0] >= top && c[0] < top + term.rows) lines.push([c[0] - top, c[1], c[2] - c[1]]); }
    }
    if (toAsk.length && !askTimer) askTimer = setTimeout(askPaths, 120);
    drawLines(lines);
    drawKitty(top);
  }
  function askPaths() {
    askTimer = 0;
    var paths = toAsk.splice(0, 64);
    if (paths.length) post({ t: 'paths', paths: paths });
    if (toAsk.length) askTimer = setTimeout(askPaths, 120);
  }
  function picturesAnswered(found) {
    for (var p in found) if (Object.prototype.hasOwnProperty.call(found, p)) { delete asking[p]; knew(p, found[p] === true); }
    scanSoon();
  }
  function drawLines(lines) {
    var screen = term.element && term.element.querySelector('.xterm-screen'); if (!screen) return;
    var cw = screen.clientWidth / term.cols, ch = screen.clientHeight / term.rows, color = term.options.theme.blue || '#3b82f6';
    var key = JSON.stringify(lines) + cw + ' ' + ch + color;
    if (key === drawn) return;
    drawn = key;
    if (!layer) { layer = document.createElement('div'); layer.className = 'picture-lines'; layer.style.cssText = 'position:absolute;left:0;top:0;right:0;bottom:0;pointer-events:none;z-index:5'; }
    if (layer.parentNode !== screen) screen.appendChild(layer);
    var html = '';
    for (var i = 0; i < lines.length; i++) html += '<div style="position:absolute;left:' + lines[i][1] * cw + 'px;top:' + ((lines[i][0] + 1) * ch - 2) + 'px;width:' + lines[i][2] * cw + 'px;border-bottom:1.5px solid ' + color + '"></div>';
    layer.innerHTML = html;
  }
  function forgetPictures() { rowsSeen = {}; rowsCount = 0; drawn = ''; scanSoon(); }
  // Kitty pictures (the app takes them out of the output, model/kitty.ts): id → { url, cols, rows, w, h }. A program
  // places one with Unicode placeholders: cells of U+10EEEE whose colour is the picture's id and whose marks number the
  // cell's row and column in it (a cell without them goes on from the one to its left). The picture fits the
  // columns × rows it was placed in, its shape kept and centred, as Ghostty draws it; each row of placeholder cells
  // shows its slice, on the cells' own background, so the marks never show. Cells whose picture the app no longer
  // has show only that background.
  var shown = {}, kittyLayer = null, kittyDrawn = '', MARK = {};
  cfg.marks.forEach(function (c, i) { MARK[c] = i; });
  function kittyPut(m) {
    var bin; try { bin = atob(m.png); } catch (e) { return; }
    var u = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) u[i] = bin.charCodeAt(i);
    var be = function (i) { return ((u[i] << 24) >>> 0) + (u[i + 1] << 16) + (u[i + 2] << 8) + u[i + 3]; };
    kittyDrop([m.id]);
    shown[m.id] = { url: URL.createObjectURL(new Blob([u], { type: 'image/png' })), cols: m.cols, rows: m.rows, w: be(16), h: be(20) };
    kittyDrawn = ''; scanSoon();
  }
  function kittyDrop(ids) {
    for (var id in shown) if (!ids || ids.indexOf(+id) >= 0) { URL.revokeObjectURL(shown[id].url); delete shown[id]; }
    kittyDrawn = ''; scanSoon();
  }
  // The runs of placeholder cells on screen row y (buffer row by): [{ x, n, id, row, col }].
  function kittyRuns(by) {
    var l = term.buffer.active.getLine(by), out = [], run = null, c;
    if (!l || l.translateToString(false).indexOf('\\udbfb\\udeee') < 0) return out;
    for (var x = 0; x < term.cols; x++) {
      c = l.getCell(x, c);
      var s = c ? c.getChars() : '';
      if (!c || s.codePointAt(0) !== 0x10eeee) { run = null; continue; }
      var marks = Array.from(s).slice(1).map(function (ch) { return MARK[ch.codePointAt(0)]; });
      var id = c.isFgRGB() || c.isFgPalette() ? c.getFgColor() : 0;
      if (marks[2] !== undefined) id = (id + marks[2] * 16777216) >>> 0;
      var row = marks[0], col = marks[1];
      if (run && run.id === id && (row === undefined || row === run.row) && (col === undefined || col === run.col + run.n)) { run.n++; continue; }
      run = { x: x, n: 1, id: id, row: row || 0, col: col || 0, bg: cellBg(c) };
      out.push(run);
    }
    return out;
  }
  function drawKitty(top) {
    var screen = term.element && term.element.querySelector('.xterm-screen'); if (!screen) return;
    var cw = screen.clientWidth / term.cols, ch = screen.clientHeight / term.rows, runs = [];
    for (var y = 0; y < term.rows; y++) kittyRuns(top + y).forEach(function (r) { r.y = y; runs.push(r); });
    if (!runs.length && !kittyLayer) return;
    var key = JSON.stringify(runs) + cw + ' ' + ch + Object.keys(shown).join(',');
    if (key === kittyDrawn) return;
    kittyDrawn = key;
    if (!kittyLayer) { kittyLayer = document.createElement('div'); kittyLayer.className = 'kitty-pictures'; kittyLayer.style.cssText = 'position:absolute;left:0;top:0;right:0;bottom:0;pointer-events:none;z-index:4'; }
    if (kittyLayer.parentNode !== screen) screen.appendChild(kittyLayer);
    var html = '';
    runs.forEach(function (r) {
      var p = shown[r.id], pic = '';
      if (p && p.w && p.h) {
        var W = p.cols * cw, H = p.rows * ch, s = Math.min(W / p.w, H / p.h), dw = p.w * s, dh = p.h * s;
        pic = 'background-image:url(' + p.url + ');background-repeat:no-repeat;background-size:' + dw + 'px ' + dh + 'px;background-position:'
          + ((W - dw) / 2 - r.col * cw) + 'px ' + ((H - dh) / 2 - r.row * ch) + 'px;';
      }
      html += '<div style="position:absolute;left:' + r.x * cw + 'px;top:' + r.y * ch + 'px;width:' + r.n * cw + 'px;height:' + ch + 'px;background-color:' + r.bg + ';' + pic + '"></div>';
    });
    kittyLayer.innerHTML = html;
  }
  // A link behind text (OSC 8): nothing in the buffer's public API says which cell has one, so note where each was
  // written, and what it was written over: text written there since without the link is no longer it (Claude Code
  // draws its next screen over a dialog's link). A link to a file that is an image is that picture's path, whole and
  // as the agent knows it, however the screen shows it: Claude Code 2.1.322 and Codex 0.167.1 link each path they show
  // with file:///<the full path>, a relative one and a bare file name too, each row of a cut path on its own
  // (recorded in e2e/tui-paths).
  var linked = [], linking = null;
  function here() { var b = term.buffer.active; return { at: term.registerMarker(0) || { line: b.baseY + b.cursorY }, col: b.cursorX }; }
  function spanText(k) {
    var b = term.buffer.active, a = k.start.at.line, z = k.end.at.line, out = '';
    if (a < 0 || z < 0 || z < a || z - a > 12) return null;
    for (var y = a; y <= z; y++) { var l = b.getLine(y); if (!l) return null; out += l.translateToString(false, y === a ? k.start.col : 0, y === z ? k.end.col : l.length) + '\\n'; }
    return out;
  }
  function filePicture(uri) {
    var m = /^file:\\/\\/(?:localhost)?(\\/[^?#]*)$/i.exec(uri), p;
    if (!m) return '';
    try { p = decodeURIComponent(m[1]); } catch (e) { return ''; }
    if (/^\\/[A-Za-z]:[\\/\\\\]/.test(p)) p = p.slice(1);
    return p.length <= 1024 && !/[\\n\\r\\0]/.test(p) && /\\.(?:png|jpe?g|gif|webp)$/i.test(p) ? p : '';
  }
  function hyperlink(data) {
    var uri = data.slice(data.indexOf(';') + 1);
    if (linking) {
      linking.end = here(); linking.text = spanText(linking);
      if (linking.text && linking.text.trim()) linked.push(linking);
      else { linking.start.at.dispose && linking.start.at.dispose(); linking.end.at.dispose && linking.end.at.dispose(); }
      linking = null;
      if (linked.length > 400) { var old = linked.shift(); old.start.at.dispose && old.start.at.dispose(); old.end.at.dispose && old.end.at.dispose(); }
    }
    var picture = filePicture(uri);
    if (/^https?:\\/\\//.test(uri)) linking = { url: uri, start: here() };
    else if (picture) linking = { picture: picture, start: here() };
    return false;
  }
  function inSpan(k, c) {
    var a = k.start.at.line, z = k.end.at.line;
    return a >= 0 && z >= 0 && (c.row > a || (c.row === a && c.col >= k.start.col)) && (c.row < z || (c.row === z && c.col < k.end.col));
  }
  // The newest link written over the cell, if the text there is still what it was written with.
  function linkUnder(c) {
    for (var i = linked.length - 1; i >= 0; i--) { var k = linked[i]; if (inSpan(k, c)) return spanText(k) === k.text ? k : null; }
    return null;
  }
  function linkBehind(c) { var k = linkUnder(c); return k && k.url ? k.url : ''; }
  // Pictures linked on rows first..last: each span still standing, with the cells it covers.
  function linkedPictures(first, last) {
    var out = [], b = term.buffer.active;
    for (var i = linked.length - 1; i >= 0; i--) {
      var k = linked[i], a = k.start.at.line, z = k.end.at.line;
      if (!k.picture || a < 0 || z < first || a > last || spanText(k) !== k.text) continue;
      var cells = [];
      for (var y = a; y <= z; y++) { var l = b.getLine(y), from = y === a ? k.start.col : 0, to = y === z ? k.end.col : (l ? l.length : 0); while (to > from && l && !l.getCell(to - 1).getChars().trim()) to--; if (to > from) cells.push([y, from, to]); }
      // Only the newest link over the same cells counts.
      if (cells.length && !out.some(function (o) { return o.cells.join(';') === cells.join(';'); })) out.push({ path: k.picture, cells: cells, rank: -1 });
    }
    return out;
  }
  function linkAt(c) { return linkBehind(c) || linkInText(c); }
  // The word under the finger, selected; a drag goes on from its start.
  function selectWord(c) {
    var l = term.buffer.active.getLine(c.row);
    var space = function (x) { var k = l && l.getCell(x); return !k || (k.getWidth() && !k.getChars().trim()); };
    if (space(c.col)) { term.select(c.col, c.row, 1); span = { s: c, e: c }; return c; }
    var a = c.col, b = c.col;
    while (a > 0 && !space(a - 1)) a--;
    while (b < term.cols - 1 && !space(b + 1)) b++;
    term.select(a, c.row, b - a + 1);
    span = { s: { col: a, row: c.row }, e: { col: b, row: c.row } };
    return { col: a, row: c.row, h: c.h };
  }
  function selectSpan(from, to) {
    var s = from, e = to;
    if (to.row < from.row || (to.row === from.row && to.col < from.col)) { s = to; e = from; }
    term.select(s.col, s.row, (e.row - s.row) * term.cols + (e.col - s.col) + 1);
    span = { s: s, e: e };
  }
  // Back in history by this many steps. A full-screen program has none here: xterm turns a wheel into what that program
  // asked for (its mouse encoding, or arrow keys in its cursor mode).
  function ownScroll() { return term.modes.mouseTrackingMode !== 'none' || term.buffer.active.type === 'alternate'; }
  // How many rows one wheel report moves a program that follows the mouse. It is the program's own choice: Pi 1.4.0
  // moves 1, OpenCode 1.19.0 3 (both measured), and nothing in the protocol tells. So the page looks: a report goes
  // out alone, and when the program has redrawn, the screen before and after says how far its content moved. After
  // 3 such looks a drag sends a report per that many rows of finger travel, so the content keeps up with the finger.
  // Until then, and for a program whose screen gives no answer, it is 1, a report per row as Termux sends. The looks
  // start over when a program enters or leaves the alternate screen. An arrow key moves one row.
  var wheel = { seen: [], misses: 0, probe: null };
  function forgetWheel() { if (wheel.probe) { clearTimeout(wheel.probe.quiet); clearTimeout(wheel.probe.timer); } wheel = { seen: [], misses: 0, probe: null }; }
  function wheelRows() {
    var n = {}, best = 1, most = 0;
    wheel.seen.forEach(function (k) { n[k] = (n[k] || 0) + 1; if (n[k] > most || (n[k] === most && k > best)) { most = n[k]; best = k; } });
    return best;
  }
  function screenRows() { var b = term.buffer.active, out = []; for (var r = 0; r < term.rows; r++) { var l = b.getLine(b.viewportY + r); out.push(l ? l.translateToString(false) : ''); } return out; }
  // How many rows the content moved from screen a to b, the way a report asked (down: 1, up: -1); 0 when no move
  // explains most of what changed. Cells are compared, so a panel that moved beside one that stayed still counts.
  function shifted(a, b, down) {
    var changed = 0, best = 0, score = 0, i, j, k;
    for (i = 0; i < b.length; i++) for (j = 0; j < b[i].length; j++) if (b[i][j] !== ' ' && b[i][j] !== a[i][j]) changed++;
    for (k = 1; k <= b.length / 2; k++) {
      var hit = 0;
      for (i = 0; i < b.length; i++) {
        var from = a[i - down * k];
        if (from === undefined) continue;
        for (j = 0; j < b[i].length; j++) { var c = b[i][j]; if (c !== ' ' && c !== a[i][j] && c === from[j]) hit++; }
      }
      if (hit > score) { score = hit; best = k; }
    }
    return score >= 4 && score * 2 >= changed ? best : 0;
  }
  // Sends one report and holds the next until the program's redraw (output quiet for 40 ms) answers: 300 ms with no
  // output, or 600 ms with no answer, is none. One look per drag, and a program that gives none 5 times gets no more.
  function look(lines, x, y, then) {
    var p = wheel.probe = { before: screenRows(), down: lines > 0 ? 1 : -1, then: then };
    p.done = function (k) {
      if (wheel.probe !== p) return;
      clearTimeout(p.quiet); clearTimeout(p.timer); wheel.probe = null;
      if (k) wheel.seen.push(k); else wheel.misses++;
      p.then(k);
    };
    p.timer = setTimeout(function () { if (!p.wrote) p.done(0); else p.timer = setTimeout(function () { p.done(0); }, 300); }, 300);
    scrollView(lines > 0 ? 1 : -1, x, y);
  }
  function redrawn() {
    var p = wheel.probe;
    if (!p) return;
    p.wrote = true;
    clearTimeout(p.quiet);
    p.quiet = setTimeout(function () { var k = shifted(p.before, screenRows(), p.down); if (k) p.done(k); }, 40);
  }
  function looking() { return wheel.seen.length < 3 && wheel.misses < 5; }
  // Rows of finger travel per step.
  function rowsPerStep() { return term.modes.mouseTrackingMode !== 'none' ? wheelRows() : 1; }
  // x, y: where the finger went down. It picks the panel, and a program can ignore a wheel elsewhere (OpenCode's prompt box).
  function scrollView(lines, x, y) {
    if (!ownScroll()) { term.scrollLines(-lines); if (term.buffer.active.viewportY > live()) term.scrollToLine(live()); return where(); }
    for (var i = Math.abs(lines); i > 0; i--) term.element.dispatchEvent(new WheelEvent('wheel', { deltaY: lines > 0 ? -1 : 1, deltaMode: 1, clientX: x, clientY: y, bubbles: true, cancelable: true }));
  }
  // A click where the finger was, for a program that follows the mouse. touchend stops the mouse events a tap would
  // make (they would focus the terminal and bring the keyboard up), so the page makes the two xterm needs; xterm encodes
  // them as the program asked (SGR, X10, ...). Its mousedown focuses the textarea, which wants no keyboard between asks.
  function click(x, y) {
    var el = term.element.querySelector('.xterm-screen'), o = { clientX: x, clientY: y, button: 0, buttons: 1, bubbles: true, cancelable: true, view: window };
    el.dispatchEvent(new MouseEvent('mousedown', o));
    o.buttons = 0;
    el.dispatchEvent(new MouseEvent('mouseup', o));
  }
  // After the finger lifts the view keeps its speed (px per ms, down = back in history) and loses it smoothly, the
  // curve native lists use: speed halves about every 225 ms. It moves in whole steps, so what is less than a step waits.
  // A program that follows the mouse glides as far as the page's own history would. Each frame goes to where the curve
  // is at that time since the lift, so a late frame (a busy phone) catches up and goes no further: moving a late
  // frame's whole gap at the speed before it sent a flick a third further after one 400 ms gap (e2e/page-touch.mjs).
  var gliding = 0;
  function stopGlide() { var was = !!gliding; if (gliding) cancelAnimationFrame(gliding); gliding = 0; return was; }
  function glide(v0, h, x, y) {
    v0 = Math.max(-8, Math.min(8, v0));
    if (Math.abs(v0) < 0.25) return;
    var start = performance.now(), sent = 0;
    function step(now) {
      var t = Math.max(0, now - start), u = h * rowsPerStep(), v = v0 * Math.exp(-t / 325);
      var n = wheel.probe ? 0 : Math.trunc((v0 * 325 - v * 325 - sent) / u), from = term.buffer.active.viewportY;
      if (n) { scrollView(n, x, y); sent += n * u; }
      var ended = Math.abs(v) < 0.03 || (n && !ownScroll() && term.buffer.active.viewportY === from);
      gliding = ended ? 0 : requestAnimationFrame(step);
    }
    gliding = requestAnimationFrame(step);
  }
  function touches() {
    var t0 = null, selecting = null, press = 0, scrolled = 0, moved = false, dragging = false, trail = [], stopped = false, drag = null;
    // Steps for the finger's travel so far. Travel short of a step is kept for the next move, so a finger that goes
    // back cancels it. While the page looks how far a report goes, the travel waits, and the answer counts.
    function keepUp() {
      if (!drag || wheel.probe) return;
      var u = drag.h * rowsPerStep(), steps = Math.trunc((drag.dy - scrolled) / u);
      if (!steps) return;
      if (term.modes.mouseTrackingMode !== 'none' && looking() && !drag.looked) {
        var g = drag, guess = u;
        g.looked = true;
        scrolled += steps > 0 ? guess : -guess;
        look(steps, g.x, g.y, function (k) { if (g === drag && k) scrolled += (steps > 0 ? 1 : -1) * (k * g.h - guess); keepUp(); });
        return;
      }
      scrollView(steps, drag.x, drag.y); scrolled += steps * u;
    }
    // touchstart keeps its default: it is what gives the WebView the system's focus, without which focusing the
    // textarea brings no keyboard up (Android, measured). touch-action:none already stops panning and zooming.
    // touchend never does: its default is the mouse events a tap turns into.
    box.addEventListener('touchstart', function (e) {
      post({ t: 'touched' });
      clearTimeout(press);
      stopped = stopGlide();
      if (e.touches.length !== 1) { t0 = null; selecting = null; return; }
      var p = e.touches[0];
      t0 = { x: p.clientX, y: p.clientY, at: Date.now() }; moved = false; scrolled = 0; selecting = null; dragging = false; trail = []; drag = null;
      press = setTimeout(function () { if (!t0 || moved) return; selecting = selectWord(cell(t0.x, t0.y)); }, 450);
    }, { passive: true });
    box.addEventListener('contextmenu', function (e) { e.preventDefault(); });
    box.addEventListener('touchmove', function (e) {
      e.preventDefault();
      if (!t0 || e.touches.length !== 1) return;
      var p = e.touches[0], dx = p.clientX - t0.x, dy = p.clientY - t0.y;
      if (Math.abs(dx) > 8 || Math.abs(dy) > 8) { moved = true; clearTimeout(press); }
      if (selecting) { selectSpan(selecting, cell(p.clientX, p.clientY)); return; }
      if (moved && Math.abs(dy) > Math.abs(dx)) {
        if (!drag) drag = { x: t0.x, y: t0.y, looked: false };
        drag.dy = dy; drag.h = cell(p.clientX, p.clientY).h;
        keepUp();
        dragging = true;
        trail.push({ y: p.clientY, at: e.timeStamp });
        while (trail.length > 2 && e.timeStamp - trail[0].at > 100) trail.shift();
      }
    }, { passive: false });
    box.addEventListener('touchend', function (e) {
      clearTimeout(press);
      e.preventDefault();
      if (!t0 || e.touches.length) return;
      var p = e.changedTouches[0], dx = p.clientX - t0.x, dy = p.clientY - t0.y, ms = Date.now() - t0.at, start = t0;
      t0 = null;
      if (selecting) { selecting = null; var text = selectedText(); if (text) post({ t: 'selected', text: text }); return; }
      if (dragging && trail.length > 1 && e.timeStamp - trail[trail.length - 1].at < 60) {
        var a = trail[0], z = trail[trail.length - 1];
        glide((z.y - a.y) / (z.at - a.at), cell(p.clientX, p.clientY).h, start.x, start.y);
        return;
      }
      if (Math.abs(dx) > 70 && Math.abs(dx) > 2 * Math.abs(dy) && ms < 600 && scrolled === 0) { post({ t: 'swipe', dir: dx < 0 ? 'next' : 'prev' }); return; }
      if (!moved && ms < 450 && !stopped) {
        term.clearSelection(); span = null;
        var url = linkAt(cell(start.x, start.y));
        if (url) { post({ t: 'link', url: url }); return; }
        var image = imageAt(cell(start.x, start.y));
        if (image) { askTap(image, start); return; }
        tapOn(start);
      }
    }, { passive: false });
  }
  // A phone's keyboard edits xterm's hidden field and says little about which key it was (keydown 229). xterm guesses
  // the bytes from the field and guesses wrong with Gboard (API 35 emulator): Enter sent "\\r" and then a
  // stray backspace, because xterm empties the field on Enter and then reads the shorter field as a deletion; a
  // correction picked from the suggestions sent the whole field again. So the page reads the field itself: whatever
  // the keyboard did to it since the last look (typed, corrected, composed and picked, deleted) goes to the program
  // as backspaces for what it took away and the text it put in. Enter empties it, as does any key xterm handles
  // (arrows, Tab, ctrl), the key bar and a paste, so the keyboard never corrects text the program may have moved
  // away from. A keyboard with real keys still goes through xterm for everything but text, Enter and backspace.
  var field = null, said = '', composing = false;
  function seen() {
    // iOS types a space as U+00A0 and swaps it for a plain one later: both are a space to the terminal.
    var now = field.value.replace(/\\u00a0/g, ' ');
    if (now === said) return;
    var a = Array.from(said), b = Array.from(now), p = 0, out = '';
    while (p < a.length && p < b.length && a[p] === b[p]) p++;
    for (var i = p; i < a.length; i++) out += '\\x7f';
    out += b.slice(p).join('');
    said = now;
    if (out) post({ t: 'input', data: out });
  }
  function forget() { if (field) { field.value = ''; said = ''; composing = false; } }
  function typing() {
    field = term.textarea;
    if (!field) return;
    var mine = function (e) { return e.target === field; };
    var stop = function (e) { e.stopImmediatePropagation(); e.stopPropagation(); };
    var on = function (type, f) { term.element.addEventListener(type, function (e) { if (mine(e)) f(e); }, true); };
    on('compositionstart', function (e) { stop(e); composing = true; });
    on('compositionupdate', stop);
    // A word picked in Chinese, Japanese or Korean leaves the field: Gboard's pinyin otherwise keeps it as its own and
    // spends the next backspace on it, sending nothing (logcat trace, API 35 emulator).
    on('compositionend', function (e) { stop(e); composing = false; seen(); if (/[\\u3000-\\u30ff\\u3400-\\u9fff\\uac00-\\ud7af\\uff00-\\uffef]/.test(e.data || '')) setTimeout(function () { if (!composing) forget(); }, 0); });
    on('beforeinput', function (e) {
      stop(e);
      if (e.inputType === 'insertLineBreak' || e.inputType === 'insertParagraph') { e.preventDefault(); seen(); post({ t: 'input', data: '\\r' }); forget(); }
    });
    on('input', function (e) { stop(e); if (!composing && !e.isComposing) seen(); });
    on('keypress', stop);
    on('keydown', function (e) {
      var plain = !e.ctrlKey && !e.altKey && !e.metaKey;
      if (e.keyCode === 229 || e.key === 'Unidentified' || e.key === 'Process' || e.isComposing || composing) { stop(e); return; }
      if (e.key === 'Enter' && plain && !e.shiftKey) { stop(e); e.preventDefault(); seen(); post({ t: 'input', data: '\\r' }); forget(); return; }
      if (e.key === 'Backspace' && plain) {
        stop(e);
        if (!field.value || field.selectionEnd === 0) { e.preventDefault(); post({ t: 'input', data: '\\x7f' }); }
        return;
      }
      if (plain && e.key && Array.from(e.key).length === 1) { stop(e); return; }
      forget();
    });
  }
  function boot() {
    var keep = cfg.phone ? { overviewRuler: { width: 1 } } : {};
    term = new Terminal(Object.assign(keep, { fontFamily: cfg.fonts, fontSize: cfg.fontSize,
      fontWeight: cfg.fontWeight, fontWeightBold: cfg.fontWeightBold, lineHeight: cfg.lineHeight,
      scrollback: cfg.scrollback, cursorBlink: true, allowProposedApi: true, customGlyphs: true,
      theme: cfg.themes[cfg.dark ? 'dark' : 'light'] }));
    // The widths Claude Code and herdr-core count (emoji are 2 cells, a joined emoji one character); xterm's default is
    // Unicode 6, where they are 1 and all that follows on the line lands in the wrong column.
    term.loadAddon(new UnicodeGraphemesAddon.UnicodeGraphemesAddon());
    term.unicode.activeVersion = '15-graphemes';
    fit = new FitAddon.FitAddon();
    term.loadAddon(fit);
    if (cfg.phone) {
      var propose = fit.proposeDimensions.bind(fit);
      fit.proposeDimensions = function () { var d = propose(); if (d) d.cols = columns(d.cols); return d; };
    }
    term.open(box);
    // Opening the page must not bring the keyboard up.
    if (term.textarea) {
      term.textarea.setAttribute('inputmode','none'); term.textarea.blur();
      term.textarea.addEventListener('blur', function () { term.textarea.setAttribute('inputmode','none'); });
    }
    // WebGL draws box drawing as joined shapes; without it (or when the context is lost) the DOM renderer draws on.
    try { var gl = new WebglAddon.WebglAddon(); gl.onContextLoss(function () { gl.dispose(); }); term.loadAddon(gl); } catch (e) {}
    new ResizeObserver(size).observe(cfg.phone ? document.documentElement : box);
    term.onData(function (d) { post({ t: 'input', data: d }); });
    typing();
    // A program that asked for the mouse without SGR (mode 1006) gets X10 reports, which xterm hands out as bytes. Below
    // 95 columns and rows every byte is ASCII and goes as text unchanged; a phone has fewer.
    term.onBinary(function (d) { post({ t: 'input', data: d }); });
    // Scrolled back or at the bottom again; and, once output settles, the last lines up to the cursor.
    var settle = null;
    term.onScroll(function () { if (!cfg.phone) where(); });
    term.onWriteParsed(function () { follow(); where(); settled(); lift(); redrawn(); });
    term.buffer.onBufferChange(function () { forgetWheel(); forgetPictures(); });
    term.onRender(function () { edges(); scanSoon(); });
    term.onScroll(scanSoon);
    term.onResize(function () { drawn = ''; scanSoon(); });
    function lines(from, to) { var b = term.buffer.active, out = []; for (var i = from; i < to; i++) { var l = b.getLine(i); out.push(l ? l.translateToString(true) : ''); } return out; }
    function settled() {
      clearTimeout(settle);
      settle = setTimeout(function () { var b = term.buffer.active, end = b.baseY + b.cursorY + 1; post({ t: 'tail', lines: lines(Math.max(0, end - 20), end) }); }, 600);
    }
    // Which keys the program asked for (kitty keyboard protocol): the way it reads Ctrl with Enter or Tab.
    term.parser.registerOscHandler(8, hyperlink);
    term.parser.registerCsiHandler({ prefix: '>', final: 'u' }, function (p) { kitty.push(p[0] || 0); return false; });
    term.parser.registerCsiHandler({ prefix: '<', final: 'u' }, function () { kitty.pop(); return false; });
    term.parser.registerCsiHandler({ prefix: '=', final: 'u' }, function (p) { kitty[Math.max(0, kitty.length - 1)] = p[0] || 0; return false; });
    // Shift and Ctrl held from the key bar, as xterm writes modified keys (1 + 1 for Shift + 4 for Ctrl); Shift+Tab
    // alone is back-tab, which every program knows.
    function held(name, plain, shift, ctrl) {
      var mod = 1 + (shift ? 1 : 0) + (ctrl ? 4 : 0);
      if (name === 'tab' && shift && !ctrl) return '\\x1b[Z';
      var code = name === 'enter' ? 13 : name === 'tab' ? 9 : 0;
      if (code) return kitty[kitty.length - 1] ? '\\x1b[' + code + ';' + mod + 'u' : '\\x1b[27;' + mod + ';' + code + '~';
      return CSI[name] ? '\\x1b[1;' + mod + CSI[name] : plain;
    }
    var marked = null;
    function mark(line, label) {
      if (marked) marked.dispose();
      var b = term.buffer.active, m = term.registerMarker(line - (b.baseY + b.cursorY));
      if (!m) return;
      marked = term.registerDecoration({ marker: m, x: 0, width: term.cols, layer: 'top' });
      if (!marked) return;
      marked.onRender(function (el) {
        if (el.firstChild) return;
        el.style.pointerEvents = 'none';
        var c = term.options.theme.blue || '#6ea8fe';
        el.style.borderTop = '1px solid ' + c;
        var t = document.createElement('span');
        t.textContent = label;
        t.style.cssText = 'position:absolute;right:0;top:-0.75em;padding:0 6px;font:500 10px/1.5 system-ui,sans-serif;border-radius:4px;color:' + c + ';background:' + term.options.theme.background;
        el.appendChild(t);
      });
    }
    var CSI = { up: 'A', down: 'B', right: 'C', left: 'D', home: 'H', end: 'F' };
    window.addEventListener('message', function (e) {
      if (typeof e.data !== 'string') return;
      var m; try { m = JSON.parse(e.data); } catch (_) { return; }
      if (m.t === 'reset') { stopGlide(); term.reset(); kitty = []; span = null; linked = []; linking = null; forgetWheel(); pictures = {}; pictureCount = 0; asking = {}; toAsk = []; ended = {}; endedCount = 0; forgetPictures(); kittyDrop(); }
      else if (m.t === 'kitty' && typeof m.png === 'string' && m.id > 0 && m.cols > 0 && m.rows > 0) kittyPut(m)
      else if (m.t === 'kitty-drop') kittyDrop(Array.isArray(m.ids) ? m.ids : null)
      else if (m.t === 'image' && typeof m.id === 'number') tapAnswered(m.id, m.picture === true);
      else if (m.t === 'pictures' && m.paths && typeof m.paths === 'object') picturesAnswered(m.paths);
      else if (m.t === 'write') term.write(m.data);
      else if (m.t === 'paste') { forget(); term.paste(m.data); }
      else if (m.t === 'theme') { term.options.theme = cfg.themes[m.dark ? 'dark' : 'light']; edges(); scanSoon(); }
      else if (m.t === 'fontSize') setFont(m.size);
      else if (m.t === 'focus') keyboard();
      else if (m.t === 'copy') post({ t: 'copied', text: screenText() });
      else if (m.t === 'refit') { setTimeout(function () { tall = room(); size(); }, 150); }
      else if (m.t === 'bottom') { stopGlide(); term.scrollToLine(live()); where(); }
      else if (m.t === 'text') post({ t: 'text', base: 0, lines: lines(0, term.buffer.active.length) });
      else if (m.t === 'mark') mark(m.line, m.label);
      else if (m.t === 'key') {
        forget();
        var seq = m.name === 'esc' ? '\\x1b' : m.name === 'tab' ? '\\t' : m.name === 'btab' ? '\\x1b[Z' : m.name === 'enter' ? '\\r' : m.name === 'ctrlc' ? '\\x03' : m.name === 'pgup' ? '\\x1b[5~' : m.name === 'pgdn' ? '\\x1b[6~'
          : CSI[m.name] ? (term.modes.applicationCursorKeysMode ? '\\x1bO' : '\\x1b[') + CSI[m.name] : '';
        if (m.ctrl || m.shift) seq = held(m.name, seq, m.shift, m.ctrl);
        if (seq) post({ t: 'input', data: seq });
      }
    });
    touches();
    document.addEventListener('visibilitychange', function () { if (!document.hidden) post({ t: 'touched' }); });
    size();
  }
  // Cells are measured from the font, so wait for it (a moment at most).
  var booted = false;
  function once() { if (!booted) { booted = true; boot(); } }
  try { Promise.all([document.fonts.load(cfg.fontSize + "px 'JetBrains Mono'"), document.fonts.load(cfg.fontSize + "px 'Symbols Nerd Font'", '\\ue0b0'), document.fonts.load(cfg.fontSize + "px 'Noto Sans Symbols 2'", '\\u23fa'), document.fonts.load(cfg.fontSize + "px 'Noto Sans Symbols'", '\\u23bf'), document.fonts.load(cfg.fontSize + "px 'Noto Sans Mono'", '\\u23ba')]).then(once, once); } catch (e) { once(); }
  setTimeout(once, 1500);
})();`

/** The whole page, scripts and fonts inline: it loads nothing from anywhere. */
export function terminalPage(opts: { dark: boolean; fontSize: number; scrollback: number; phone?: boolean; raised?: number }): string {
  const themes = { dark: xtermTheme(true), light: xtermTheme(false) }
  const bg = themes[opts.dark ? 'dark' : 'light'].background
  const cfg = JSON.stringify({ phone: !!opts.phone, raised: opts.raised ?? 0, dark: opts.dark, fontSize: opts.fontSize, scrollback: opts.scrollback, themes, fonts: TERMINAL_FONTS, marks: KITTY_MARKS, ...LOOK })
  const fonts = `@font-face{font-family:'JetBrains Mono';src:url(data:font/woff;base64,${MONO_WOFF}) format('woff');font-weight:100 800}`
    + `@font-face{font-family:'Symbols Nerd Font';src:url(data:font/woff;base64,${SYMBOLS_WOFF}) format('woff')}`
    + `@font-face{font-family:'Noto Sans Symbols 2';src:url(data:font/woff;base64,${SYMBOLS2_WOFF}) format('woff')}`
    + `@font-face{font-family:'Noto Sans Symbols';src:url(data:font/woff;base64,${SYMBOLS1_WOFF}) format('woff')}`
    + `@font-face{font-family:'Noto Sans Mono';src:url(data:font/woff;base64,${NOTO_MONO_WOFF}) format('woff')}`
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">`
    + `<style>${fonts}\n${XTERM_CSS}\nhtml,body{margin:0;height:100%;background:${bg};overflow:hidden;touch-action:none;-webkit-user-select:none;user-select:none}#t{position:absolute;inset:${opts.phone ? '12px 0' : '12px 14px'}}</style></head>`
    + `<body><div id="t"></div><script>${XTERM_JS}</script><script>${FIT_JS}</script><script>${WEBGL_JS}</script><script>${GRAPHEMES_JS}</script><script>window.__terminal=${cfg};${BOOT}</script></body></html>`
}

/** A page message, or undefined for anything else. */
export function readPageMessage(data: string): PageMessage | undefined {
  let m: any
  try { m = JSON.parse(data) } catch { return undefined }
  if ((m?.t === 'ready' || m?.t === 'size') && Number.isInteger(m.cols) && Number.isInteger(m.rows) && m.cols > 0 && m.rows > 0) return { t: m.t, cols: m.cols, rows: m.rows }
  if (m?.t === 'input' && typeof m.data === 'string') return { t: 'input', data: m.data }
  if (m?.t === 'copied' && typeof m.text === 'string') return { t: 'copied', text: m.text }
  if (m?.t === 'selected' && typeof m.text === 'string') return { t: 'selected', text: m.text }
  if (m?.t === 'swipe' && (m.dir === 'next' || m.dir === 'prev')) return { t: 'swipe', dir: m.dir }
  if (m?.t === 'link' && typeof m.url === 'string' && /^https?:\/\//.test(m.url)) return { t: 'link', url: m.url }
  if (m?.t === 'image' && typeof m.path === 'string' && m.path && m.path.length <= 1024) return { t: 'image', path: m.path, id: Number.isInteger(m.id) ? m.id : 0 }
  if (m?.t === 'tap') return { t: 'tap' }
  if (m?.t === 'focused') return { t: 'focused' }
  if (m?.t === 'touched') return { t: 'touched' }
  if (m?.t === 'away' && typeof m.away === 'boolean') return { t: 'away', away: m.away }
  const strings = (v: any, max: number) => Array.isArray(v) && v.length <= max && v.every((l: any) => typeof l === 'string')
  if (m?.t === 'tail' && strings(m.lines, 100)) return { t: 'tail', lines: m.lines }
  if (m?.t === 'paths' && strings(m.paths, 64) && m.paths.every((p: string) => p && p.length <= 1024)) return { t: 'paths', paths: m.paths }
  if (m?.t === 'text' && strings(m.lines, 20000) && Number.isInteger(m.base) && m.base >= 0) return { t: 'text', base: m.base, lines: m.lines }
  return undefined
}

type Queued = { t: 'write'; data: string } | { t: 'reset' } | ({ t: 'kitty' } & KittyImage) | { t: 'kitty-drop'; ids?: number[] }

/** The app's side of the page: output waits until the page is ready, decoded as one UTF-8 stream, and writes
 * between two flushes go as one message. With the terminal's pictures (model/kitty.ts), the graphics commands are
 * taken out of the output, each picture goes to the page on its own before the text that shows it, and all of them
 * again after a reset or a page loaded again. */
export class PageChannel {
  private open = false
  private queue: Queued[] = []
  private timer: number | undefined
  private dec = new TextDecoder()
  private fresh = false
  constructor(private post: (message: string) => void, private later: (f: () => void, ms: number) => number = setTimeout, private images?: KittyImages) {}
  ready() {
    if (this.fresh && this.images) this.queue.unshift(...this.pictures())
    this.fresh = false
    this.open = true
    this.schedule()
  }
  /** The page went away (reloaded or torn down): nothing more is sent until it says ready again. */
  lost() { this.open = false; this.queue = []; this.timer = undefined; this.fresh = true }
  write(bytes: Uint8Array) {
    let data = this.dec.decode(bytes, { stream: true })
    if (this.images && data) {
      const r = this.images.take(data)
      data = r.text
      if (r.dropped) this.queue.push(r.dropped === 'all' ? { t: 'kitty-drop' } : { t: 'kitty-drop', ids: r.dropped })
      for (const img of r.put) this.queue.push({ t: 'kitty', ...img })
      if (r.dropped || r.put.length) this.schedule()
    }
    if (!data) return
    const tail = this.queue.at(-1)
    if (tail?.t === 'write') tail.data += data
    else this.queue.push({ t: 'write', data })
    this.schedule()
  }
  /** A new attachment repaints from scratch, with the pictures kept. */
  reset() { this.dec = new TextDecoder(); this.queue = [{ t: 'reset' }, ...this.pictures()]; this.schedule() }
  private pictures(): Queued[] { return (this.images?.all() ?? []).map((img) => ({ t: 'kitty', ...img })) }
  send(message: { t: 'key'; name: BarKey; ctrl?: boolean; shift?: boolean } | { t: 'paste'; data: string } | { t: 'theme'; dark: boolean } | { t: 'fontSize'; size: number } | { t: 'focus' } | { t: 'copy' }
    | { t: 'bottom' } | { t: 'refit' } | { t: 'text' } | { t: 'mark'; line: number; label: string }
    | { t: 'image'; id: number; picture: boolean } | { t: 'pictures'; paths: Record<string, boolean> }) {
    if (this.open) this.post(JSON.stringify(message))
  }
  private schedule() {
    if (!this.open || this.timer !== undefined) return
    this.timer = this.later(() => {
      this.timer = undefined
      if (!this.open) return
      const q = this.queue
      this.queue = []
      for (const m of q) this.post(JSON.stringify(m))
    }, 16)
  }
}
