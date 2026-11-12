// computed() whose object stays reachable while its component lives. nsh 0.3.0 counts a live computed's value as its
// object's child: when the object is left only in a cycle with the value, the collector frees the value under the
// computed, and the computed's next run frees it again (a crash; upstream longbridge/nsh 0d0338ac, #1014, fixed from
// nsh 0.5.0). Held here, the object is never only in a cycle; the component's cleanup lets it go. App.kept.test.tsx.
import { cleanup, computed, type Accessor, type SignalOptions } from 'nsh'

const live = new Set<Accessor<unknown>>()

export function kept<T>(fn: (previous: T | undefined) => T, options?: SignalOptions<T>): Accessor<T> {
  const c = computed(fn, options)
  live.add(c as Accessor<unknown>)
  cleanup(() => { live.delete(c as Accessor<unknown>) })
  return c
}

/** How many are held now (for the app tests). */
export const keptCount = () => live.size
