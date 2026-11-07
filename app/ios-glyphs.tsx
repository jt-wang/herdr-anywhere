// The app e2e/ios-glyphs.sh installs: the real terminal page in a real WKWebView, printing what agents print. A probe
// draws each character with the terminal's own fonts, as xterm's renderer does, and compares it with a code point that
// has no character at all: the same picture means the phone could not draw it. iOS 26.3 draws such a code point as a
// box; iOS 17.0 and 26.5 draw nothing (2026-11-07), so there a character that is blank by nature (a space, U+2800)
// cannot be judged and is left out. (nsh.json must sit next to the entry, so it is here.)
import { mount, signal, Text, View, WebView } from 'nsh'
import { TERMINAL_FONTS, readPageMessage, terminalPage } from './src/ui/terminal-page.ts'
import { AGENT_GLYPHS, EMOJI, MORE_GLYPHS, SCANNED_BLOCKS } from './e2e/agent-glyphs.ts'

const hex = (c: string) => 'U+' + c.codePointAt(0)!.toString(16).toUpperCase().padStart(4, '0')
const scanned: string[] = []
for (const [from, to] of SCANNED_BLOCKS) for (let c = from; c <= to; c++) scanned.push(String.fromCodePoint(c))

const probe = `<script>(function () {
  var cfg = ${JSON.stringify({ fonts: TERMINAL_FONTS, need: [...AGENT_GLYPHS, ...MORE_GLYPHS], emoji: EMOJI, scan: scanned })};
  var canvas = document.createElement('canvas'); canvas.width = canvas.height = 96;
  var ctx = canvas.getContext('2d', { willReadFrequently: true });
  function picture(c) {
    ctx.clearRect(0, 0, 96, 96); ctx.font = '48px ' + cfg.fonts; ctx.textBaseline = 'middle'; ctx.fillStyle = '#000';
    ctx.fillText(c, 16, 48);
    return Array.prototype.join.call(ctx.getImageData(0, 0, 96, 96).data);
  }
  function run() {
    var none = picture('\\u0378');
    // A missing character is a box on some systems and nothing on others (then it looks like a space).
    var boxed = picture(' ') !== none;
    // Which of the three checks held, so a failed calibration says why: another unassigned point looks the same, a
    // letter and a line do not.
    var cal = [picture('\\u0379') === none, picture('A') !== none, picture('\\u2500') !== none];
    var instrument = cal.every(Boolean);
    var empty = /^[\\s\\u2800]$/;
    var lost = function (c) { return picture(c) === none && (boxed || !empty.test(c)); };
    // Colour, not a monochrome box: some pixel whose red, green and blue differ.
    function colour(c, fonts) {
      ctx.clearRect(0, 0, 96, 96); ctx.font = '48px ' + fonts; ctx.textBaseline = 'middle'; ctx.fillStyle = '#000'; ctx.fillText(c, 16, 48);
      var d = ctx.getImageData(0, 0, 96, 96).data;
      for (var i = 0; i < d.length; i += 4) if (d[i + 3] > 200 && (Math.abs(d[i] - d[i + 1]) > 16 || Math.abs(d[i + 1] - d[i + 2]) > 16)) return true;
      return false;
    }
    var grey = function (fonts) { return cfg.emoji.filter(function (c) { return !colour(c, fonts); }); };
    window.nsh.postMessage(JSON.stringify({ t: 'glyphs', instrument: instrument, missing: boxed ? 'box' : 'blank', cal: cal.map(Number).join(''), need: cfg.need.filter(lost), scan: cfg.scan.filter(lost), page: grey(cfg.fonts), system: grey('sans-serif') }));
  }
  // The terminal's page posts ready once its fonts are loaded; the probe follows it and loads nothing itself.
  var post = window.nsh.postMessage.bind(window.nsh), done = false;
  window.nsh.postMessage = function (m) { post(m); if (!done && m.indexOf('"ready"') > 0) { done = true; setTimeout(run, 300); } };
})();</script>`
const html = terminalPage({ dark: true, fontSize: 15, scrollback: 100 }).replace('<script>', probe + '<script>')

const result = signal('probing')
let web: any
const show = AGENT_GLYPHS.join(' ')
mount(() => (
  <View class="flex-1 bg-background" style={{ paddingTop: 60 }}>
    <Text class="text-[20px] text-foreground">{`native ${EMOJI.join(' ')}`}</Text>
    <Text testID="glyphs" accessibilityLabel={result()} class="text-[11px] text-foreground">{result()}</Text>
    <WebView ref={(r: any) => { web = r }} html={html} class="flex-1" onMessage={(e: any) => {
      const raw = String(e.data)
      if (raw.includes('"glyphs"')) {
        const r = JSON.parse(raw)
        result.set(`GLYPHS instrument=${r.instrument} need=${r.need.map(hex).join(',') || 'none'} emojiPage=${r.page.map(hex).join(',') || 'none'} emojiSystem=${r.system.map(hex).join(',') || 'none'} scan=${r.scan.map(hex).join(',') || 'none'} missing=${r.missing} cal=${r.cal}`)
        return
      }
      if (readPageMessage(raw)?.t === 'ready') web?.postMessage(JSON.stringify({ t: 'write', data: `  ⎿  Read 12 lines\r\n${show}\r\n${MORE_GLYPHS.join(' ')}\r\n${EMOJI.join(' ')} A\r\n` }))
    }} />
  </View>
))
