// How far a picture in its sheet zooms and what a double tap does (ui/picture-zoom.ts).
import { expect, test } from 'nsh/test'
import { DOUBLE_TAP_MS, doubleTapScale, isDoubleTap, maxZoom, pictureZoom } from './picture-zoom.ts'

test('a picture zooms to twice its own pixels, between 4 and 16 times its fitted size', () => {
  expect(maxZoom({ width: 960, height: 640 }, { width: 326, height: 217 })).toBe(960 * 2 / 326)
  expect(maxZoom({ width: 200, height: 100 }, { width: 200, height: 100 })).toBe(4) // shown at its own size
  expect(maxZoom({ width: 8000, height: 4000 }, { width: 326, height: 163 })).toBe(16)
  expect(maxZoom({ width: 0, height: 0 }, { width: 326, height: 160 })).toBe(4) // not loaded yet
})

test('a double tap zooms in to 2.5 times, or as far as it may; zoomed in at all, back to the fitted size', () => {
  expect(doubleTapScale(1, 5.9)).toBe(2.5)
  expect(doubleTapScale(1.005, 5.9)).toBe(2.5) // a pinch that let go just above 1 is not zoomed in
  expect(doubleTapScale(1, 2)).toBe(2)
  expect(doubleTapScale(1.3, 5.9)).toBe(1)
  expect(doubleTapScale(5.9, 5.9)).toBe(1)
})

test('two taps make a double tap when the second comes soon after, near the first', () => {
  const a = { at: 1000, x: 100, y: 80 }
  expect(isDoubleTap(null, a)).toBe(false)
  expect(isDoubleTap(a, { at: 1000 + DOUBLE_TAP_MS - 1, x: 110, y: 90 })).toBe(true)
  expect(isDoubleTap(a, { at: 1000 + DOUBLE_TAP_MS + 1, x: 100, y: 80 })).toBe(false) // too late
  expect(isDoubleTap(a, { at: 1100, x: 160, y: 80 })).toBe(false) // too far
})

test('a double tap zooms in on the tapped point, the next one back out; a pinch counts as zoomed in', () => {
  const asked: unknown[] = []
  const z = pictureZoom((to) => asked.push(to))
  const size = { natural: { width: 960, height: 640 }, box: { width: 326, height: 217 } }
  z.tap({ at: 0, x: 50, y: 40 }, size)
  expect(asked).toEqual([]) // one tap: nothing
  z.tap({ at: 200, x: 52, y: 41 }, size)
  expect(asked).toEqual([{ scale: 2.5, x: 52, y: 41, animated: true }])
  expect(z.zoomed()).toBe(true)
  z.tap({ at: 1000, x: 10, y: 10 }, size)
  z.tap({ at: 1100, x: 10, y: 10 }, size)
  expect(asked[1]).toEqual({ scale: 1, x: 10, y: 10, animated: true })
  expect(z.zoomed()).toBe(false)
  z.tap({ at: 2000, x: 10, y: 10 }, size)
  z.scrolled(3.2) // pinched between the two taps: the double tap goes back out
  z.tap({ at: 2100, x: 10, y: 10 }, size)
  expect(asked[2]).toEqual({ scale: 1, x: 10, y: 10, animated: true })
  // three quick taps are one double tap and the first of another
  z.tap({ at: 3000, x: 10, y: 10 }, size); z.tap({ at: 3100, x: 10, y: 10 }, size); z.tap({ at: 3200, x: 10, y: 10 }, size)
  expect(asked.length).toBe(4)
  z.reset()
  expect(z.zoomed()).toBe(false)
})
