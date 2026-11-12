// Starts the app over the device's storage: App.tsx for every build, and ios-e2e.tsx with a pairing link to open.
import { host, mount, platform, Storage } from 'nsh'
import { Linker } from './link.ts'
import { dropPairFromPage, pageAddress } from './page.ts'
import { PrefStore } from './prefs.ts'
import { Store } from './store.ts'
import { Root } from './ui/root.tsx'
import { nativeTunnel } from './tunnel.ts'
import '../assets/fonts/Geist.ttf'
import '../assets/fonts/JetBrainsMono-Variable.ttf'

// TailPocket lives in the phone apps' host projects; a browser pairs over the LAN, or through the page's computer.
const tunnel = platform.os !== 'web' && host.available() ? nativeTunnel : undefined
const io = {
  load: (k: string) => Storage.getItem(k),
  save: (k: string, v: string) => void Storage.setItem(k, v).catch(() => {}),
  remove: (k: string) => void Storage.removeItem(k).catch(() => {}),
}
const DEVICE: Record<string, string> = { ios: platform.idiom === 'pad' ? 'iPad' : 'iPhone', android: 'Android phone', web: 'Browser' }

/** `pairLink`: a link (or a TailPocket invite) to pair with at once, as a web page opened at a link does.
 *  `updateEveryMs`: how often a connected phone reads the computer's answer about new versions (an hour; an e2e's is short). */
export function start(o: { pairLink?: string; opensSettings?: boolean; updateEveryMs?: number } = {}) {
  void pageAddress().then((page) => mount(() => (
    <Root deps={{
      linker: new Linker({ ...io, tunnel }),
      prefs: new PrefStore(io),
      makeStore: (l) => new Store({ pipe: () => l.pipe(), tunnelled: () => l.link.get()?.via === 'tailpocket', load: io.load, save: io.save }),
      device: DEVICE[platform.os] ?? 'Phone',
      pairLink: o.pairLink ?? page?.href,
      origin: page?.origin,
      pairLinkUsed: dropPairFromPage,
      opensSettings: o.opensSettings,
      tunnel,
      updateEveryMs: o.updateEveryMs,
    }} />
  )))
}
