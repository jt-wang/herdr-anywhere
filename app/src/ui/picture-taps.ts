// A tap on an image path a terminal shows, and the paths it shows: whether each is a picture on the computer, which
// the computer says (link.ts imageApi.check). The sheet (picture-sheet.tsx) opens only for a picture.
import type { Store } from '../store.ts'

export function pictureTaps(store: Pick<Store, 'images' | 'picture'>, terminal: string) {
  return {
    async image(path: string): Promise<boolean> {
      // null: a Companion from before the check, which can't say; a failure: the sheet says why it shows nothing.
      const found = await store.images?.check(terminal, [path]).catch(() => null)
      const picture = found ? found[path] === true : true
      if (picture) store.picture.set({ path, terminal })
      return picture
    },
    paths(paths: string[]): Promise<Record<string, boolean> | null> {
      return store.images?.check(terminal, paths).catch(() => null) ?? Promise.resolve(null)
    },
  }
}
