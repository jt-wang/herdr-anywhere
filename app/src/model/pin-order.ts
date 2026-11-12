// The order of Pinned on the phone: the order agents were pinned in, changed by dragging a row. Each pinned agent's place
// is a label on its terminal in herdr-core, next to `herdr.pinned`, so every phone on that computer reads the same order,
// after a restart or a reconnect too, and nothing is kept on the phone. Only the phone works the order out: Companion
// doesn't list pins, and Herdr Desktop App 0.1.15 keeps its own Pinned by status and reads none of our labels (it reads
// herdr.pinned, herdr.seen, herdr.right-click, herdr.space and herdr.tab).
//
// A place is a fraction written in base 36 without "0." (digits and lower-case letters, not ending in 0): between
// any two there is always another, so a move writes the moved agent's place alone. Two phones moving different
// agents at once both keep their move; the same agent, the last write herdr-core takes. Equal places (two phones
// filled one gap at once) go by terminal address, the same on every phone.

/** The label with an agent's place in Pinned. */
export const PIN_ORDER = 'herdr-anywhere.pin-order'

const DIGITS = '0123456789abcdefghijklmnopqrstuvwxyz'
/** Longer than a place grows in use (each move into one gap adds about a digit every five moves), short enough that a stray value is not one. */
const LONGEST = 256

/** Whether `v` is a place we wrote; anything else (another program's value, a typo by hand) counts as none. */
export function isPlace(v: unknown): v is string {
  return typeof v === 'string' && v.length <= LONGEST && /^[0-9a-z]*[1-9a-z]$/.test(v)
}

// The fraction halfway between a and b (b null: 1), as digits, never ending in 0. a < b.
function mid(a: string, b: string | null): string {
  if (b !== null) {
    let n = 0
    while ((a[n] ?? '0') === b[n]) n++
    if (n > 0) return b.slice(0, n) + mid(a.slice(n), b.slice(n))
  }
  const da = a ? DIGITS.indexOf(a[0]) : 0
  const db = b !== null ? DIGITS.indexOf(b[0]) : DIGITS.length
  if (db - da > 1) return DIGITS[Math.round((da + db) / 2)]
  if (b !== null && b.length > 1) return b[0]
  return DIGITS[da] + mid(a.slice(1), null)
}

/** A place after `lo` and before `hi` (either missing: the start or the end). */
export function placeBetween(lo: string | undefined, hi: string | undefined): string {
  return mid(lo ?? '', hi ?? null)
}

export type Pin = { address: string; place?: string }

/** Pinned's order: by place, equal places by terminal address; agents with no place after them, in the order given. */
export function pinOrder<T extends Pin>(pins: T[]): T[] {
  const placed = pins.filter((p) => isPlace(p.place)).sort((x, y) => cmp(x.place!, y.place!) || cmp(x.address, y.address))
  return [...placed, ...pins.filter((p) => !isPlace(p.place))]
}
const cmp = (x: string, y: string) => (x < y ? -1 : x > y ? 1 : 0)

export type Write = { address: string; place: string }

/** Places for the agents in `order` (pinOrder's) that have none, after the last place, in their order: the agents
 *  pinned in Herdr Desktop App, which writes herdr.pinned only, go last the first time a phone sees them. */
export function missingPlaces(order: Pin[]): Write[] {
  let last = order.filter((p) => isPlace(p.place)).at(-1)?.place
  return order.filter((p) => !isPlace(p.place)).map((p) => ({ address: p.address, place: (last = placeBetween(last, undefined)) }))
}

/** The place of an agent pinned now: after the last one, counting the places missingPlaces is about to give. */
export function placeAfterLast(order: Pin[]): string {
  const last = missingPlaces(order).at(-1)?.place ?? order.filter((p) => isPlace(p.place)).at(-1)?.place
  return placeBetween(last, undefined)
}

/** What to write so that `address` is at index `to` of `order` (pinOrder's): its own place between its new neighbours'.
 *  Neighbours without places, or with the same place, get places too, as far as the move needs them. */
export function moveWrites(order: Pin[], address: string, to: number): Write[] {
  const from = order.findIndex((p) => p.address === address)
  if (from < 0 || from === to) return []
  const target = order.filter((p) => p.address !== address)
  target.splice(Math.max(0, Math.min(to, target.length)), 0, order[from])
  const writes: Write[] = []
  let last: string | undefined
  let moved = false
  for (const [i, p] of target.entries()) {
    const kept = p.address !== address && isPlace(p.place) && (last === undefined || p.place > last)
    if (kept) { last = p.place; continue }
    // Past the moved row, rows with no place all the way down sort after the placed ones anyway.
    if (moved && target.slice(i).every((q) => !isPlace(q.place))) break
    const n = target[i + 1]
    const hi = n && n.address !== address && isPlace(n.place) && (last === undefined || n.place > last) ? n.place : undefined
    last = placeBetween(last, hi)
    writes.push({ address: p.address, place: last })
    if (p.address === address) moved = true
  }
  return writes
}
