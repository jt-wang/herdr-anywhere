// TailPocket on a phone: the native bridge the app's host project attaches (ios/Sources/App.swift, android
// MainActivity), through TailPocket's NSH client (third_party/tailpocket-086e368/sdk/js). It keeps the computer's
// link, which holds the phone's private key, in the Keychain / Android Keystore under one slot.
import { tailPocket } from '../../third_party/tailpocket-086e368/sdk/js/nsh.ts'
import type { Tunnel } from './link.ts'

/** What `herdr-anywhere pair` starts its invites with (companion/internal/tailcat Prefix); TailPocket's own differs. */
export const PREFIX = 'herdr1:'
/** Its v2 invites (TailPocket v0.3), shown once the computer has a relay or the encrypted local network. */
export const PREFIX2 = 'herdr2:'
const SLOT = 'herdr'

export const nativeTunnel: Tunnel = {
  async pair(invite, device, onEmoji) {
    const decoded = invite.startsWith(PREFIX2) ? await tailPocket.decodeInvite2(invite, PREFIX2) : await tailPocket.decodeInvite(invite, PREFIX)
    const link = await tailPocket.pair(decoded, { device, save: true, slot: SLOT, onEmoji: onEmoji ?? (() => {}) })
    return link.payload ?? ''
  },
  scan: () => tailPocket.scan(PREFIX, PREFIX2),
  setTailcatOnly: (on) => tailPocket.setTailcatOnly(on),
  async connect() {
    const c = await tailPocket.connect({ slot: SLOT })
    // A link paired over Tailcat before v0.3 has no `pk`: sync registers this phone's end-to-end key and saves the link
    // with the computer's every route, so it is never paired again. Not awaited: it may take its 30 s over Tailcat, and
    // a computer not on v0.3 yet doesn't serve it at all; either way the tunnel goes on over Tailcat now.
    void c.link().then((l) => (l.pk ? undefined : c.sync())).catch(() => {})
    return { baseUrl: c.baseUrl, close: () => void c.close().catch(() => {}), path: () => c.path() }
  },
  forget: () => void tailPocket.store.deleteLink(SLOT).catch(() => {}),
}
