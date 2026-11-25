// Background checks identify picture paths; confirmed taps need no second round trip before opening.
import type { Store } from '../store.ts'
import { t } from '../strings/index.ts'
import { pictureRequest } from '../picture-request.ts'

export function pictureTaps(store: Pick<Store, 'images' | 'picture'> & Partial<Pick<Store, 'toast' | 'conn' | 'toasts' | 'dismiss' | 'term'>>, terminal: string) {
  const confirmed = new Map<string, number>()
  let checking: AbortController | undefined
  let epoch = 0, serial = 0
  const versions = new Map<string, number>()
  const identity = () => { const t = store.term?.(terminal); return JSON.stringify([t?.pid, t?.created_ms, t?.cwd, t?.foreground?.pid, t?.foreground?.cwd]) }
  let owner = identity()
  const sameOwner = () => {
    const next = identity()
    if (next !== owner) { owner = next; epoch++; checking?.abort(); confirmed.clear(); versions.clear() }
    return owner
  }
  const reset = store.conn?.sub(() => { epoch++; checking?.abort(); confirmed.clear() })
  return {
    dispose() { epoch++; reset?.(); checking?.abort(); confirmed.clear() },
    async image(path: string): Promise<boolean> {
      sameOwner()
      checking?.abort()
      const context = owner
      if (Date.now() - (confirmed.get(path) ?? 0) < 30_000) {
        store.picture.set({ path, terminal })
        return true
      }
      const controller = new AbortController()
      checking = controller
      store.toast?.({ title: t('pictureChecking'), body: t('pictureCancelCheck'), level: 'info', action: { run: () => controller.abort() } })
      const notice = store.toasts?.get().at(-1)?.id
      let found: Record<string, boolean> | null | undefined
      try {
        found = await pictureRequest(signal => store.images?.check(terminal, [path], signal) ?? Promise.resolve(null), controller.signal, 5000)
      } catch (error) {
        if (!controller.signal.aborted) store.toast?.({ title: error instanceof Error ? (error.message === 'picture-request-cancelled' ? t('pictureTimeout') : error.message || t('pictureCantRead')) : t('pictureCantRead'), level: 'error' })
        return true // consumed; never turn a timed-out picture request into an unexpected terminal click
      } finally { if (notice !== undefined) store.dismiss?.(notice) }
      if (sameOwner() !== context || controller.signal.aborted || checking !== controller) return true
      const picture = found ? found[path] === true : true
      if (picture) store.picture.set({ path, terminal })
      return picture
    },
    async paths(paths: string[]): Promise<Record<string, boolean> | null> {
      const context = sameOwner(), before = epoch, batch = ++serial
      if (versions.size + paths.length > 500) { versions.clear(); confirmed.clear() }
      for (const path of paths) versions.set(path, batch)
      const found = await store.images?.check(terminal, paths).catch(() => null) ?? null
      if (sameOwner() !== context || before !== epoch) return null
      const fresh: Record<string, boolean> = {}
      if (found) for (const [path, picture] of Object.entries(found)) {
        if (versions.get(path) !== batch) continue
        fresh[path] = picture
        if (picture) confirmed.set(path, Date.now()); else confirmed.delete(path)
      }
      if (confirmed.size > 500) confirmed.clear()
      return found ? fresh : null
    },
  }
}
