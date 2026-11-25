import { expect, test } from 'nsh/test'
import { pictureRequest } from './picture-request.ts'

test('picture deadline rejects even when transport ignores abort', async () => {
  let aborted = false
  const result = pictureRequest(signal => { signal.addEventListener('abort', () => { aborted = true }); return new Promise(() => {}) }, undefined, 10)
  expect(await result.then(() => 'ok', () => 'failed')).toBe('failed')
  expect(aborted).toBe(true)
})
test('closing cancels picture body work and a late response cannot resolve it', async () => {
  const controller = new AbortController()
  let finish!: (x: string) => void
  const pending = pictureRequest(() => new Promise<string>(r => { finish = r }), controller.signal)
  await Promise.resolve()
  controller.abort(); finish('late')
  expect(await pending.then(() => 'ok', () => 'cancelled')).toBe('cancelled')
})
test('a successful picture request returns unchanged', async () => {
  expect(await pictureRequest(async () => 'picture')).toBe('picture')
})
