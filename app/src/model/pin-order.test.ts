// The order of Pinned: each pinned agent's place, a label on its terminal that every phone reads (pin-order.ts).
import { expect, test } from 'nsh/test'
import { PIN_ORDER, isPlace, missingPlaces, moveWrites, placeAfterLast, placeBetween, pinOrder, type Pin } from './pin-order.ts'

test('the label is the phone\'s own, apart from herdr.pinned, which Herdr Desktop App reads', () => {
  expect(PIN_ORDER).toBe('herdr-anywhere.pin-order')
})

test('a place between two others sorts between them, however many times a row is put between the same two', () => {
  let lo = placeBetween(undefined, undefined), hi = placeBetween(lo, undefined)
  expect(lo < hi).toBe(true)
  // Always into the gap just above the lower one: the places get longer but stay apart.
  for (let i = 0; i < 200; i++) {
    const m = placeBetween(lo, hi)
    expect(`${lo} < ${m} < ${hi}: ${lo < m && m < hi && isPlace(m)}`).toBe(`${lo} < ${m} < ${hi}: true`)
    hi = m
  }
  // Before the first and after the last too.
  const first = placeBetween(undefined, lo), last = placeBetween(hi, undefined)
  expect([first < lo, last > hi, isPlace(first), isPlace(last)]).toEqual([true, true, true, true])
})

test('a place is digits and lower-case letters not ending in 0; anything else someone wrote counts as no place', () => {
  for (const ok of ['i', 'a1', 'zz5', '0i']) expect(`${ok} ${isPlace(ok)}`).toBe(`${ok} true`)
  for (const bad of ['', '0', 'a0', 'A', 'i ', '-', 'x'.repeat(257), undefined, 3]) expect(`${String(bad)} ${isPlace(bad)}`).toBe(`${String(bad)} false`)
})

test('Pinned goes by place, a tie by terminal address, and agents with no place after them in the order they came', () => {
  const pins = [
    { address: 't5' }, { address: 't1', place: 'r' }, { address: 't4', place: 'bad place' },
    { address: 't3', place: 'i' }, { address: 't2', place: 'r' }, { address: 't9' },
  ]
  expect(pinOrder(pins).map((p) => p.address)).toEqual(['t3', 't1', 't2', 't5', 't4', 't9'])
})

test('agents with no place get one each, after the last place, in the order Pinned shows them', () => {
  const order = pinOrder([{ address: 't1', place: 'i' }, { address: 't7' }, { address: 't3', place: 'r' }, { address: 't2' }])
  const writes = missingPlaces(order)
  expect(writes.map((w) => w.address)).toEqual(['t7', 't2'])
  expect(['r' < writes[0].place, writes[0].place < writes[1].place]).toEqual([true, true])
  expect(missingPlaces(pinOrder([{ address: 't1', place: 'i' }]))).toEqual([])
  // With none placed yet, the first goes in the middle of the room, so rows fit before it as well as after.
  expect(missingPlaces([{ address: 't1' }])).toEqual([{ address: 't1', place: 'i' }])
})

test('pinning puts an agent after the last one, whether or not the others have places yet', () => {
  expect(placeAfterLast([])).toBe('i')
  expect(placeAfterLast(pinOrder([{ address: 't1', place: 'i' }, { address: 't2', place: 'r' }])) > 'r').toBe(true)
  // An agent pinned in Herdr Desktop App gets its place first (missingPlaces), so the new one goes after that too.
  const order = pinOrder([{ address: 't1', place: 'i' }, { address: 't2' }])
  const after = placeAfterLast(order)
  expect(after > missingPlaces(order)[0].place).toBe(true)
})

test('moving a row writes only its own place, between its new neighbours', () => {
  const order = pinOrder([{ address: 't1', place: 'i' }, { address: 't2', place: 'r' }, { address: 't3', place: 'v' }, { address: 't4', place: 'x' }])
  const apply = (writes: { address: string; place: string }[]) =>
    pinOrder(order.map((p) => ({ ...p, place: writes.find((w) => w.address === p.address)?.place ?? p.place }))).map((p) => p.address)
  const up = moveWrites(order, 't4', 1)
  expect(up.map((w) => w.address)).toEqual(['t4'])
  expect(apply(up)).toEqual(['t1', 't4', 't2', 't3'])
  expect(apply(moveWrites(order, 't4', 0))).toEqual(['t4', 't1', 't2', 't3'])
  expect(apply(moveWrites(order, 't1', 3))).toEqual(['t2', 't3', 't4', 't1'])
  expect(apply(moveWrites(order, 't2', 2))).toEqual(['t1', 't3', 't2', 't4'])
  // Put back where it was: nothing to write.
  expect(moveWrites(order, 't2', 1)).toEqual([])
})

test('two neighbours with the same place (two phones filled one gap at once): the move still lands there, writing what it must', () => {
  const order = pinOrder([{ address: 't1', place: 'i' }, { address: 't2', place: 'r' }, { address: 't3', place: 'r' }, { address: 't4', place: 'v' }])
  const writes = moveWrites(order, 't4', 2)
  const after = pinOrder(order.map((p) => ({ ...p, place: writes.find((w) => w.address === p.address)?.place ?? p.place })))
  expect(after.map((p) => p.address)).toEqual(['t1', 't2', 't4', 't3'])
  expect(writes.every((w) => isPlace(w.place))).toBe(true)
})

test('moving among rows with no place yet places every row it has to, so the move holds', () => {
  const order = pinOrder<Pin>([{ address: 't1' }, { address: 't2' }, { address: 't3' }])
  const writes = moveWrites(order, 't3', 0)
  const after = pinOrder(order.map((p) => ({ ...p, place: writes.find((w) => w.address === p.address)?.place ?? p.place })))
  expect(after.map((p) => p.address)).toEqual(['t3', 't1', 't2'])
})
