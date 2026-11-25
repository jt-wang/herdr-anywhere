import { expect, test } from 'nsh/test'
import { pictureEvents } from './picture-events.ts'

test('late decode success and failure from A cannot complete or clear B', () => {
  const a = { url: 'a' }, b = { url: 'b' }, first = new AbortController(), second = new AbortController()
  let current = { image: a, owner: first }, loaded = 0, failed = 0
  const callbacks = pictureEvents(a, first, () => current, () => loaded++, () => failed++)
  callbacks.onLoad({ width: 2, height: 2 }); expect(loaded).toBe(1)
  current = { image: b, owner: second }
  callbacks.onLoad({ width: 2, height: 2 }); callbacks.onError()
  expect(loaded).toBe(1); expect(failed).toBe(0)
  // A retry of the same URL/object still has a new request owner.
  current = { image: a, owner: second }
  callbacks.onLoad({ width: 2, height: 2 }); callbacks.onError()
  expect(loaded).toBe(1); expect(failed).toBe(0)
})

test('closing an image prevents subsequent native decode events', () => {
  const image = {}, owner = new AbortController(); let changed = 0
  const callbacks = pictureEvents(image, owner, () => ({ image, owner }), () => changed++, () => changed++)
  owner.abort(); callbacks.onLoad({ width: 2, height: 2 }); callbacks.onError()
  expect(changed).toBe(0)
})
