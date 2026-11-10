// The dialog's words: every language has every one of them, with the same blanks.
import test from 'node:test'
import assert from 'node:assert/strict'
import { LANGS, STRINGS, fill, wordsFor } from './strings.js'

const blanks = (text) => (text.match(/\{\w+\}/g) ?? []).sort().join(' ')

test('the seven languages of the phone app, and nothing else', () => {
  assert.deepEqual(LANGS, ['en', 'zh-Hans', 'zh-Hant', 'es', 'ko', 'ja', 'ru'])
  assert.deepEqual(Object.keys(STRINGS).sort(), [...LANGS].sort())
})

test('each language has every word English has, none empty, with the same blanks', () => {
  const keys = Object.keys(STRINGS.en).sort()
  for (const lang of LANGS) {
    assert.deepEqual(Object.keys(STRINGS[lang]).sort(), keys, lang)
    for (const k of keys) {
      const v = STRINGS[lang][k]
      if (k === 'units') {
        assert.deepEqual(Object.keys(v).sort(), ['d', 'h', 'm'], lang)
        continue
      }
      assert.ok(typeof v === 'string' && v.trim(), `${lang}.${k}`)
      assert.equal(blanks(v), blanks(STRINGS.en[k]), `${lang}.${k}`)
      if (lang !== 'en' && !/^[A-Z][a-z]+$/.test(STRINGS.en[k]) && k !== 'title') assert.notEqual(v, STRINGS.en[k], `${lang}.${k} is still English`)
    }
  }
})

test('an unknown or missing language reads English', () => {
  assert.equal(wordsFor('ja'), STRINGS.ja)
  assert.equal(wordsFor('fr'), STRINGS.en)
  assert.equal(wordsFor(undefined), STRINGS.en)
})

test('blanks are filled, and a blank without a value stays as it is', () => {
  assert.equal(fill('{name} is paired.', { name: 'Pixel' }), 'Pixel is paired.')
  assert.equal(fill('{name} is paired.'), '{name} is paired.')
})
