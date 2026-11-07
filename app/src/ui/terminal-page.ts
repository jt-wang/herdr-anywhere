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

export type PageMessage = { t: 'ready' | 'size'; cols: number; rows: number } | { t: 'input'; data: string } | { t: 'copied'; text: string }
  | { t: 'selected'; text: string } | { t: 'swipe'; dir: 'next' | 'prev' } | { t: 'link'; url: string } | { t: 'tap' } | { t: 'touched' } | { t: 'focused' }
  | { t: 'away'; away: boolean } | { t: 'tail'; lines: string[] } | { t: 'text'; base: number; lines: string[] }
export type BarKey = 'esc' | 'tab' | 'btab' | 'enter' | 'ctrlc' | 'up' | 'down' | 'left' | 'right' | 'home' | 'end' | 'pgup' | 'pgdn'

/** Whether the terminal's WebView is left scrollable. On Android NSH 0.3.0's WebView (WebBox) swallows every finger
 *  move while it is not, and the page saw only touchstart and touchend: no drag or selection (measured
 *  2026-10-31 on the API 35 emulator over DevTools). The page never scrolls itself: touch-action is none. iOS keeps
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
    for (var x = 0; l && x < l.length; x++) { at.push(text.length); c = l.getCell(x); text += c ? c.getChars() || (c.getWidth() ? ' ' : '') : ' '; }
    return { text: text, at: at, wrapped: !!(l && l.isWrapped) };
  }
  // Does the text of row y go on in row y + 1? The terminal says when it wrapped it; a TUI wraps by itself, with its
  // indent, at the right edge, and a link's characters are then on both sides of the break.
  function flows(y) {
    var a = rowOf(y).text.replace(/\\s+$/, ''), n = rowOf(y + 1);
    return n.wrapped || (ending(term.buffer.active.getLine(y)).end >= term.cols - 6 && URLCHAR.test(a[a.length - 1] || ' ') && URLCHAR.test(n.text.replace(/^\\s+/, '')[0] || ' '));
  }
  function linkInText(c) {
    var first = c.row, rows = 0;
    while (first > 0 && rows++ < 8 && flows(first - 1)) first--;
    var text = '', base = 0, y;
    for (y = first; ; y++) {
      var r = rowOf(y), piece = r.text;
      var wrapped = r.wrapped || y === first;
      if (!wrapped) {
        // Only a link goes on across a TUI's break: other text keeps its lines.
        if (!/https?:\\/\\/\\S*$/.test(text)) { if (y > c.row) break; text = ''; }
        piece = piece.replace(/^\\s+/, '');
      }
      if (y === c.row) {
        var lead = r.text.length - piece.length, i = r.at[c.col] === undefined ? r.text.length : r.at[c.col];
        if (i < lead) return '';
        base = text.length + i - lead;
      }
      text += flows(y) ? piece.replace(/\\s+$/, '') : piece;
      if (y >= c.row && !flows(y)) break;
      if (y - first > 12) break;
    }
    var re = /https?:\\/\\/[^\\s"'<>\\u2500-\\u257f\\u3000-\\u9fff\\uff00-\\uffef]+/g, m;
    while ((m = re.exec(text))) { var url = m[0].replace(/[.,;:!?)\\]]+$/, ''); if (base >= m.index && base < m.index + url.length) return url; }
    return '';
  }
  // A link behind text (OSC 8): nothing in the buffer's public API says which cell has one, so note where each was written.
  var linked = [], linking = null;
  function here() { var b = term.buffer.active; return { at: term.registerMarker(0) || { line: b.baseY + b.cursorY }, col: b.cursorX }; }
  function hyperlink(data) {
    var uri = data.slice(data.indexOf(';') + 1);
    if (linking) { linking.end = here(); linked.push(linking); linking = null; if (linked.length > 200) linked.shift(); }
    if (/^https?:\\/\\//.test(uri)) { linking = { url: uri, start: here() }; }
    return false;
  }
  function linkBehind(c) {
    for (var i = linked.length - 1; i >= 0; i--) {
      var k = linked[i], a = k.start.at.line, z = k.end.at.line;
      if (a < 0 || z < 0) continue;
      if ((c.row > a || (c.row === a && c.col >= k.start.col)) && (c.row < z || (c.row === z && c.col < k.end.col))) return k.url;
    }
    return '';
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
  // Rows of finger travel per step. A program that follows the mouse gets a wheel report per 3 rows, the rate xterm
  // sends for a Mac trackpad (small pixel wheels count 0.3 of their rows); OpenCode moves 3 rows on each, so its
  // content keeps up with the finger. An arrow key moves one row.
  function rowsPerStep() { return term.modes.mouseTrackingMode !== 'none' ? 3 : 1; }
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
  // A program that follows the mouse glides as far as the page's own history would.
  var gliding = 0;
  function stopGlide() { var was = !!gliding; if (gliding) cancelAnimationFrame(gliding); gliding = 0; return was; }
  function glide(v, h, x, y) {
    v = Math.max(-8, Math.min(8, v));
    if (Math.abs(v) < 0.25) return;
    var last = performance.now(), carry = 0;
    h *= rowsPerStep();
    function step(now) {
      var dt = Math.max(0, now - last);
      last = now; carry += v * dt; v *= Math.exp(-dt / 325);
      var n = Math.trunc(carry / h), from = term.buffer.active.viewportY;
      if (n) { scrollView(n, x, y); carry -= n * h; }
      var ended = Math.abs(v) < 0.03 || (n && !ownScroll() && term.buffer.active.viewportY === from);
      gliding = ended ? 0 : requestAnimationFrame(step);
    }
    gliding = requestAnimationFrame(step);
  }
  function touches() {
    var t0 = null, selecting = null, press = 0, scrolled = 0, moved = false, dragging = false, trail = [], stopped = false;
    // touchstart keeps its default: it is what gives the WebView the system's focus, without which focusing the
    // textarea brings no keyboard up (Android, measured). touch-action:none already stops panning and zooming.
    // touchend never does: its default is the mouse events a tap turns into.
    box.addEventListener('touchstart', function (e) {
      post({ t: 'touched' });
      clearTimeout(press);
      stopped = stopGlide();
      if (e.touches.length !== 1) { t0 = null; selecting = null; return; }
      var p = e.touches[0];
      t0 = { x: p.clientX, y: p.clientY, at: Date.now() }; moved = false; scrolled = 0; selecting = null; dragging = false; trail = [];
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
        // Travel short of a step is kept for the next move, so a finger that goes back cancels it.
        var u = cell(p.clientX, p.clientY).h * rowsPerStep(), steps = Math.trunc((dy - scrolled) / u);
        if (steps) { scrollView(steps, t0.x, t0.y); scrolled += steps * u; }
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
        if (term.modes.mouseTrackingMode !== 'none') click(start.x, start.y);
        post({ t: 'tap' });
      }
    }, { passive: false });
  }
  // A phone's keyboard edits xterm's hidden field and says little about which key it was (keydown 229). xterm guesses
  // the bytes from the field and guesses wrong with Gboard (API 35 emulator, 2026-11-07): Enter sent "\\r" and then a
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
    // spends the next backspace on it, sending nothing (logcat trace, API 35 emulator, 2026-11-07).
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
    term.onWriteParsed(function () { follow(); where(); settled(); lift(); });
    term.onRender(edges);
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
      if (m.t === 'reset') { stopGlide(); term.reset(); kitty = []; span = null; linked = []; linking = null; }
      else if (m.t === 'write') term.write(m.data);
      else if (m.t === 'paste') { forget(); term.paste(m.data); }
      else if (m.t === 'theme') { term.options.theme = cfg.themes[m.dark ? 'dark' : 'light']; edges(); }
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
  const cfg = JSON.stringify({ phone: !!opts.phone, raised: opts.raised ?? 0, dark: opts.dark, fontSize: opts.fontSize, scrollback: opts.scrollback, themes, fonts: TERMINAL_FONTS, ...LOOK })
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
  if (m?.t === 'tap') return { t: 'tap' }
  if (m?.t === 'focused') return { t: 'focused' }
  if (m?.t === 'touched') return { t: 'touched' }
  if (m?.t === 'away' && typeof m.away === 'boolean') return { t: 'away', away: m.away }
  const strings = (v: any, max: number) => Array.isArray(v) && v.length <= max && v.every((l: any) => typeof l === 'string')
  if (m?.t === 'tail' && strings(m.lines, 100)) return { t: 'tail', lines: m.lines }
  if (m?.t === 'text' && strings(m.lines, 20000) && Number.isInteger(m.base) && m.base >= 0) return { t: 'text', base: m.base, lines: m.lines }
  return undefined
}

/** The app's side of the page: output waits until the page is ready, decoded as one UTF-8 stream, and writes
 * between two flushes go as one message. */
export class PageChannel {
  private open = false
  private queue: ({ t: 'write'; data: string } | { t: 'reset' })[] = []
  private timer: number | undefined
  private dec = new TextDecoder()
  constructor(private post: (message: string) => void, private later: (f: () => void, ms: number) => number = setTimeout) {}
  ready() { this.open = true; this.schedule() }
  /** The page went away (reloaded or torn down): nothing more is sent until it says ready again. */
  lost() { this.open = false; this.queue = []; this.timer = undefined }
  write(bytes: Uint8Array) {
    const data = this.dec.decode(bytes, { stream: true })
    if (!data) return
    const tail = this.queue.at(-1)
    if (tail?.t === 'write') tail.data += data
    else this.queue.push({ t: 'write', data })
    this.schedule()
  }
  /** A new attachment repaints from scratch. */
  reset() { this.dec = new TextDecoder(); this.queue = [{ t: 'reset' }]; this.schedule() }
  send(message: { t: 'key'; name: BarKey; ctrl?: boolean; shift?: boolean } | { t: 'paste'; data: string } | { t: 'theme'; dark: boolean } | { t: 'fontSize'; size: number } | { t: 'focus' } | { t: 'copy' }
    | { t: 'bottom' } | { t: 'refit' } | { t: 'text' } | { t: 'mark'; line: number; label: string }) {
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
