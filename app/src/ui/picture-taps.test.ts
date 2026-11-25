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
  expect(down.picture.get()).toBe(null)
  expect(await pictureTaps(down as any, 't1').paths(['a.png'])).toBe(null)
})

test('a background-confirmed picture opens without waiting for another check', async () => {
  const s = fakeStore(async (ps) => Object.fromEntries(ps.map(p => [p, true])))
  const taps = pictureTaps(s as any, 't1')
  await taps.paths(['a.png'])
  s.images.check = async () => new Promise(() => {})
  const pending = taps.image('a.png')
  expect(s.picture.get()).toEqual({ path: 'a.png', terminal: 't1' })
  expect(await pending).toBe(true)
})

test('unknown picture checks give cancellable feedback and never open after cancellation', async () => {
  let answer!: (value: Record<string, boolean>) => void
  const s = fakeStore(async () => new Promise(resolve => { answer = resolve }))
  const notices: any[] = []
  const taps = pictureTaps({ ...s, toast: (n: any) => notices.push(n) } as any, 't1')
  const pending = taps.image('a.png')
  await Promise.resolve()
  expect(notices.length).toBe(1)
  notices[0].action.run()
  answer({ 'a.png': true })
  expect(await pending).toBe(true)
  expect(notices.length).toBe(1)
  expect(s.picture.get()).toBe(null)
})

test('an unresponsive check times out visibly instead of opening a late modal', async () => {
  let finish!: (x: Record<string, boolean>) => void
  const s = fakeStore(async () => new Promise(r => { finish = r }))
  const notices: any[] = []
  const taps = pictureTaps({ ...s, toast: (n: any) => notices.push(n) } as any, 't1')
  expect(await taps.image('a.png')).toBe(true)
  expect(notices.length).toBe(2)
  expect(notices[1].level).toBe('error')
  expect(notices[1].title).toBe('The picture took too long to load. Try again.')
  finish({ 'a.png': true }); await Promise.resolve()
  expect(s.picture.get()).toBe(null)
})

test('a disposed terminal cannot open a late picture', async () => {
  let finish!: (x: Record<string, boolean>) => void
  const s = fakeStore(async () => new Promise(r => { finish = r }))
  const taps = pictureTaps(s as any, 't1')
  const pending = taps.image('a.png'); await Promise.resolve()
  taps.dispose(); finish({ 'a.png': true })
  await pending
  expect(s.picture.get()).toBe(null)
})

test('changing the foreground folder invalidates a confirmed relative picture', async () => {
  let cwd = '/work/first', valid = true
  const s = fakeStore(async ps => Object.fromEntries(ps.map(p => [p, valid])))
  const taps = pictureTaps({ ...s, term: () => ({ pid: 1, cwd, foreground: { pid: 2, cwd } }) } as any, 't1')
  await taps.paths(['a.png'])
  cwd = '/work/second'; valid = false
  expect(await taps.image('a.png')).toBe(false)
  expect(s.picture.get()).toBe(null)
})

test('older background confirmation cannot replace a newer negative check', async () => {
  const answers: ((x: Record<string, boolean>) => void)[] = []
  const s = fakeStore(async () => new Promise(r => answers.push(r)))
  const taps = pictureTaps(s as any, 't1')
  const first = taps.paths(['a.png']), next = taps.paths(['a.png'])
  answers[1]({ 'a.png': false }); await next
  answers[0]({ 'a.png': true }); await first
  s.images.check = async () => ({ 'a.png': false })
  expect(await taps.image('a.png')).toBe(false)
  expect(s.picture.get()).toBe(null)
})

for (const message of ['The computer is still reading other pictures. Try again shortly.', "Can't reach the computer."]) test(`an immediate check refusal retains its reason: ${message}`, async () => {
  const notices: any[] = []
  const s = fakeStore(async () => { throw new Error(message) })
  await pictureTaps({ ...s, toast: (n: any) => notices.push(n) } as any, 't1').image('a.png')
  expect(notices[1].title).toBe(message)
  expect(s.picture.get()).toBe(null)
})

test('an unknown check failure does not claim a timeout', async () => {
  const notices: any[] = []
  const s = fakeStore(async () => { throw null })
  await pictureTaps({ ...s, toast: (n: any) => notices.push(n) } as any, 't1').image('a.png')
  expect(notices[1].title).toBe("The computer can't read this picture.")
})
