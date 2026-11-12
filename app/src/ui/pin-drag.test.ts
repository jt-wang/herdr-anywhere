// Dragging a Pinned row: hold it still to lift it, move to drag, let go to drop; held still and let go, its menu.
import { expect, test } from 'nsh/test'
import { EdgeScroll, Hold, LIFT_MS, shifts, targetIndex } from './pin-drag.ts'

function hold() {
  const said: string[] = []
  const timers: { at: number; f: () => void }[] = []
  let now = 0
  const h = new Hold({
    setTimeout: (f, ms) => { timers.push({ at: now + ms, f }); return timers.length },
    clearTimeout: (id) => { if (typeof id === "number") timers[id - 1].f = () => {} },
    lift: () => said.push('lift'), move: (dy) => said.push(`move ${dy}`), drop: (dy) => said.push(`drop ${dy}`),
    menu: () => said.push('menu'), cancel: () => said.push('cancel'),
  })
  const run = (ms: number) => { now += ms; for (const t of timers.splice(0)) if (t.at <= now) t.f(); else timers.push(t) }
  return { h, said, run }
}

test('held still past the lift time, a row lifts; moved, it follows the finger; let go, it drops where it is', () => {
  const { h, said, run } = hold()
  h.grant(100)
  run(LIFT_MS - 1)
  expect(said).toEqual([])
  run(1)
  // Lifted, the row keeps the touch: the list around it may not take it to scroll.
  expect(h.wantsToKeep()).toBe(true)
  h.move(160)
  h.move(40)
  h.release()
  expect(said).toEqual(['lift', 'move 60', 'move -60', 'drop -60'])
})

test('lifted and let go without moving, the row opens its menu, so the long press still pins, unpins and closes', () => {
  const { h, said, run } = hold()
  h.grant(100)
  run(LIFT_MS)
  // A finger never stays exactly still: a few points don't count as a drag.
  h.move(103)
  h.release()
  expect(said).toEqual(['lift', 'menu'])
})

test('a tap does nothing here (the row\'s press opens the agent); after a lift the press that follows is not a tap', () => {
  const { h, said, run } = hold()
  h.grant(100)
  run(80)
  h.release()
  expect([said, h.pressed()]).toEqual([[], true])
  h.grant(100)
  run(LIFT_MS)
  h.release()
  expect(h.pressed()).toBe(false)
  // The next touch starts afresh.
  h.grant(100)
  h.release()
  expect(h.pressed()).toBe(true)
})

test('moved before the lift, the touch is the list\'s to scroll: it lets go, and nothing lifts later', () => {
  const { h, said, run } = hold()
  h.grant(100)
  run(50)
  expect(h.wantsToKeep()).toBe(false)
  h.terminate()
  run(LIFT_MS)
  expect(said).toEqual([])
  // A finger that slides off before the lift is scrolling or swiping too, even while the list hasn't taken it yet.
  h.grant(100)
  h.move(130)
  run(LIFT_MS)
  h.release()
  expect(said).toEqual([])
})

test('taken away after the lift (a call, the app going away), the row goes back and nothing is written', () => {
  const { h, said, run } = hold()
  h.grant(100)
  run(LIFT_MS)
  h.move(200)
  h.terminate()
  expect(said).toEqual(['lift', 'move 100', 'cancel'])
})

test('where a dragged row lands: past the middle of a neighbour it takes that neighbour\'s place', () => {
  // Rows of 40, 60 (one with an activity line), 40 and 40 points, one under the other.
  const rows = [{ y: 0, h: 40 }, { y: 40, h: 60 }, { y: 100, h: 40 }, { y: 140, h: 40 }]
  expect(targetIndex(rows, 3, 0)).toBe(3)
  expect(targetIndex(rows, 3, -19)).toBe(3)
  expect(targetIndex(rows, 3, -21)).toBe(2)
  expect(targetIndex(rows, 3, -100)).toBe(1)
  expect(targetIndex(rows, 3, -500)).toBe(0)
  expect(targetIndex(rows, 0, 31)).toBe(1)
  expect(targetIndex(rows, 0, 29)).toBe(0)
  expect(targetIndex(rows, 0, 500)).toBe(3)
})

test('while a row is dragged from 3 to 1, the rows it passes move down by its height to make room', () => {
  const rows = [{ y: 0, h: 40 }, { y: 40, h: 60 }, { y: 100, h: 40 }, { y: 140, h: 40 }]
  expect(shifts(rows, 3, 1)).toEqual([0, 40, 40, 0])
  expect(shifts(rows, 0, 2)).toEqual([0, -40, -40, 0])
  expect(shifts(rows, 2, 2)).toEqual([0, 0, 0, 0])
})

function edge(at: number, contentH = 2000) {
  const said: string[] = []
  let tick: (() => void) | undefined
  const e = new EdgeScroll({
    setInterval: (f) => { tick = f; return 1 }, clearInterval: () => { tick = undefined },
    list: () => ({ at, top: 100, viewH: 600, contentH }),
    show: (y) => said.push(`show ${y}`), scrollTo: (y) => said.push(`scrollTo ${y}`),
  })
  const scrolled: number[] = []
  const run = (n: number) => { for (let i = 0; i < n && tick; i++) tick() }
  return { e, said, scrolled, run, follow: (y: number) => e.follow(y, (by) => scrolled.push(by)), ticking: () => !!tick }
}

test('a row held near the top edge moves the content down, and the list scrolls there only once the finger is up', () => {
  // On Android a scroll under a finger ends the touch (nsh's ScrollBox calls cancelTouches): measured on the emulator
  // (e2e/android-pin-order.sh's edge step): the row dropped as the list moved.
  const { e, said, scrolled, run, follow } = edge(200)
  e.begin()
  follow(120)
  run(3)
  expect(said).toEqual(['show -6', 'show -12', 'show -18'])
  expect(scrolled).toEqual([-6, -12, -18])
  e.end()
  expect(said.slice(3)).toEqual(['show 0', 'scrollTo 182'])
})

test('the content shows no further than the list goes, at either end', () => {
  const top = edge(10)
  top.e.begin(); top.follow(120); top.run(5)
  expect(top.said).toEqual(['show -6', 'show -10'])
  const bottom = edge(1395)
  bottom.e.begin(); bottom.follow(690); bottom.run(5)
  expect(bottom.said).toEqual(['show 5'])
})

test('away from the edges nothing moves, and let go without a move nothing scrolls', () => {
  const { e, said, follow, ticking, run } = edge(200)
  e.begin()
  follow(120)
  run(1)
  follow(400)
  expect(ticking()).toBe(false)
  e.end()
  expect(said).toEqual(['show -6', 'show 0', 'scrollTo 194'])
  const still = edge(200)
  still.e.begin(); still.follow(400); still.e.end()
  expect(still.said).toEqual([])
})
