// Dragging a Pinned row with a finger (home.tsx). The row takes its touches through the responder system and keeps
// them once lifted, and the list stops scrolling meanwhile: measured on the Android emulator (API 35) and the iOS
// simulator (26.5) with a probe app, up and down. Without the list stopping, a drag towards where
// the list can scroll was the list's. A tap stays the row's press; held still and let go, the row
// opens the menu the long press opened before.

/** How long a finger rests on a row before it lifts: Android's long-press time (ViewConfiguration, 400 ms since 12). */
export const LIFT_MS = 400
/** How far a finger may wander and still be holding still, in points. */
export const SLOP = 8

export type HoldIO = {
  setTimeout: (f: () => void, ms: number) => unknown
  clearTimeout: (id: unknown) => void
  lift: () => void
  /** The finger moved by `dy` points since it went down (once it went further than SLOP). */
  move: (dy: number) => void
  drop: (dy: number) => void
  menu: () => void
  /** The touch was taken away after the lift: the row goes back. */
  cancel: () => void
}

/** One row's touch: down (`grant`), moves, up (`release`) or taken away (`terminate`). */
export class Hold {
  private y0 = 0
  private dy = 0
  private lifted = false
  private dragging = false
  private timer: unknown
  constructor(private io: HoldIO) {}

  grant(y: number) {
    this.stop()
    this.y0 = y
    this.dy = 0
    this.lifted = this.dragging = false
    this.timer = this.io.setTimeout(() => { this.timer = undefined; this.lifted = true; this.io.lift() }, LIFT_MS)
  }

  move(y: number) {
    this.dy = y - this.y0
    const far = Math.abs(this.dy) > SLOP
    if (!this.lifted) { if (far) this.stop(); return }
    if (far) this.dragging = true
    if (this.dragging) this.io.move(this.dy)
  }

  release() {
    this.stop()
    if (!this.lifted) return
    if (this.dragging) this.io.drop(this.dy)
    else this.io.menu()
  }

  terminate() {
    this.stop()
    if (this.lifted) this.io.cancel()
  }

  /** Whether the row keeps the touch from the list around it: once lifted. */
  wantsToKeep() {
    return this.lifted
  }

  /** Whether the row's press is a tap: not after a lift, which was a hold. */
  pressed() {
    return !this.lifted
  }

  private stop() {
    if (this.timer !== undefined) this.io.clearTimeout(this.timer)
    this.timer = undefined
  }
}

export type RowBox = { y: number; h: number }

/** Where row `from`, dragged `dy` points, lands: the index whose neighbours' middles it has passed. */
export function targetIndex(rows: RowBox[], from: number, dy: number): number {
  const r = rows[from]
  if (!r) return from
  const top = r.y + dy, bottom = r.y + r.h + dy
  let to = from
  for (let i = from - 1; i >= 0 && top < rows[i].y + rows[i].h / 2; i--) to = i
  for (let i = from + 1; i < rows.length && bottom > rows[i].y + rows[i].h / 2; i++) to = i
  return to
}

/** How far each row moves while row `from` is held over index `to`: the rows between make room by its height. */
export function shifts(rows: RowBox[], from: number, to: number): number[] {
  const h = rows[from]?.h ?? 0
  return rows.map((_, i) => (i === from ? 0 : from < to && i > from && i <= to ? -h : to < from && i >= to && i < from ? h : 0))
}

export type ListBox = { at: number; top: number; viewH: number; contentH: number }

export type EdgeIO = {
  setInterval: (f: () => void, ms: number) => unknown
  clearInterval: (id: unknown) => void
  /** Where the list is: its offset, its top on screen, its height and its content's (points). */
  list: () => ListBox
  /** Shows the content `by` points further down it than the list is scrolled (0: as scrolled). */
  show: (by: number) => void
  scrollTo: (y: number) => void
}

/** How far into the list a finger counts as at its edge, and how far the content moves each frame there (points). */
export const EDGE = 56
export const EDGE_STEP = 6

/** While a row is dragged near the top or bottom of the list, the content moves under it. The list itself scrolls
 *  there only after the finger is up: on Android a scroll under a finger ends the touch, the row dropping (nsh's
 *  ScrollBox takes any scroll then for the finger's and cancels the touches; e2e/android-pin-order.sh's edge step). */
export class EdgeScroll {
  private by = 0
  private speed = 0
  private tick: unknown
  constructor(private io: EdgeIO) {}

  begin() {
    this.stop()
    this.by = 0
  }

  /** The finger is at `pageY`; `scrolled` hears how far the content has moved since begin. */
  follow(pageY: number, scrolled: (by: number) => void) {
    const l = this.io.list()
    this.speed = !l.viewH ? 0 : pageY < l.top + EDGE ? -EDGE_STEP : pageY > l.top + l.viewH - EDGE ? EDGE_STEP : 0
    if (!this.speed) return this.stop()
    if (this.tick !== undefined) return
    this.tick = this.io.setInterval(() => {
      const b = this.io.list()
      const next = Math.max(-b.at, Math.min(Math.max(0, b.contentH - b.viewH) - b.at, this.by + this.speed))
      if (next === this.by) return
      this.by = next
      this.io.show(next)
      scrolled(next)
    }, 16)
  }

  /** The finger is up: the list scrolls to what shows, and the content goes back into it. */
  end() {
    this.stop()
    if (!this.by) return
    const y = this.io.list().at + this.by
    // The content back first, then the scroll: nsh sends a node's commands after the changes made before them, so both
    // land in one frame.
    this.io.show(0)
    this.io.scrollTo(y)
    this.by = 0
  }

  private stop() {
    if (this.tick !== undefined) this.io.clearInterval(this.tick)
    this.tick = undefined
    this.speed = 0
  }
}
