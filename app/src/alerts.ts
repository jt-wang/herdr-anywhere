// What an alert does beyond the in-app toast. herdr-app (src/window.rs `alert`, src/sound.rs) plays Herdr's sound for
// every alert, whether or not the person is looking; a phone also posts the system's notification while the app is not
// in front, where its toast can't be seen.
import { Signal, type Toast } from './store.ts'

export type AlertKind = 'blocked' | 'done'
export type Sound = { id: number; kind: AlertKind }
/** herdr-app gives a sound player this long before it stops it (src/sound.rs PLAYER_LIMIT). */
export const SOUND_LIMIT_MS = 15_000

export class Alerts {
  /** The sounds playing now: each is a player on screen until it ends. */
  readonly sounds = new Signal<Sound[]>([])
  private next = 0
  private setTimeout: typeof setTimeout

  /** `soundAway: false`: where the system's notification makes the sound (Android, MainActivity's channels play
   *  Herdr's sounds), away from the app the notification is the only sound, so its channel and the ringer decide it. */
  constructor(private io: { notify?: (t: Toast, kind: AlertKind, quiet?: boolean) => void; setTimeout?: typeof setTimeout; soundAway?: boolean } = {}) {
    this.setTimeout = io.setTimeout ?? globalThis.setTimeout.bind(globalThis)
  }

  /** `again`: new words for the last alert of its terminal, put on its notification without a sound. */
  ring(kind: AlertKind, toast: Toast, front: boolean, again = false) {
    if (again) {
      if (!front) this.io.notify?.(toast, kind, true)
      return
    }
    if (!front) this.io.notify?.(toast, kind)
    if (!front && this.io.soundAway === false) return
    const sound = { id: ++this.next, kind }
    this.sounds.set([...this.sounds.get(), sound])
    // A player that never says it ended (a sound that can't load) must not stay forever.
    this.setTimeout(() => this.ended(sound.id), SOUND_LIMIT_MS)
  }

  ended(id: number) {
    const left = this.sounds.get().filter((s) => s.id !== id)
    if (left.length !== this.sounds.get().length) this.sounds.set(left)
  }
}
