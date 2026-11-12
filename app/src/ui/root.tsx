// The app: pairing on first launch, then home ⇄ space ⇄ settings over one live store, with alerts and sheets
// over every screen.
import { cleanup, colorScheme, computed, createAnimatedValue, effect, Keyboard, platform, safeAreaInsets, setColorScheme, signal, Storage, Text, Video, View } from 'nsh'
import { createNavStack, NavPage, NavStack } from 'nsh/base'
import type { ViewSwitchProps } from './view-switch.tsx'
import type { Store } from '../store.ts'
import { chatApi, fileApi, folderApi, imageApi, parseInvite, type Link, type Linker, type Tunnel } from '../link.ts'
import type { PrefStore } from '../prefs.ts'
import { Alerts, type AlertKind } from '../alerts.ts'
import { followInstallFailures, installApp, allowNotifications, followPage, keepAlive, notifications, notifyFromPage, wantsKeepAlive } from '../page.ts'
import { HomeScreen } from './home.tsx'
import { ProgressData, ProgressHome, TaskScreen } from './progress.tsx'
import { xtermTheme } from './terminal-page.ts'
import { SpaceScreen } from './space.tsx'
import { SettingsScreen } from './settings.tsx'
import { PairScreen } from './pair.tsx'
import { NewSheet, RenameSheet, type NewTarget, type RenameTarget } from './sheets.tsx'
import { PeekSheet } from './peek.tsx'
import { PictureSheet } from './picture-sheet.tsx'
import { MenuSheet } from './menu-sheet.tsx'
import { toastTop } from './toast-top.ts'
import { makeDemo, type Demo } from '../demo.ts'
import { useSignal } from './kit.tsx'
import { APP_VERSION, installProblem, Updates } from '../updates.ts'
import { CHANNEL } from '../channel.ts'
import { t } from '../strings/index.ts'
import { restartFor } from '../restart.ts'

export type RootDeps = {
  linker: Linker
  prefs: PrefStore
  makeStore: (linker: Linker) => Store
  device: string
  fetcher?: typeof fetch
  /** The address of the web page the app was opened at (a `#pair=` link pairs by itself). */
  pairLink?: string
  /** The web page's origin: the computer that serves it, so the code alone pairs with it. */
  origin?: string
  /** After that link paired: it leaves the page's address. */
  pairLinkUsed?: () => void
  /** Opens on Settings: for the iOS e2e, since a script cannot tap the simulator. */
  opensSettings?: boolean
  /** TailPocket, in the phone apps: pairing by the invite `serve --tailcat` shows, and the tunnel the Linker uses. */
  tunnel?: Tunnel
}

type Route = { name: 'home' } | { name: 'space'; id: string } | { name: 'settings' } | { name: 'progress' } | { name: 'task'; id: string }

/** Points up, or points a second up at the release, past which a pull on a toast takes it away. */
const SWIPE_AWAY = 40, FLICK = 1000

function Toasts(props: { store: Store; open: (terminal: string) => void }) {
  const toasts = useSignal(props.store.toasts)
  const insets = safeAreaInsets()
  return (
    <View class="absolute left-3 right-3 gap-2" style={{ top: toastTop(platform.os, insets().top) }} pointerEvents="box-none">
      <Each of={toasts()} key={(t) => t.id}>{(t) => {
        // Swiped up it goes, as the system's banner does, without doing what it offers; a shorter pull springs back.
        const y = createAnimatedValue(0)
        const swiped = (e: { cancelled: boolean; translationY: number; velocityY: number }) => {
          if (!e.cancelled && (e.translationY < -SWIPE_AWAY || e.velocityY < -FLICK)) void y.timing(-200, { duration: 180 }).then(() => props.store.dismiss(t().id))
          else void y.spring(0)
        }
        return (
        <View role="button" testID={`toast-${t().id}`} onPress={() => { props.store.dismiss(t().id); if (t().action?.terminal) props.open(t().action!.terminal!); t().action?.run?.() }}
          pan={{ y }} panActivation={{ activeOffsetY: 10, failOffsetX: 20 }} onPanEnd={swiped} style={{ transform: [{ translateY: y }] }}
          class="flex-row items-center gap-2.5 rounded-[12px] bg-popover border border-border px-3.5 py-2.5 shadow-md">
          <View class={`w-2 h-2 rounded-full ${t().level === 'error' ? 'bg-chart-2' : 'bg-chart-3'}`} />
          <View class="flex-1">
            <Text class="text-[14px] font-medium text-foreground" numberOfLines={1}>{t().title}</Text>
            <If when={t().body}><Text class="text-[12px] text-muted-foreground" numberOfLines={3}>{t().body}</Text></If>
          </View>
        </View>
        )
      }}</Each>
    </View>
  )
}

// Herdr's sounds (assets/sounds): the app's own files on a phone (android/build.sh ships them), and data: URIs in a
// browser, since a web package ships only imported files and a TSX bundle can't import an .mp3 (scripts/sounds-data.sh).
const SOUND_FILES: Record<AlertKind, string> = { blocked: 'asset:/assets/sounds/request.mp3', done: 'asset:/assets/sounds/done.mp3' }

/** Each sound playing now, as a player nobody sees; it goes when it ends. */
function AlertSounds(props: { alerts: Alerts }) {
  const sounds = useSignal(props.alerts.sounds)
  const src = signal<Record<AlertKind, string> | null>(platform.os === 'web' ? null : SOUND_FILES)
  if (platform.os === 'web') void import('./sounds-data.ts').then((m) => src.set({ blocked: m.BLOCKED, done: m.DONE }))
  return (
    <Each of={src() ? sounds() : []} key={(s) => s.id}>{(s) => (
      <Video testID={`sound-${s().kind}`} src={src()![s().kind]} pointerEvents="none" accessibilityElementsHidden
        style={{ position: 'absolute', width: 1, height: 1, opacity: 0 }}
        onEnd={() => props.alerts.ended(s().id)} onError={() => props.alerts.ended(s().id)} />
    )}</Each>
  )
}

function Connected(props: { deps: RootDeps; store: Store; link: Link; unpaired: () => void; leaveDemo?: () => void }) {
  const { store, deps } = props
  const startOn = props.deps.prefs.prefs.get()
  const first: Route = startOn.progressFirst && startOn.homeView !== 'agents' ? { name: 'progress' } : { name: 'home' }
  const nav = createNavStack<Route>(props.deps.opensSettings ? [first, { name: 'settings' }] : [first])
  const progress = new ProgressData(store, 4000, { load: () => Storage.getItem('progress-alerts'), save: (v) => void Storage.setItem('progress-alerts', v).catch(() => {}) })
  progress.told = { get: () => deps.prefs.prefs.get().progressLang, set: (l) => deps.prefs.set({ progressLang: l }) }
  const newTarget = signal<NewTarget | null>(null)
  const renaming = signal<RenameTarget | null>(null)
  const peeking = signal<string | null>(null)
  const folders = folderApi(deps.linker, deps.fetcher)
  store.files = fileApi(deps.linker, deps.fetcher)
  store.chat = chatApi(deps.linker, deps.fetcher)
  store.chatFirst = { get: () => deps.prefs.prefs.get().chatFirst, set: (v) => deps.prefs.set({ chatFirst: v }) }
  store.chatSetupAsked = {
    has: (agent) => deps.prefs.prefs.get().chatSetupAsked.includes(agent),
    add: (agent) => { const was = deps.prefs.prefs.get().chatSetupAsked; if (!was.includes(agent)) deps.prefs.set({ chatSetupAsked: [...was, agent] }) },
  }
  store.images = imageApi(deps.linker, deps.fetcher)
  const updates = new Updates({
    baseUrl: () => deps.linker.baseUrl(), token: () => deps.linker.link.get()?.token ?? '', conn: store.conn, fetcher: deps.fetcher ?? fetch,
    os: platform.os, channel: CHANNEL, appVersion: APP_VERSION, toast: (t) => store.toast(t), install: installApp,
  })
  updates.start()
  const offInstall = followInstallFailures((m) => store.toast({ title: t('couldntUpdateApp'), body: installProblem(m), level: 'error' }))
  cleanup(() => { updates.stop(); offInstall() })
  const prefs = useSignal(deps.prefs.prefs)
  store.showNames = () => prefs().showAgentNames
  store.wants = (k) => (k === 'blocked' ? deps.prefs.prefs.get().alertBlocked : deps.prefs.prefs.get().alertDone)
  // The page (web-host.js) or the Android activity says whether the app is in front; on iOS it is while it runs.
  const alerts = new Alerts({ notify: (t, kind, quiet) => notifyFromPage({ title: t.title, body: t.body, terminal: t.action?.terminal, kind, quiet }), soundAway: platform.os !== 'android' })
  // With the progress page on, a pane working on a task rings through its task (the tasks above › the task, once per change).
  store.onAlert = (kind, toast, again) => {
    if (deps.prefs.prefs.get().progressFirst && progress.covers(toast.action?.terminal)) return
    alerts.ring(kind, toast, store.appActive, again)
  }
  progress.onAlert = (a) => {
    const terminal = progress.paneFor(a.task)
    alerts.ring(a.kind === 'wait' ? 'blocked' : 'done', { id: 0, title: a.title, body: a.body, level: 'info', action: terminal ? { terminal } : undefined }, store.appActive)
  }
  const offPage = followPage({ front: (f) => { store.appActive = f; if (f) void updates.inFront() }, open: (a) => openTerminal(a) })
  const offConn = store.conn.sub((c) => { if (c === 'unpaired') props.unpaired() })
  const conn = useSignal(store.conn)
  effect(() => keepAlive(wantsKeepAlive(conn(), prefs().stayConnected)))
  // A program started from the phone asks the terminal's colours as it starts (OpenCode picks light or dark from them).
  effect(() => { const th = xtermTheme(colorScheme() === 'dark'); store.display = { fg: th.foreground, bg: th.background, cursor: th.cursor } })
  // Android 13 and later post nothing until this is allowed, and an agent's question is why the app has notifications.
  // A browser asks only from a tap, so there Settings has the button. The sample data has nobody to hear from.
  effect(() => {
    if (conn() !== 'ready' || platform.os === 'web' || props.leaveDemo || prefs().askedNotifications) return
    deps.prefs.set({ askedNotifications: true })
    void notifications().then((n) => { if (n === 'default') void allowNotifications() })
  })
  cleanup(() => { offConn(); offPage(); store.disconnect() })
  void store.connect().catch(() => {})

  // Another screen has no use for the keyboard the composer brought up, and Settings would sit under it.
  let depth = nav.routes().length
  effect(() => {
    const now = nav.routes().length
    if (now !== depth) { depth = now; Keyboard.dismiss() }
  })

  const openSpace = (id: string) => {
    store.selectSpace(id)
    const top = nav.routes().at(-1)
    if (top?.name === 'space' && top.id === id) return
    nav.popToRoot()
    nav.push({ name: 'space', id })
  }
  const openTerminal = (addr: string) => {
    if (!store.focusTerminal(addr)) return peeking.set(addr)
    const sp = store.model.activeSpace()
    if (sp) openSpace(sp.id)
  }
  // With the progress page on, the home page is one of two, switched at the top: the switch replaces the page, so back
  // never goes from one to the other, and the app opens on the last one.
  const view = (): ViewSwitchProps | undefined => {
    if (!prefs().progressFirst) return undefined
    return {
      current: nav.routes()[0]?.name === 'progress' ? 'progress' : 'agents',
      switchTo: (v) => { deps.prefs.set({ homeView: v }); nav.setRoutes([v === 'progress' ? { name: 'progress' } : { name: 'home' }]) },
    }
  }
  // Turned off while the progress page is the bottom page: the agent list takes its place.
  effect(() => {
    const r = nav.routes()
    if (!prefs().progressFirst && r[0]?.name === 'progress') nav.setRoutes([{ name: 'home' }, ...r.slice(1).filter((x) => x.name !== 'progress' && x.name !== 'task')])
  })
  const pactions = {
    open: (v: { name: 'task'; id: string } | { name: 'home' }) => { if (v.name === 'home') nav.popToRoot(); else nav.push(v) },
    back: () => nav.pop(), openPane: (a: string) => openTerminal(a), view: () => view() ?? { current: 'progress' as const, switchTo: () => {} }, settings: () => nav.push({ name: 'settings' }),
    folded: () => prefs().progressFolded,
    fold: (id: string) => { const f = prefs().progressFolded; deps.prefs.set({ progressFolded: f.includes(id) ? f.filter((x) => x !== id) : [...f, id] }) },
  }
  const progressPage = (r: Route, body: any) => (
    <NavPage headerVisible={false} class="bg-sidebar">
      {/* Below the status bar and above the home bar, as the agent list is: the switch stays put between the two. */}
      <View class="flex-1 pt-[env(safe-area-inset-top)] pb-[env(safe-area-inset-bottom)]">
        {body}
      </View>
    </NavPage>
  )
  const page = (r: Route) =>
    r.name === 'progress' ? progressPage(r, <ProgressHome data={progress} actions={pactions} />)
    : r.name === 'task' ? progressPage(r, <TaskScreen data={progress} id={r.id} actions={pactions} />)
    : r.name === 'space'
      ? <NavPage headerVisible={false} class="bg-sidebar">
          <SpaceScreen store={store} spaceId={r.id} actions={{
            back: () => nav.pop(), newTab: (space) => newTarget.set({ kind: 'tab', space }), addAgent: (space) => newTarget.set({ kind: 'tab', space, agent: true }),
            rename: (kind, id, current) => renaming.set({ kind, id, current }), fontSize: () => prefs().fontSize,
            typeIn: () => deps.prefs.prefs.get().typeIn, keepTypeIn: (m) => deps.prefs.set({ typeIn: m }),
            open: (a) => openTerminal(a), settings: () => nav.push({ name: 'settings' }),
          }} />
        </NavPage>
      : r.name === 'settings'
      ? <NavPage headerVisible={false} class="bg-sidebar">
          <SettingsScreen updates={updates} store={store} prefs={deps.prefs} link={props.link} linker={deps.linker} fetcher={deps.fetcher} back={() => nav.pop()} forget={() => { if (props.leaveDemo) props.leaveDemo(); else deps.linker.forget() }} />
        </NavPage>
      : <NavPage headerVisible={false} class="bg-sidebar">
          <HomeScreen updates={updates} prefs={deps.prefs} store={store} actions={{
            openSpace, openAgent: (a) => openTerminal(a.address), peek: (addr) => peeking.set(addr),
            addAgent: (space) => newTarget.set({ kind: 'tab', space, agent: true }), newMenu: () => newTarget.set({ kind: 'space' }), settings: () => nav.push({ name: 'settings' }),
            rename: (kind, id, current) => renaming.set({ kind, id, current }), leaveDemo: props.leaveDemo,
            doing: (addr) => (deps.prefs.prefs.get().progressFirst ? progress.taskOn(addr) : undefined),
            view,
          }} />
        </NavPage>
  return (
    <View class="flex-1 bg-sidebar">
      <NavStack state={nav} class="flex-1">{(r) => page(r)}</NavStack>
      <NewSheet store={store} target={newTarget} folders={folders} onStarted={(r) => openSpace(r.space)} />
      <RenameSheet store={store} target={renaming} />
      <PeekSheet store={store} address={peeking} fontSize={() => prefs().fontSize} />
      <PictureSheet store={store} />
      <MenuSheet />
      <Toasts store={store} open={openTerminal} />
      <AlertSounds alerts={alerts} />
    </View>
  )
}

export function Root(props: { deps: RootDeps }) {
  const { deps } = props
  const loaded = signal(false)
  const link = useSignal(deps.linker.link)
  const prefs = useSignal(deps.prefs.prefs)
  const unpairedMessage = signal('')
  // Pairing again from the banner: the link stays until a new one pairs, and Back returns to it.
  const repairing = signal(false)
  const demo = signal<Demo | null>(null)
  const leaveDemo = () => { demo()?.stop(); demo.set(null) }
  effect(() => setColorScheme(prefs().appearance))
  Promise.all([deps.linker.load(), deps.prefs.load().then(() => deps.prefs.set({ opened: deps.prefs.prefs.get().opened + 1 }))]).finally(() => loaded.set(true))
  // A store per link: pairing again with another computer starts over.
  let made: { key: string; store: Store } | undefined
  const store = computed(() => {
    const l = link()
    if (!l) return (made = undefined)
    const key = `${l.url} ${l.token}`
    if (made?.key !== key) made = { key, store: deps.makeStore(deps.linker) }
    made.store.pairAgain = () => repairing.set(true)
    made.store.restart = restartFor(l)
    return made.store
  })
  const opened = deps.pairLink
  // A link pairs once: after that, pairing again starts from an empty field. The app may open with a link while it is
  // paired already (e2e/ios-tailcat.sh's build opens with its invite every launch): forgotten then, it pairs again only
  // when the person asks.
  const pairInitial = signal(opened && (opened.includes('#pair=') || parseInvite(opened)) ? opened : undefined)
  return (
    <If when={loaded()} else={<View class="flex-1 bg-background" />}>
      <If when={!demo()} else={
        <Each of={demo() ? [demo()!] : []}>{(d) => (
          <Connected deps={{ ...deps, linker: d().linker, fetcher: d().fetcher, opensSettings: false }} store={d().store} link={d().link} unpaired={leaveDemo} leaveDemo={leaveDemo} />
        )}</Each>
      }>
      <If when={link() && !unpairedMessage() && !repairing()} else={
        <PairScreen device={deps.device} fetcher={deps.fetcher} origin={deps.origin} tunnel={deps.tunnel} message={unpairedMessage()} initial={pairInitial()} onTry={repairing() ? undefined : () => demo.set(makeDemo())}
          onBack={repairing() ? () => repairing.set(false) : undefined}
          onPaired={(l) => {
            if (pairInitial()) { pairInitial.set(undefined); deps.pairLinkUsed?.() }
            unpairedMessage.set('')
            repairing.set(false)
            deps.linker.set(l)
          }} />
      }>
        <Each of={store() ? [store()!] : []}>{(s) => (
          <Connected deps={deps} store={s()} link={link()!} unpaired={() => { pairInitial.set(undefined); unpairedMessage.set(t('forgotten')) }} />
        )}</Each>
      </If>
      </If>
    </If>
  )
}
