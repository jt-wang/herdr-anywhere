import { expect, test } from 'nsh/test'
import { pickedWord, rowKeys } from './pane-bar.tsx'

const has = (keys: { key: string }[], k: string) => keys.some((x) => x.key === k)

test('the key row has Esc only when nothing else on screen does', () => {
  expect(has(rowKeys('compose'), 'esc')).toBe(false)
  expect(has(rowKeys('keys'), 'esc')).toBe(false)
  expect(has(rowKeys('choices'), 'esc')).toBe(true)
})

test('the keys a phone keyboard lacks stay', () => {
  for (const k of ['shift', 'tab', 'ctrl', 'ctrlc', 'left', 'up', 'down', 'right', 'enter']) expect(has(rowKeys('compose'), k)).toBe(true)
})

test('Shift is a key held for the next one, as Ctrl is, in place of a Shift+Tab key', () => {
  expect(has(rowKeys('compose'), 'btab')).toBe(false)
  expect(rowKeys('compose').map((k) => k.key).slice(0, 3)).toEqual(['shift', 'tab', 'ctrl'])
})

test('a word picked in Chinese, Japanese or Korean is told apart from typing and deleting', () => {
  expect(pickedWord('ni hao', '你好')).toBe(true) // the pinyin's letters become the word
  expect(pickedWord('', '你好')).toBe(true)
  expect(pickedWord('你好', '你好世界')).toBe(true)
  expect(pickedWord('你好shi jie', '你好世界')).toBe(true)
  expect(pickedWord('ka', 'か')).toBe(true)
  expect(pickedWord('', '안')).toBe(true)
  expect(pickedWord('你好', '你')).toBe(false) // a backspace
  expect(pickedWord('你', '')).toBe(false)
  expect(pickedWord('ni', 'ni h')).toBe(false) // pinyin still composing
  expect(pickedWord('hello', 'hello ')).toBe(false)
  expect(pickedWord('你好', '你好 ')).toBe(false)
  expect(pickedWord('你好', '你好')).toBe(false)
})
