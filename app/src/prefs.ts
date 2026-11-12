// This phone's own settings (herdr-app's Look and Alerts sections), kept in the app's storage.
import { Signal } from './store.ts'

export type Appearance = 'system' | 'light' | 'dark'
/** Where typing goes in a pane: our input box, or the phone's own keyboard straight into the terminal. */
export type TypeIn = 'composer' | 'terminal'
export type Prefs = { appearance: Appearance; typeIn: TypeIn; fontSize: number; alertBlocked: boolean; alertDone: boolean; stayConnected: boolean; dismissedUpdate: string; askedNotifications: boolean
  /** Agent rows say the program's name ("Claude Code") before the title; off, the icon alone says it (Settings › Look). */
  showAgentNames: boolean
  /** Experimental: the first screen is the progress page (ui/progress.tsx), goals first. */
  progressFirst: boolean
  /** The agent the user picked on the computer to sort what they say, by name ("" before a pick). */
  progressAgent: string
  /** The phone's language as last told to the computer, where the sorting agent writes in it. */
  progressLang: string
  /** With the progress page on: which of the two the home page shows, the agent list or the progress page; the last one switched to. */
  homeView: 'progress' | 'agents'
  /** The progress page's headings the user folded, by task id. */
  progressFolded: string[]
  /** How many times the app has opened: the question about checking for new versions waits for the second. */
  opened: number }
export const DEFAULTS: Prefs = { appearance: 'system', typeIn: 'composer', fontSize: 13, alertBlocked: true, alertDone: true, stayConnected: true, dismissedUpdate: '', askedNotifications: false, showAgentNames: true, progressFirst: false, progressAgent: '', progressLang: '', homeView: 'progress', progressFolded: [], opened: 0 }
import { FONT_MAX, FONT_MIN } from './ui/terminal-page.ts'
export { FONT_MAX, FONT_MIN }

export class PrefStore {
  readonly prefs = new Signal<Prefs>(DEFAULTS)
  constructor(private io: { load: (k: string) => Promise<string | null>; save: (k: string, v: string) => void }) {}

  async load() {
    const v = await this.io.load('prefs')
    if (!v) return
    try {
      const p = JSON.parse(v)
      this.prefs.set(clean({ ...DEFAULTS, ...p }))
    } catch {}
  }

  set(change: Partial<Prefs>) {
    const p = clean({ ...this.prefs.get(), ...change })
    this.prefs.set(p)
    this.io.save('prefs', JSON.stringify(p))
  }
}

function clean(p: Prefs): Prefs {
  return {
    appearance: (['system', 'light', 'dark'] as const).includes(p.appearance) ? p.appearance : 'system',
    typeIn: p.typeIn === 'terminal' ? 'terminal' : 'composer',
    fontSize: Math.min(FONT_MAX, Math.max(FONT_MIN, Math.round(Number(p.fontSize) || DEFAULTS.fontSize))),
    alertBlocked: p.alertBlocked !== false,
    alertDone: p.alertDone !== false,
    stayConnected: p.stayConnected !== false,
    askedNotifications: p.askedNotifications === true,
    showAgentNames: p.showAgentNames !== false,
    progressFirst: p.progressFirst === true,
    progressAgent: typeof p.progressAgent === 'string' ? p.progressAgent : '',
    progressLang: typeof p.progressLang === 'string' ? p.progressLang : '',
    homeView: p.homeView === 'agents' ? 'agents' : 'progress',
    progressFolded: Array.isArray(p.progressFolded) ? p.progressFolded.filter((x): x is string => typeof x === 'string') : [],
    dismissedUpdate: typeof p.dismissedUpdate === 'string' ? p.dismissedUpdate : '',
    opened: Math.max(0, Math.floor(Number(p.opened) || 0)),
  }
}
