import { expect, test } from 'nsh/test'
import { setDiag } from '../diag.ts'
import { Hold, LIFT_MS } from './pin-drag.ts'
import { pinDropEvidence, pinWriteEvidence } from './pin-evidence.ts'

type Event = { what: string; fields: Record<string, string> }
function touch(drop: (dy: number) => void = () => {}) {
  let fire = () => {}; const calls: string[] = []
  const hold = new Hold({ setTimeout: (f) => { fire = f; return 1 }, clearTimeout: () => {},
    lift: () => calls.push('lift'), move: (dy) => calls.push(`move ${dy}`), drop,
    menu: () => calls.push('menu'), cancel: () => calls.push('cancel') })
  return { hold, calls, lift: () => fire() }
}

test('pin touch records grant/lift and one release summary, never a per-move event', () => {
  const events: Event[] = []; setDiag((what, fields) => events.push({ what, fields }))
  try {
    const { hold, lift, calls } = touch((dy) => calls.push(`drop ${dy}`))
    hold.grant(240); lift(); hold.move(210); hold.move(120)
    expect(calls).toEqual(['lift', 'move -30', 'move -120'])
    expect(events.map(e => e.what)).toEqual(['pin-grant', 'pin-lift'])
    hold.release()
    expect(calls.at(-1)).toBe('drop -120')
    expect(events.at(-1)?.what).toBe('pin-release')
    expect(events.at(-1)?.fields).toEqual({ gesture: events[0].fields.gesture, moves: '2', firstY: '210', lastY: '120', firstDy: '-30', lastDy: '-120', dy: '-120', lifted: '1', dragging: '1' })
  } finally { setDiag(() => {}) }
})

test('pin terminate is distinct and a drop preserves its original thrown object', () => {
  const events: Event[] = []; setDiag((what, fields) => events.push({ what, fields }))
  try {
    const a = touch(); a.hold.grant(100); a.lift(); a.hold.move(130); a.hold.terminate()
    expect(a.calls).toEqual(['lift', 'move 30', 'cancel'])
    expect(events.at(-1)?.what).toBe('pin-terminate')
    const original = new Error('synthetic drop failure'); const b = touch(() => { throw original })
    b.hold.grant(100); b.lift(); b.hold.move(50)
    let caught: unknown; try { b.hold.release() } catch (e) { caught = e }
    expect(caught).toBe(original)
    expect(events.at(-1)?.what).toBe('pin-release')
  } finally { setDiag(() => {}) }
})

test('pin drop records only numeric geometry used for its target', () => {
  const events: Event[] = []; setDiag((what, fields) => events.push({ what, fields }))
  try {
    pinDropEvidence(3, { from: 3, target: 1, finger: -100, scrolled: 6, dy: -94,
      held: { top: 240, h: 40 }, list: { top: 100, viewH: 500 }, rows: [{ y: 0, h: 40 }, { y: 40, h: 60 }] })
    expect(events).toEqual([{ what: 'pin-drop', fields: { gesture: '3', from: '3', target: '1', finger: '-100', scrolled: '6', dy: '-94', held: '1', heldTop: '240', heldH: '40', listTop: '100', listH: '500', rowYs: '0,40', rowHs: '40,60' } }])
  } finally { setDiag(() => {}) }
})

test('pin write records success or rejection with final index and preserves the promise', async () => {
  const events: Event[] = []; setDiag((what, fields) => events.push({ what, fields }))
  try {
    const ok = Promise.resolve(); expect(pinWriteEvidence(3, 3, 1, () => ok, () => 1)).toBe(ok); await ok; await Promise.resolve()
    const original = new Error('synthetic refusal'); const bad = Promise.reject(original)
    const observed = pinWriteEvidence(4, 3, 1, () => bad, () => 3)
    expect(observed).toBe(bad)
    let caught: unknown; try { await observed } catch (e) { caught = e }
    expect(caught).toBe(original)
    expect(events.map(e => e.what)).toEqual(['pin-write-start', 'pin-write-success', 'pin-write-start', 'pin-write-rejected'])
    expect(events.at(-1)?.fields).toEqual({ gesture: '4', from: '3', target: '1', after: '3' })
    expect(JSON.stringify(events).includes(original.message)).toBe(false)
  } finally { setDiag(() => {}) }
})

test('a throwing diagnostic subscriber cannot change pin gesture callbacks', () => {
  setDiag(() => { throw new Error('synthetic hook failure') })
  try {
    const { hold, lift, calls } = touch(dy => calls.push(`drop ${dy}`))
    hold.grant(100); lift(); hold.move(50); hold.release()
    expect(calls).toEqual(['lift', 'move -50', 'drop -50'])
  } finally { setDiag(() => {}) }
})
