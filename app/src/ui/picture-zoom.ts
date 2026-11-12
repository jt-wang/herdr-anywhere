// How far a picture in its sheet (picture-sheet.tsx) zooms, and what a double tap on it does. The pinch and the drag
// are the scroll view's own (nsh `Scroll` with `maximumZoomScale`: UIScrollView on iOS), which keeps the picture's
// edges inside the box.
type Size = { width: number; height: number }

/** Taps closer than this in time, and than DOUBLE_TAP_PT apart, are a double tap. */
export const DOUBLE_TAP_MS = 300
const DOUBLE_TAP_PT = 40
/** A double tap on a picture at its fitted size zooms in this far. */
const DOUBLE_TAP_ZOOM = 2.5

/** As far as a picture fitted into `box` zooms: to twice its own pixels, and between 4 and 16 times. */
export function maxZoom(natural: Size, box: Size): number {
  if (!natural.width || !box.width) return 4
  return Math.min(16, Math.max(4, (natural.width * 2) / box.width))
}

/** The scale a double tap goes to from `scale`: zoomed in at all, back to 1; else in, as far as `max` allows. */
export function doubleTapScale(scale: number, max: number): number {
  return scale > 1.01 ? 1 : Math.min(DOUBLE_TAP_ZOOM, max)
}

export type Tap = { at: number; x: number; y: number }

export function isDoubleTap(last: Tap | null, tap: Tap): boolean {
  return !!last && tap.at - last.at < DOUBLE_TAP_MS && Math.hypot(tap.x - last.x, tap.y - last.y) < DOUBLE_TAP_PT
}

export type ZoomTo = { scale: number; x: number; y: number; animated: boolean }

/** A picture's zoom as its sheet sees it: taps (`tap`) and the scroll view's own zoom (`scrolled`, from `onScroll`),
 * and where a double tap asks the scroll view to go (`zoomTo`, the scroll view's `ref.zoomTo`). */
export function pictureZoom(zoomTo: (to: ZoomTo) => void) {
  let scale = 1
  let last: Tap | null = null
  return {
    tap(t: Tap, size: { natural: Size; box: Size }) {
      if (!isDoubleTap(last, t)) { last = t; return }
      last = null
      scale = doubleTapScale(scale, maxZoom(size.natural, size.box))
      zoomTo({ scale, x: t.x, y: t.y, animated: true })
    },
    scrolled(zoomScale: number) { scale = zoomScale },
    zoomed: () => scale > 1.01,
    reset() { scale = 1; last = null },
  }
}
