// Typing on a phone's keyboard into the terminal page, in a real browser: Chromium over CDP sends what Chrome on
// Android makes of Gboard's typing (a keydown with keyCode 229, then the text or the change to the field), and the
// page must send the program exactly what was typed. Run with `e2e/page-ime.sh`.
// The sequences follow what Gboard did on the API 35 emulator (2026-11-07, e2e/android-typing.sh checks the same on
// the device): before the fix, Enter sent "\r" and then a stray backspace, and a correction picked from the
// suggestions sent the whole field again.
import { createRequire } from 'node:module'
import { terminalPage } from '../src/ui/terminal-page.ts'

const require = createRequire(process.env.PLAYWRIGHT_DIR + '/')
const { chromium } = require('./index.js')

const browser = await chromium.launch()
const failures = []
const check = (name, ok, seen) => { console.log(`${ok ? 'ok' : 'FAIL'}: ${name}${seen === undefined ? '' : ` (${JSON.stringify(seen).replace(/\x7f/g, "\\x7f")})`}`); if (!ok) failures.push(name) }

// What a line editor shows after these bytes: a backspace takes the last character back.
const line = (s) => { const out = []; for (const c of s) { if (c === '\x7f') out.pop(); else out.push(c) } return out.join('') }

// Runs in the page: what Chrome on Android does with an IME's edits to the focused field.
const IME = () => {
  const ta = () => document.querySelector('.xterm-helper-textarea')
  const fire = (type, Kind, init, props) => { const e = new Kind(type, { bubbles: true, cancelable: true, composed: true, ...init }); for (const k in props) Object.defineProperty(e, k, { get: () => props[k] }); return ta().dispatchEvent(e) }
  const down = (keyCode, key) => fire('keydown', KeyboardEvent, { key }, { keyCode, which: keyCode })
  const up = (keyCode, key) => fire('keyup', KeyboardEvent, { key }, { keyCode, which: keyCode })
  // The field's text from `from` to the caret becomes `text` (an insert when from is the caret).
  const put = (text, from, inputType, composing) => {
    const f = ta(), end = f.selectionEnd, start = from ?? end
    if (!fire('beforeinput', InputEvent, { inputType, data: text, isComposing: composing })) return
    f.value = f.value.slice(0, start) + text + f.value.slice(end)
    f.selectionStart = f.selectionEnd = start + text.length
    fire('input', InputEvent, { inputType, data: text, isComposing: composing })
  }
  let composing = null // where the composition starts
  return {
    down, up,
    commit(text) {
      down(229, 'Unidentified')
      if (composing !== null) { put(text, composing, 'insertCompositionText', true); fire('compositionend', CompositionEvent, { data: text }); composing = null }
      else put(text, null, 'insertText', false)
      up(229, 'Unidentified')
    },
    // A composition of `text`; `from` marks text already in the field as the part being replaced (a correction).
    compose(text, from) {
      down(229, 'Unidentified')
      if (composing === null) { composing = from ?? ta().selectionEnd; fire('compositionstart', CompositionEvent, { data: '' }) }
      fire('compositionupdate', CompositionEvent, { data: text })
      put(text, composing, 'insertCompositionText', true)
      up(229, 'Unidentified')
    },
    // The keyboard rewrites text before the caret without composing (iOS swaps the no-break space it typed for a space).
    swap(from, text) { down(229, 'Unidentified'); put(text, from, 'insertReplacementText', false); up(229, 'Unidentified') },
    erase() {
      down(229, 'Unidentified')
      const f = ta(), end = f.selectionEnd
      if (end > 0) {
        const chars = [...f.value.slice(0, end)], last = chars.pop()
        if (fire('beforeinput', InputEvent, { inputType: 'deleteContentBackward' })) {
          f.value = chars.join('') + f.value.slice(end); f.selectionStart = f.selectionEnd = end - last.length
          fire('input', InputEvent, { inputType: 'deleteContentBackward' })
        }
      } else { down(8, 'Backspace'); up(8, 'Backspace') }
      up(229, 'Unidentified')
    },
  }
}

async function open() {
  const context = await browser.newContext({ viewport: { width: 390, height: 700 }, hasTouch: true, isMobile: true, deviceScaleFactor: 3 })
  const page = await context.newPage()
  await page.addInitScript(() => { window.posts = []; window.nsh = { postMessage: (m) => window.posts.push(JSON.parse(m)) }; window.addEventListener('error', (e) => { window.pageError = e.message })})
  await page.addInitScript(`window.__ime = (${IME.toString()})()`)
  await page.route('http://terminal.test/', (r) => r.fulfill({ contentType: 'text/html', body: terminalPage({ dark: true, fontSize: 13, scrollback: 2000, phone: true }) }))
  await page.goto('http://terminal.test/')
  await page.waitForFunction(() => window.posts.some((m) => m.t === 'ready'))
  await page.evaluate(() => window.postMessage(JSON.stringify({ t: 'focus' }), '*'))
  await page.waitForTimeout(100)
  const cdp = await context.newCDPSession(page)
  let seen = 0
  const t = {
    page,
    // What the program got since the last call.
    input: async () => { await page.waitForTimeout(120); const all = await page.evaluate(() => window.posts.filter((m) => m.t === 'input').map((m) => m.data)); const s = all.slice(seen).join(''); seen = all.length; return s },
    field: () => page.evaluate(() => document.querySelector('.xterm-helper-textarea').value),
    // Chrome on Android, in one task as it does: keydown 229 ("Unidentified"), the change to the field with its
    // composition and input events, keyup. Synthetic events, which xterm reads as it reads real ones.
    commit: (text) => page.evaluate((text) => window.__ime.commit(text), text),
    letters: async (s) => { for (const c of s) await t.commit(c) },
    swap: (from, text) => page.evaluate(([from, text]) => window.__ime.swap(from, text), [from, text]),
    compose: (text, from) => page.evaluate(([text, from]) => window.__ime.compose(text, from), [text, from ?? null]),
    // Gboard's Enter: the IME's keydown 229 and, in the same moment, the Enter key itself.
    enter: () => page.evaluate(() => { window.__ime.down(229, 'Unidentified'); window.__ime.down(13, 'Enter'); window.__ime.up(13, 'Enter') }),
    // Gboard's backspace with text in the field: keydown 229 and the field loses its last character.
    backspace: () => page.evaluate(() => window.__ime.erase()),
    key: async (key, code, keyCode, extra = {}) => {
      await cdp.send('Input.dispatchKeyEvent', { type: 'rawKeyDown', key, code, windowsVirtualKeyCode: keyCode, nativeVirtualKeyCode: keyCode, ...extra })
      if (extra.text) await cdp.send('Input.dispatchKeyEvent', { type: 'char', key, code, text: extra.text, windowsVirtualKeyCode: keyCode })
      await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key, code, windowsVirtualKeyCode: keyCode, nativeVirtualKeyCode: keyCode, modifiers: extra.modifiers || 0 })
    },
    close: () => context.close(),
  }
  await t.input()
  return t
}

{
  const t = await open()
  await t.letters('ls')
  check('letters arrive as typed', (await t.input()) === 'ls')
  for (let i = 0; i < 5; i++) {
    await t.letters('ab')
    await t.enter()
    const got = await t.input()
    check(`Enter ${i + 1}: "\\r" and no stray backspace`, got === 'ab\r', got)
  }
  await t.close()
}
{
  // A correction picked from the suggestions: Gboard marks the word it replaces and commits the new one.
  const t = await open()
  await t.letters('hello teh')
  await t.input()
  const f = await t.field()
  await t.compose('teh', f.length - 3)
  await t.commit('the ')
  const got = await t.input()
  check('a picked correction leaves "hello the " on the line', line('hello teh' + got) === 'hello the ', got)
  // A completion: "hel" becomes "hello ".
  await t.enter(); await t.input()
  await t.letters('hel')
  await t.input()
  const g = await t.field()
  await t.compose('hel', g.length - 3)
  await t.commit('hello ')
  const done = await t.input()
  check('a picked completion adds the rest of the word', line('hel' + done) === 'hello ', done)
  await t.close()
}
{
  // Pinyin: the letters compose, a pick commits the characters.
  const t = await open()
  for (const s of ['n', 'ni', 'nih', 'niha', 'nihao']) await t.compose(s)
  check('nothing goes while composing', (await t.input()) === '')
  await t.commit('你好')
  check('the picked characters go once', (await t.input()) === '你好')
  // Gboard's pinyin keeps a picked word as its own after the commit and spends the next backspace on it: nothing
  // reached the page on the first backspace after 你好 or 世界 (logcat trace, API 35 emulator, 2026-11-07). The page
  // empties the field once a word in such a script is picked, so the keyboard has nothing of it left to hold.
  await t.page.waitForTimeout(50)
  const left = await t.page.evaluate(() => document.querySelector('textarea').value)
  check('the field is empty after a picked Chinese word', left === '', left)
  await t.backspace()
  const b = await t.input()
  check('the first backspace after a pick goes', b === '\x7f', b)
  await t.backspace()
  check('and the next', (await t.input()) === '\x7f')
  await t.backspace()
  check('a backspace with nothing typed still goes', (await t.input()) === '\x7f')
  await t.close()
}
{
  // iOS's space: the field gets a no-break space (U+00A0), swapped for a plain one when the next word starts (seen in
  // the iPhone SE simulator, iOS 17, 2026-11-07: the program read "hi\xc2\xa0", then "\x7f a").
  const t = await open()
  await t.letters('teh')
  await t.commit('\u00a0')
  const sp = await t.input()
  check('a space typed on iOS arrives as a space', sp === 'teh ', sp)
  await t.swap(3, ' ')
  await t.commit('a')
  const a = await t.input()
  check('and the swap that follows sends nothing more than the next letter', a === 'a', a)
  await t.close()
}
{
  // A keyboard with real keys (Bluetooth, an emulator's): the page sends what xterm sends.
  const t = await open()
  await t.key('a', 'KeyA', 65, { text: 'a' })
  check('a key with a letter: the letter, once', (await t.input()) === 'a')
  await t.key('ArrowUp', 'ArrowUp', 38)
  check('an arrow: its escape sequence', (await t.input()) === '\x1b[A')
  await t.key('c', 'KeyC', 67, { modifiers: 2 })
  check('ctrl+c: ^C', (await t.input()) === '\x03')
  await t.key('Tab', 'Tab', 9)
  check('tab', (await t.input()) === '\t')
  await t.key('Backspace', 'Backspace', 8)
  check('backspace', (await t.input()) === '\x7f')
  await t.key('Enter', 'Enter', 13, { text: '\r' })
  check('enter', (await t.input()) === '\r')
  check('no error', !(await t.page.evaluate(() => window.pageError)))
  await t.close()
}

await browser.close()
if (failures.length) { console.log(`FAIL: ${failures.length} check(s)`); process.exit(1) }
console.log('PASS')
