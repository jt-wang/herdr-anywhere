// One pane's terminal on the phone: the web page (terminal-page.ts) says when it is laid out and what was typed;
// this attaches to herdr-core at that size, sends output to the page, and attaches again (resumed) after the
// line comes back, as herdr-app's terminal view does.
import type { Store } from '../store.ts'
import type { CloseReason, Stream } from '../hc/client.ts'
import { Signal } from '../store.ts'
import type { TypeIn } from '../prefs.ts'
import { focusTerminalView } from '../page.ts'
import { PageChannel, ctrlKey, readPageMessage, xtermTheme, type BarKey } from './terminal-page.ts'
import { findSince, sinceLabel, tailOf } from '../model/seen.ts'
import { t, type Key } from '../strings/index.ts'

export type SessionState = 'waiting' | 'live' | 'ended' | 'lost' | 'failed'
/** What fingers did on the page that the app acts on. */
export type Gestures = { swipe?: (dir: 'next' | 'prev') => void; link?: (url: string) => void
  selected?: (text: string) => void; tap?: () => void; focused?: () => void }

const WHY: Record<string, Key> = {
  exited: 'programExited',
  killed: 'terminalClosed',
  lost: 'terminalLost',
  finished: 'terminalFinished',
}

/** How often a viewer in use tells herdr-core so. */
const CLAIM_MS = 2000
/** How long a page has to say ready. It takes 0.3–1.4 s on the iOS 17 simulator (2026-11-07). A page whose web process
 *  crashed never does, and the WebView says nothing about it: on the iOS 26.3 simulator JavaScriptCore crashed in about
 *  half the opens while xterm's script ran (in its string code: parseInt, String.prototype.replace). */
export const PAGE_READY_MS = 6000

export class TerminalSession {
  readonly state = new Signal<SessionState>('waiting')
  readonly why = new Signal('')
  readonly ctrl = new Signal(false)
  readonly shift = new Signal(false)
  /** Where typing goes in this pane now: the input box or the terminal (pane-bar.tsx switches it). */
  readonly typeIn = new Signal<TypeIn>('composer')
  private claimedAt = 0
  /** Scrolled back from the live bottom. */
  readonly away = new Signal(false)
  private channel: PageChannel
  private stream?: Stream
  private size?: { cols: number; rows: number }
  private attaching = false
  /** The keyboard was asked for before the page could take it (a switch tapped as the pane opens): asked again on ready. */
  private focusLater = false
  private attachedOnce = false
  private copying?: (text: string) => void
  private off: () => unknown
  private disposed = false
  // What you saw when you last looked (before this opening), the screen's last lines now, and whether this opening
  // has asked for its mark.
  private before?: { at: number; tail: string[] }
  private lines?: string[]
  private marking = false
  private dark?: boolean
  // The page's wait for ready: which one is current, whether it said ready (focus() waits for that too), and whether it
  // was already loaded again.
  private pageWait = 0
  private pageReady = false
  private reloaded = false

  constructor(private store: Store, readonly address: string, post: (m: string) => void, later?: (f: () => void, ms: number) => number,
    private opts: { observe?: boolean; on?: Gestures; reload?: () => void; pageTimer?: (f: () => void, ms: number) => unknown } = {}) {
    this.channel = new PageChannel(post, later)
    this.waitForPage()
    this.off = store.conn.sub((c) => {
      if (c === 'ready' && this.state.get() === 'lost') void this.attach()
    })
  }

  /** A message from the page. */
  page(data: string) {
    const m = readPageMessage(data)
    if (!m) return
    if (m.t === 'ready') {
      this.pageReady = true
      this.reloaded = false
      if (this.state.get() === 'failed') { this.state.set('waiting'); this.why.set('') }
      this.size = { cols: m.cols, rows: m.rows }
      this.channel.ready()
      if (this.focusLater) { this.focusLater = false; this.focus() }
      if (!this.stream) void this.attach()
      else if (!this.opts.observe) this.stream.resize(m.cols, m.rows)
    } else if (m.t === 'size') {
      this.size = { cols: m.cols, rows: m.rows }
      if (!this.opts.observe) this.stream?.resize(m.cols, m.rows)
    } else if (m.t === 'input') {
      let d = m.data
      if (this.shift.get()) {
        if (d.length === 1) d = d.toUpperCase()
        this.shift.set(false)
      }
      if (this.ctrl.get()) {
        d = ctrlKey(d)
        this.ctrl.set(false)
      }
      this.stream?.write(d)
    } else if (m.t === 'copied') {
      this.copying?.(m.text)
      this.copying = undefined
    } else if (m.t === 'swipe') this.opts.on?.swipe?.(m.dir)
    else if (m.t === 'link') this.opts.on?.link?.(m.url)
    else if (m.t === 'selected') this.opts.on?.selected?.(m.text)
    else if (m.t === 'tap') this.opts.on?.tap?.()
    else if (m.t === 'focused') this.opts.on?.focused?.()
    else if (m.t === 'touched') this.claim()
    else if (m.t === 'away') this.away.set(m.away)
    else if (m.t === 'tail') {
      // The page settled after output: the first time after the history replayed, find where the new output starts.
      this.lines = m.lines
      if (this.before && !this.marking && this.stream) { this.marking = true; this.channel.send({ t: 'text' }) }
    } else if (m.t === 'text' && this.before) {
      const i = findSince(m.lines, this.before.tail)
      if (i !== undefined) this.channel.send({ t: 'mark', line: m.base + i, label: sinceLabel(this.before.at, Date.now()) })
    }
  }

  /** The page did not load: load it again and wait once more. */
  retry() {
    this.state.set('waiting')
    this.why.set('')
    this.opts.reload?.()
    this.waitForPage()
  }

  /** Back to the live bottom. */
  bottom() { this.channel.send({ t: 'bottom' }) }
  refit() { this.channel.send({ t: 'refit' }) }

  /** The page was torn down or reloads: output waits until it is ready again. */
  pageLost() {
    this.channel.lost()
    this.waitForPage()
  }

  /** A page that does not say ready in time is loaded again once; after that the terminal says so (retry()). */
  private waitForPage() {
    const wait = ++this.pageWait
    this.pageReady = false
    ;(this.opts.pageTimer ?? setTimeout)(() => {
      if (wait !== this.pageWait || this.pageReady || this.disposed) return
      if (!this.reloaded) {
        this.reloaded = true
        this.opts.reload?.()
        this.waitForPage()
        return
      }
      this.state.set('failed')
      this.why.set(t('terminalPageFailed'))
    }, PAGE_READY_MS)
  }

  key(name: BarKey) {
    const ctrl = this.ctrl.get()
    const shift = this.shift.get()
    if (ctrl) this.ctrl.set(false)
    if (shift) this.shift.set(false)
    this.channel.send({ t: 'key', name, ...(ctrl ? { ctrl } : {}), ...(shift ? { shift } : {}) })
    this.claim()
  }
  /** The person is on this viewer: the terminal takes its size back from another one (herdr-core: the last active viewer sets it). */
  claim() {
    if (this.opts.observe || Date.now() - this.claimedAt < CLAIM_MS) return
    this.claimedAt = Date.now()
    this.stream?.focus(true)
  }
  toggleCtrl() { this.ctrl.set(!this.ctrl.get()) }
  toggleShift() { this.shift.set(!this.shift.get()) }
  paste(text: string) { this.channel.send({ t: 'paste', data: text }) }
  focus() {
    if (!this.pageReady) { this.focusLater = true; return }
    this.stream?.focus(true)
    void focusTerminalView().then(() => this.channel.send({ t: 'focus' }))
  }
  /** The page's colours, and herdr-core's answer when a program asks the terminal's colours (OpenCode picks its theme
   *  from it): a change while attached attaches again, since herdr-core takes a viewer's colours only on attach. */
  setTheme(dark: boolean) {
    const was = this.dark
    this.dark = dark
    this.channel.send({ t: 'theme', dark })
    if (was !== undefined && was !== dark && this.stream) {
      const s = this.stream
      this.stream = undefined
      s.detach()
      void this.attach()
    }
  }
  setFontSize(size: number) { this.channel.send({ t: 'fontSize', size }) }

  /** The selection, or the screen's text without one. */
  copy(): Promise<string> {
    return new Promise((resolve) => {
      this.copying = resolve
      this.channel.send({ t: 'copy' })
    })
  }

  dispose() {
    this.disposed = true
    if (this.lines && !this.opts.observe) this.store.saw(this.address, tailOf(this.lines))
    this.off()
    // A left terminal keeps the phone's width (Store.hold); one only watched never had it.
    if (this.stream && !this.opts.observe) this.store.hold(this.address, this.stream)
    else this.stream?.detach()
    this.stream = undefined
  }

  private async attach() {
    if (this.attaching || this.disposed || !this.size || !this.store.connected) return
    this.attaching = true
    try {
      const resumed = this.attachedOnce
      this.channel.reset()
      const s = await this.store.attach(this.address, this.size, {
        data: (b) => { if (!this.disposed) this.channel.write(b) },
        close: (r) => this.closed(r),
      }, { ...(resumed ? { resumed: true } : {}), ...(this.opts.observe ? { observe: true } : {}), ...(this.display() ? { display: this.display() } : {}) })
      if (this.disposed) return s.detach()
      this.stream = s
      // Attached at the phone's size, it no longer needs the attachment it left behind.
      if (!this.opts.observe) this.store.release(this.address)
      if (!resumed && !this.opts.observe) this.before = this.store.lastSeen(this.address)
      // A dropped line changes nothing about the width: the phone keeps it, in front or in a pocket, until the computer is
      // used (herdr-core gives it to the viewer used last). Left with the computer, what an agent wrote while the phone
      // was away stayed that narrow in the phone's history.
      if (resumed && !this.opts.observe) s.focus(true)
      this.attachedOnce = true
      this.state.set('live')
      this.why.set('')
    } catch (e: any) {
      const kind = e?.kind ?? ''
      if (kind === 'disconnected') this.state.set('lost')
      else {
        this.state.set('ended')
        this.why.set(kind === 'not_found' ? t('terminalLost') : kind === 'finished' ? t('terminalFinished') : String(e?.message ?? e))
      }
    } finally {
      this.attaching = false
    }
  }

  private display() {
    if (this.dark === undefined) return undefined
    const th = xtermTheme(this.dark)
    return { fg: th.foreground, bg: th.background, cursor: th.cursor }
  }

  private closed(r: CloseReason) {
    // Only this session detaches, and it lets go of the stream first: a late close must not drop the next one.
    if (r.reason === 'detached') return
    this.stream = undefined
    if (this.disposed) return this.store.release(this.address)
    if (r.reason === 'disconnected') {
      this.state.set('lost')
      this.why.set(t('reconnecting'))
      return
    }
    this.state.set('ended')
    this.why.set(WHY[r.reason] ? t(WHY[r.reason]) : t('terminalEnded', { reason: r.reason }))
  }
}
