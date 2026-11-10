// The dialog itself, under node, with Herdr's modules faked: `node --test`.
import test from 'node:test'
import assert from 'node:assert/strict'
import { calls, closed, context, find, replies, texts } from './fake-herdr.mjs'

const { default: AddPhone } = await import('./main.js')
const { LANGS, STRINGS } = await import('./strings.js')

const CODE = (seconds = 120) => ({
  code: 0, stderr: '',
  stdout: JSON.stringify({ invite: 'herdr1:X', expires: new Date(Date.now() + seconds * 1000).toISOString(), qr: ['101', '010', '101'] }),
})
const NOT_RUNNING = { code: 1, stdout: '{"error":"not-running"}', stderr: '' }
const DEVICES = (...names) => ({ code: 0, stderr: '', stdout: JSON.stringify(names.map((name) => ({ id: name.toLowerCase(), name }))) })

const LANGUAGE = (lang) => ({ code: 0, stdout: `${lang}\n`, stderr: '' })

async function open(pair, devices = DEVICES(), language = LANGUAGE('en')) {
  calls.length = 0
  closed.length = 0
  Object.assign(replies, { pair, devices, language, start: undefined, revoke: undefined })
  const view = new AddPhone()
  const cx = context()
  view.init({}, cx)
  await cx.settle()
  return { view, cx, shown: () => view.render(), words: () => texts(view.render()).join(' | ') }
}
const click = async (d, id) => { find(d.shown(), id).onClick({}, d.cx); await d.cx.settle() }
const fire = async (d, ms) => { for (const [every, f] of d.cx.timers) if (every === ms) f(d.cx); await d.cx.settle() }
const press = async (d, key) => { d.shown().onKey({ key }, d.cx); await d.cx.settle() }

test('with the companion running it shows a code, drawn in black on white', async () => {
  const d = await open(CODE())
  assert.match(d.words(), /scan this code/)
  assert.match(d.words(), /Expires in 2:00|Expires in 1:59/)
  assert.ok(calls.includes('herdr-anywhere pair --state /state --json'), calls.join('; '))
})

test('the list is read before the code, so the phone that scans it counts as new', async () => {
  await open(CODE())
  assert.ok(calls.indexOf('herdr-anywhere devices --state /state --json') < calls.indexOf('herdr-anywhere pair --state /state --json'))
})

test('Start runs the companion and then shows a code', async () => {
  const d = await open(NOT_RUNNING)
  assert.match(d.words(), /isn't running/)
  replies.pair = CODE()
  await click(d, 'ap-start')
  assert.ok(calls.includes('herdr-anywhere start --state /state'))
  assert.match(d.words(), /scan this code/)
})

test('Enter does what the button does', async () => {
  const d = await open(NOT_RUNNING)
  replies.pair = CODE()
  await press(d, 'enter')
  assert.ok(calls.includes('herdr-anywhere start --state /state'))
})

test('Start says why when the companion will not run, and can try again', async () => {
  const d = await open(NOT_RUNNING)
  replies.start = { code: 1, stdout: '', stderr: 'herdr-anywhere start: no herdr-core\n' }
  await click(d, 'ap-start')
  assert.match(d.words(), /didn't start\. \| herdr-anywhere start: no herdr-core/)
  replies.start = undefined
  replies.pair = CODE()
  await click(d, 'ap-start')
  assert.match(d.words(), /scan this code/)
})

test('a program that cannot run says what to do', async () => {
  const d = await open(new Error('no such file'), new Error('no such file'))
  assert.match(d.words(), /install-dialog again/)
})

test('an expired code goes away and asks for a new one', async () => {
  const d = await open(CODE(-1))
  await fire(d, 1000)
  assert.match(d.words(), /expired/)
  assert.doesNotMatch(d.words(), /scan this code/)
  replies.pair = CODE()
  await click(d, 'ap-new')
  assert.match(d.words(), /scan this code/)
})

test('the phone that pairs is named, and the code goes away', async () => {
  const d = await open(CODE())
  replies.devices = DEVICES('Pixel')
  await fire(d, 2000)
  assert.match(d.words(), /Pixel is paired/)
  assert.doesNotMatch(d.words(), /scan this code/)
  assert.match(d.words(), /Paired phones/)
})

test('a phone already paired is not news', async () => {
  const d = await open(CODE(), DEVICES('Pixel'))
  await fire(d, 2000)
  assert.match(d.words(), /scan this code/)
})

test('polling draws nothing when the list has not changed', async () => {
  const d = await open(CODE())
  const before = d.cx.renders
  await fire(d, 2000)
  assert.equal(d.cx.renders, before)
})

test('Remove asks first, then revokes that phone', async () => {
  const d = await open(CODE(), DEVICES('Pixel', 'iPhone'))
  await click(d, 'ap-remove-pixel')
  assert.match(d.words(), /Remove it\?/)
  assert.ok(!calls.some((c) => c.includes('revoke')))
  replies.devices = DEVICES('iPhone')
  await click(d, 'ap-remove-pixel')
  assert.ok(calls.includes('herdr-anywhere revoke --state /state pixel'))
  assert.doesNotMatch(d.words(), /Pixel/)
})

test('a failed removal is shown', async () => {
  const d = await open(CODE(), DEVICES('Pixel'))
  replies.revoke = { code: 1, stdout: '', stderr: 'herdr-anywhere revoke: nope\n' }
  await click(d, 'ap-remove-pixel')
  await click(d, 'ap-remove-pixel')
  assert.match(d.words(), /revoke: nope/)
})

test('Escape closes the dialog', async () => {
  const d = await open(CODE())
  await press(d, 'escape')
  assert.deepEqual(closed, ['the-overlay'])
})

// --- the computer's language ----------------------------------------------

const never = new Promise(() => {})
const SEEN = { code: 0, stderr: '', stdout: JSON.stringify([
  { id: 'pixel', name: 'Pixel' },
  { id: 'iphone', name: 'iPhone', last_seen: new Date(Date.now() - 5 * 60000).toISOString() },
]) }

// Every state the dialog can be in, reached the way a person reaches it.
async function everyState(lang) {
  const shown = {}
  const L = LANGUAGE(lang)
  let d = await open(CODE(), SEEN, L)
  shown.code = d.words()
  await click(d, 'ap-remove-pixel')
  shown.confirm = d.words()
  replies.revoke = { code: 1, stdout: '', stderr: 'herdr-anywhere revoke: nope\n' }
  await click(d, 'ap-remove-pixel')
  shown.removeFailed = d.words()
  d = await open(never, DEVICES(), L)
  shown.loading = d.words()
  d = await open(NOT_RUNNING, DEVICES(), L)
  shown.notRunning = d.words()
  replies.start = never
  await click(d, 'ap-start')
  shown.starting = d.words()
  d = await open(NOT_RUNNING, DEVICES(), L)
  replies.start = { code: 1, stdout: '', stderr: 'herdr-anywhere start: no herdr-core\n' }
  await click(d, 'ap-start')
  shown.startFailed = d.words()
  d = await open(CODE(-1), DEVICES(), L)
  await fire(d, 1000)
  shown.expired = d.words()
  d = await open(CODE(), DEVICES(), L)
  replies.devices = DEVICES('Pixel')
  await fire(d, 2000)
  shown.paired = d.words()
  d = await open({ code: 1, stdout: '{"error":"relay down"}', stderr: '' }, DEVICES(), L)
  shown.pairFailed = d.words()
  d = await open({ code: 0, stdout: 'not json', stderr: '' }, DEVICES(), L)
  shown.unreadable = d.words()
  d = await open(new Error('no such file'), new Error('no such file'), L)
  shown.cantRun = d.words()
  return shown
}

// Pieces of English four letters or longer, from each English sentence with its blanks taken out.
const english = Object.entries(STRINGS.en).filter(([k]) => k !== 'units')
  .flatMap(([, v]) => v.split(/\{\w+\}/)).map((p) => p.trim()).filter((p) => /[a-z]{4}/i.test(p))

test('in Japanese no state shows an English sentence of the dialog', async () => {
  const shown = await everyState('ja')
  assert.equal(Object.keys(shown).length, 12)
  for (const [state, words] of Object.entries(shown)) {
    for (const piece of english) assert.ok(!words.includes(piece), `${state} shows "${piece}": ${words}`)
  }
  assert.match(shown.code, /端末を追加/)
  assert.match(shown.code, /5分前に接続/)
  assert.match(shown.startFailed, /herdr-anywhere start: no herdr-core/) // what the CLI said, as it said it
})

test('every language draws its own words in every state', async () => {
  for (const lang of LANGS) {
    const shown = await everyState(lang)
    const s = STRINGS[lang]
    assert.match(shown.code, new RegExp(s.title), lang)
    assert.ok(shown.code.includes(s.scan), lang)
    assert.ok(shown.notRunning.includes(s.notRunning), lang)
    assert.ok(shown.paired.includes(s.paired.replace('{name}', 'Pixel')), lang)
  }
})

test('the language is asked once, first, from the companion', async () => {
  await open(CODE(), DEVICES(), LANGUAGE('ko'))
  assert.equal(calls[0], 'herdr-anywhere language')
  assert.equal(calls.filter((c) => c.includes('language')).length, 1)
})

test('nothing is drawn in English while the language is still coming', async () => {
  calls.length = 0
  Object.assign(replies, { pair: CODE(), devices: DEVICES(), language: never })
  const view = new AddPhone()
  view.init({}, context())
  assert.deepEqual(texts(view.render()), [])
})

test('a companion that has no language command, or fails, gets English', async () => {
  let d = await open(CODE(), DEVICES(), { code: 2, stdout: '', stderr: 'unknown command "language"' })
  assert.match(d.words(), /Add phone/)
  d = await open(CODE(), DEVICES(), new Error('no such file'))
  assert.match(d.words(), /Add phone/)
  d = await open(CODE(), DEVICES(), LANGUAGE('fr'))
  assert.match(d.words(), /scan this code/)
})
