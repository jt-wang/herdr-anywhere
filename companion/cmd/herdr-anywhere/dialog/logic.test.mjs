// Run with: node --test
import test from 'node:test'
import assert from 'node:assert/strict'
import { addedSince, ago, parseDevices, parsePair, remaining, runs } from './logic.js'
import { STRINGS } from './strings.js'

const ok = (stdout) => ({ code: 0, stdout, stderr: '' })
const failed = (stdout, stderr = '') => ({ code: 1, stdout, stderr })

test('a pair reply becomes a code to show', () => {
  const r = parsePair(ok(JSON.stringify({ invite: 'herdr1:X', expires: '2026-11-03T10:00:00Z', qr: ['10', '01'] })))
  assert.deepEqual(r, { kind: 'code', expires: Date.parse('2026-11-03T10:00:00Z'), rows: ['10', '01'] })
})

test('no companion is its own state, so the dialog can offer Start', () => {
  assert.deepEqual(parsePair(failed('{"error":"not-running"}')), { kind: 'not-running' })
})

test('any other failure shows what went wrong', () => {
  assert.deepEqual(parsePair(failed('{"error":"relay down"}')), { kind: 'error', message: 'relay down' })
  assert.deepEqual(parsePair(failed('', 'herdr-anywhere pair: boom\n')), { kind: 'error', message: 'herdr-anywhere pair: boom' })
  assert.deepEqual(parsePair(ok('not json')), { kind: 'error', message: '' })
  assert.deepEqual(parsePair(ok('{"invite":"herdr1:X"}')), { kind: 'error', message: '' })
})

test('devices are read, and a failure is not an empty list', () => {
  assert.deepEqual(parseDevices(ok('[{"id":"a","name":"Pixel"}]')), [{ id: 'a', name: 'Pixel' }])
  assert.deepEqual(parseDevices(ok('[]')), [])
  assert.equal(parseDevices(failed('')), null)
  assert.equal(parseDevices(ok('nope')), null)
})

test('a row is drawn as runs of one colour, in order, covering every module', () => {
  assert.deepEqual(runs('1100010'), [[true, 2], [false, 3], [true, 1], [false, 1]])
  assert.deepEqual(runs('0'), [[false, 1]])
  assert.deepEqual(runs(''), [])
})

test('the new phone is the one that was not there before', () => {
  const a = { id: 'a', name: 'Pixel' }, b = { id: 'b', name: 'iPhone' }
  assert.deepEqual(addedSince([a], [a, b]), [b])
  assert.deepEqual(addedSince([a, b], [a]), [])
  assert.deepEqual(addedSince(null, [a]), [])
})

test('time left reads as minutes and seconds and stops at zero', () => {
  assert.equal(remaining(1000 + 103_000, 1000), '1:43')
  assert.equal(remaining(1000 + 9_000, 1000), '0:09')
  assert.equal(remaining(1000, 5000), '0:00')
})

test('last seen reads the way the phone app writes a span, in the dialog\'s language', () => {
  const now = Date.parse('2026-11-03T10:00:00Z')
  const en = STRINGS.en, ja = STRINGS.ja, ru = STRINGS.ru
  assert.equal(ago(undefined, now, en), 'Never connected')
  assert.equal(ago('2026-11-03T09:59:40Z', now, en), 'Last seen just now')
  assert.equal(ago('2026-11-03T09:55:00Z', now, en), 'Last seen 5m ago')
  assert.equal(ago('2026-11-03T07:00:00Z', now, en), 'Last seen 3h ago')
  assert.equal(ago('2026-11-01T10:00:00Z', now, en), 'Last seen 2d ago')
  assert.equal(ago('2026-11-03T09:55:00Z', now, ja), '5分前に接続')
  assert.equal(ago('2026-11-03T07:00:00Z', now, STRINGS['zh-Hans']), '3小时前连接过')
  assert.equal(ago('2026-11-01T10:00:00Z', now, ru), 'Последнее подключение 2 д назад')
  assert.equal(ago(undefined, now, ja), ja.neverConnected)
})
