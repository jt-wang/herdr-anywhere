// What a tap on an image path does in the app: the sheet opens only for a picture on the computer (ui/picture-taps.ts).
import { expect, test } from 'nsh/test'
import { Signal } from '../store.ts'
import { pictureTaps } from './picture-taps.ts'

function fakeStore(answer: (paths: string[]) => Promise<Record<string, boolean> | null>) {
  const asked: string[][] = []
  return { asked, picture: new Signal<{ path: string; terminal: string } | null>(null),
    images: { read: async () => { throw new Error('not read here') }, check: async (terminal: string, paths: string[]) => { asked.push([terminal, ...paths]); return answer(paths) } } }
}

test('the sheet opens for a picture only; a path only mentioned goes on as a tap', async () => {
  const s = fakeStore(async (ps) => Object.fromEntries(ps.map((p) => [p, p === 'a.png'])))
  const on = pictureTaps(s as any, 'box/t3')
  expect(await on.image('gone.png')).toBe(false)
  expect(s.picture.get()).toBe(null)
  expect(await on.image('a.png')).toBe(true)
  expect(s.picture.get()).toEqual({ path: 'a.png', terminal: 'box/t3' })
  expect(s.asked).toEqual([['box/t3', 'gone.png'], ['box/t3', 'a.png']])
  expect(await on.paths(['a.png', 'gone.png'])).toEqual({ 'a.png': true, 'gone.png': false })
})

// A Companion too old to check opens the sheet as before; one that can't be reached opens it to say so.
test('a computer that can not say opens the sheet, which says why', async () => {
  const old = fakeStore(async () => null)
  expect(await pictureTaps(old as any, 't1').image('a.png')).toBe(true)
  expect(await pictureTaps(old as any, 't1').paths(['a.png'])).toBe(null)
  const down = fakeStore(async () => { throw new Error("Can't reach the computer.") })
  expect(await pictureTaps(down as any, 't1').image('a.png')).toBe(true)
  expect(down.picture.get()).toEqual({ path: 'a.png', terminal: 't1' })
  expect(await pictureTaps(down as any, 't1').paths(['a.png'])).toBe(null)
})
