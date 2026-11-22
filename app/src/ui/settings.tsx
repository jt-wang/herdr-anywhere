// Settings: how the app looks, which alerts show, and the computer it is paired with.
import { cleanup, platform, Scroll, signal, Text, View } from 'nsh'
import { AlertDialog } from 'nsh/base'
import { explain, type Store } from '../store.ts'
import { setupChangedText, type ChatSetup } from '../model/chat.ts'
import { agentsLine, parseAgents, pickLine, switchLine, type AgentChoice } from '../model/progress.ts'
import { busyWord, runUpdate, useUpdates } from './update.tsx'
import { APP_VERSION, type Updates } from '../updates.ts'
import { companionVersion, type Link, type Linker, type Route } from '../link.ts'
import { CONNECTIONS, FONT_MAX, FONT_MIN, type Appearance, type Connection, type PrefStore } from '../prefs.ts'
import { allowNotifications, notifications, notifyChannels, onFront, openChannel, type ChannelKind, type Channels, type ChannelState, type Permission } from '../page.ts'
import { connStatus, Glyph, IconButton, openPrivacy, SectionLabel, useRev, useSignal } from './kit.tsx'
import { LANG_NAMES, lang, langName, systemLang, t, type Key, type Lang } from '../strings/index.ts'
import { diagnostics, type DiagnosticCategory, type DiagnosticStatus, type NativeDiagnostics } from '../diag.ts'

function Row(props: { label: string; detail?: string; children?: any; testID?: string; onPress?: () => void }) {
  return (
    <View role={props.onPress ? 'button' : undefined} testID={props.testID} onPress={props.onPress}
      class={`flex-row items-center min-h-12 px-4 gap-3 border-b border-border bg-card ${props.onPress ? 'active:bg-sidebar-accent' : ''}`}>
      <View class="flex-1 py-2">
        <Text class="text-[14px] text-foreground">{props.label}</Text>
        <If when={props.detail}><Text class="text-[12px] text-sidebar-foreground mt-0.5">{props.detail}</Text></If>
      </View>
      {props.children}
    </View>
  )
}

/** A fact about the computer: its name on the left, its value on the right, with a smaller line under it if there is one. */
function Fact(props: { label: string; value: string; under?: string; dot?: string; testID?: string }) {
  return (
    <View testID={props.testID} class={`flex-row ${props.under ? 'items-start' : 'items-center'} min-h-12 px-4 py-2.5 gap-3 border-b border-border bg-card`}>
      <Text class="text-[14px] text-foreground">{props.label}</Text>
      <View class="flex-1 items-end gap-0.5">
        <View class="flex-row items-center gap-1.5">
          <If when={props.dot}><View class={`w-2 h-2 rounded-full ${props.dot}`} /></If>
          <Text class="text-[14px] text-sidebar-foreground text-right">{props.value}</Text>
        </View>
        <If when={props.under}><Text class="text-[12px] text-sidebar-foreground text-right">{props.under}</Text></If>
      </View>
    </View>
  )
}

/** One row per agent on the computer whose chat needs a small file (ChatSetup): on or off, Install or Remove at any
 *  time. Remove asks first; a file someone changed is replaced or removed only when the person says so. */
function ChatSetupRows(props: { store: Store }) {
  const chat = props.store.chat
  const setups = signal<ChatSetup[]>([])
  const busy = signal('')
  const load = () => { if (chat) void chat.setups().then((x) => setups.set(x), () => {}) }
  load()
  const put = (x: ChatSetup) => setups.set(setups().map((y) => (y.agent === x.agent ? x : y)))
  const set = async (x: ChatSetup, on: boolean) => {
    if (!on) {
      const r = await AlertDialog.show({ title: t('setupRemoveTitle', { name: x.name }), description: t('setupRemoveBody', { name: x.name }),
        actions: [{ id: 'cancel', label: t('cancel'), role: 'cancel' }, { id: 'remove', label: t('setupRemove'), role: 'destructive' }] })
      if (r !== 'remove') return
    }
    busy.set(x.agent)
    try {
      let r = await chat!.setSetup(x.agent, on)
      while (r.result === 'changed') {
        const current = r.setup ?? x
        const a = await AlertDialog.show({ title: t('setupChangedTitle'), description: setupChangedText(current),
          actions: [{ id: 'keep', label: t('setupKeep'), role: 'cancel' }, { id: 'go', label: on ? t('setupReplace') : t('setupRemove'), role: on ? undefined : 'destructive' }] })
        if (a !== 'go') break
        r = await chat!.setSetup(x.agent, on, true, current.confirmation)
      }
      if (r.setup) put(r.setup)
      if (r.result === 'ok') props.store.chatSetupChanged(x.agent)
      if (r.result === 'failed') props.store.toast({ title: t('setupFailed'), level: 'error' })
    } catch {
      props.store.toast({ title: t('setupFailed'), level: 'error' })
    } finally {
      busy.set('')
    }
  }
  const shown = () => setups().filter((x) => x.state !== 'no-agent')
  const detail = (x: ChatSetup) => (x.state === 'installed' || x.state === 'old' ? t('setupOn') : x.state === 'changed' ? t('setupChanged') : t('setupOff', { name: x.name }))
  return (
    <If when={shown().length}>
      <SectionLabel label={t('chatSection')} />
      <Each of={shown()} key={(x) => x.agent}>{(x) => (
        <Row label={t('setupRow', { name: x().name })} detail={detail(x())} testID={`setup-${x().agent}`}>
          <If when={x().state === 'absent' || x().state === 'old' || x().state === 'changed'}>
            <View role="button" testID={`setup-${x().agent}-install`} disabled={busy() === x().agent} onPress={() => void set(x(), true)} class="px-2.5 py-1 rounded-[6px] bg-muted active:bg-accent">
              <Text class="text-[12px] text-foreground">{t('setupInstall')}</Text>
            </View>
          </If>
          <If when={x().state !== 'absent'}>
            <View role="button" testID={`setup-${x().agent}-remove`} disabled={busy() === x().agent} onPress={() => void set(x(), false)} class="px-2.5 py-1 rounded-[6px] bg-muted active:bg-accent">
              <Text class="text-[12px] text-destructive">{t('setupRemove')}</Text>
            </View>
          </If>
        </Row>
      )}</Each>
    </If>
  )
}

/** A Tailscale name, or an address in the range Tailscale gives out (100.64.0.0/10, which carriers use too). */
const maybeTailnet = (host: string) => /^100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\./.test(host) || /\.ts\.net(:|$)/.test(host)

/** How often the route is asked again: a direct path can come a little after the relay. */
const ROUTE_EVERY_MS = 30_000

/** A new version, if there is one; whether the computer installs them by itself; which version this app is. */
function UpdateRows(props: { updates: Updates }) {
  const state = useUpdates(props.updates)
  const notice = () => state().notice
  const busy = () => state().busy
  const label = () => (notice()?.kind === 'app' || notice()?.kind === 'rebuild' ? t('updateApp') : t('updateComputer'))
  const checking = useSignal(props.updates.checking)
  const checked = useSignal(props.updates.checked)
  const checkedWord = () => {
    if (checking()) return t('checking')
    const c = checked()
    if (!c) return undefined
    if ('error' in c) return t('couldntCheck', { error: c.error })
    return notice() ? undefined : t('upToDate')
  }
  return (
    <View>
      <If when={busy() || notice()}>
        <Row label={label()} detail={busy() ? busyWord(busy(), state().progress) : t('updateAvailable', { version: notice()?.version ?? '' })}>
          <If when={!busy()}>
            <View role="button" testID="settings-update" onPress={() => void runUpdate(props.updates, notice()!)} class="px-2.5 py-1 rounded-[6px] bg-muted active:bg-accent">
              <Text class="text-[12px] text-foreground">{t('updateNow')}</Text>
            </View>
          </If>
        </Row>
      </If>
      <If when={state().info?.checks}>
        <Row label={t('checksDaily')} detail={t('checksDailyDetail')}>
          <Toggle label={t('checksDaily')} testID="update-checks" on={state().info?.checks === 'on'} onChange={(on) => void props.updates.setChecks(on)} />
        </Row>
        <Row label={t('newVersions')} detail={checkedWord()}>
          <View role="button" testID="update-check" onPress={() => void props.updates.checkNow()} class="px-2.5 py-1 rounded-[6px] bg-muted active:bg-accent">
            <Text class="text-[12px] text-foreground">{t('checkNow')}</Text>
          </View>
        </Row>
      </If>
      <If when={state().info?.canUpdate && state().info?.checks !== 'off' && state().info?.checks !== 'ask'}>
        <Row label={t('autoUpdateComputer')} detail={t('autoUpdateDetail')}>
          <Toggle label={t('autoUpdateComputer')} testID="auto-update" on={state().info?.autoUpdate === true} onChange={(on) => void props.updates.setAuto(on)} />
        </Row>
      </If>
      <If when={platform.os !== 'web'}><Fact label={t('appVersion')} value={APP_VERSION} testID="app-version" /></If>
    </View>
  )
}

const CHANNEL_WORD: Record<ChannelState, Key> = { alert: 'channelAlert', sound: 'channelSound', silent: 'channelSilent', off: 'channelOff' }

/** Under an alert's switch on Android: what Android does with that kind now; a tap opens its page there, where the
 *  person picks the sound, the pop-up or silence (an app can't change a channel once it exists). */
function ChannelLine(props: { kind: ChannelKind; label: string; state: ChannelState }) {
  return (
    <View role="button" testID={`channel-${props.kind}`} accessibilityLabel={`${props.label}: ${t(CHANNEL_WORD[props.state])}`} onPress={() => openChannel(props.kind)}
      class="flex-row items-center min-h-10 pl-4 pr-3 gap-2 border-b border-border bg-card active:bg-sidebar-accent">
      <Text class="flex-1 text-[12px] text-foreground">{t(CHANNEL_WORD[props.state])}</Text>
      <Text class="text-[12px] text-sidebar-foreground">{t('androidSettings')}</Text>
      <Glyph name="chevron-right" size={14} />
    </View>
  )
}

function Toggle(props: { on: boolean; disabled?: boolean; label: string; testID: string; onChange: (on: boolean) => void }) {
  return (
    <View role="switch" accessibilityLabel={props.label} accessibilityState={{ checked: props.on, disabled: props.disabled }} disabled={props.disabled} testID={props.testID} onPress={() => { if (!props.disabled) props.onChange(!props.on) }}
      class={`w-11 h-[26px] rounded-full p-[3px] ${props.on ? 'bg-chart-3 items-end' : 'bg-input items-start'} ${props.disabled ? 'opacity-40' : ''}`}>
      <View class="w-5 h-5 rounded-full bg-white" />
    </View>
  )
}

/** Native owns consent, bounded retention and sharing; unsupported hosts never show a usable switch. */
export function DiagnosticsScreen(props: { back: () => void; controller?: NativeDiagnostics; changed?: (s: DiagnosticStatus) => void }) {
  const controller = props.controller ?? diagnostics
  const state = signal(controller.status)
  const busy = signal(false)
  const note = signal('')
  const update = (s: DiagnosticStatus) => { state.set(s); props.changed?.(s) }
  void controller.refresh().then(update)
  const run = async (action: 'toggle' | 'export' | 'clear' | DiagnosticCategory) => {
    if (busy() || !state().supported) return
    if (action !== 'toggle' && action !== 'export' && action !== 'clear' && !state().enabled) return
    busy.set(true); note.set('')
    try {
      if (action === 'toggle') {
        const wanted = !state().enabled
        const next = await controller.set(wanted)
        update(next)
        if (!next.supported || next.enabled !== wanted) note.set(t('diagnosticFailed'))
      } else if (action === 'clear') {
        note.set(t(await controller.clear() ? 'diagnosticCleared' : 'diagnosticFailed'))
      } else if (action === 'export') {
        await controller.export() // Cancel is not a failure and must not claim the report was shared.
      } else {
        const wanted = !state().categories[action]
        const next = await controller.setCategories({ [action]: wanted })
        update(next)
        if (!next.supported || next.categories[action] !== wanted) note.set(t('diagnosticFailed'))
      }
    } catch { note.set(t('diagnosticFailed')) }
    finally { busy.set(false) }
  }
  const actions: { id: 'export' | 'clear'; label: Key }[] = [
    { id: 'export', label: 'diagnosticExport' }, { id: 'clear', label: 'diagnosticClear' },
  ]
  const categories: { id: DiagnosticCategory; label: Key }[] = [
    { id: 'interaction', label: 'diagnosticInteraction' }, { id: 'performance', label: 'diagnosticPerformance' },
    { id: 'network', label: 'diagnosticNetwork' }, { id: 'resources', label: 'diagnosticResources' },
  ]
  return (
    <View class="flex-1 bg-sidebar pt-[env(safe-area-inset-top)]">
      <View class="flex-row items-center h-12 pl-1 pr-3">
        <IconButton name="chevron-left" label={t('back')} testID="diagnostic-back" onPress={props.back} />
        <Text class="text-[16px] font-semibold text-foreground ml-1">{t('diagnosticTitle')}</Text>
      </View>
      <Scroll class="flex-1" contentClass="pb-10">
        <If when={state().supported} else={<Row label={t('diagnosticTitle')} detail={t('diagnosticUnsupported')} testID="diagnostic-unsupported" />}>
          <Row label={t('diagnosticRecord')} detail={t('diagnosticInstructions')}>
            <Toggle label={t('diagnosticRecord')} testID="diagnostic-toggle" on={state().enabled} disabled={busy()} onChange={() => void run('toggle')} />
          </Row>
          <Text testID="diagnostic-recording" class={`px-4 py-2 text-[12px] ${state().enabled ? 'text-destructive' : 'text-sidebar-foreground'}`}>
            {t(!state().enabled ? 'diagnosticOff' : Object.values(state().categories).some(Boolean) ? 'diagnosticRecording' : 'diagnosticNoCategories')}
          </Text>
          <Each of={categories} key={(c) => c.id}>{(c) => (
            <Row label={t(c().label)}>
              <Text class={`text-[13px] text-sidebar-foreground ${!state().enabled ? 'opacity-40' : ''}`}>{t(state().enabled && state().categories[c().id] ? 'diagnosticOnState' : 'diagnosticOffState')}</Text>
              <Toggle label={t(c().label)} testID={`diagnostic-${c().id}`} on={state().enabled && state().categories[c().id]} disabled={busy() || !state().enabled} onChange={() => void run(c().id)} />
            </Row>
          )}</Each>
          <View class="px-4 py-2"><Text class="text-[12px] leading-[17px] text-sidebar-foreground">{t('diagnosticContent')}</Text></View>
          <View class="px-4 py-2"><Text class="text-[12px] leading-[17px] text-sidebar-foreground">{t('diagnosticPrivacy')}</Text></View>
          <Each of={actions} key={(a) => a.id}>{(a) => (
            <Row label={t(a().label)}>
              <View role="button" testID={`diagnostic-${a().id}`} disabled={busy()} onPress={() => void run(a().id)} class="px-2.5 py-1 rounded-[6px] bg-muted active:bg-accent">
                <Text class="text-[12px] text-foreground">{t(a().label)}</Text>
              </View>
            </Row>
          )}</Each>
          <If when={note()}><Text testID="diagnostic-note" class="px-4 py-2 text-[12px] text-sidebar-foreground">{note()}</Text></If>
        </If>
      </Scroll>
    </View>
  )
}

/** Settings › Language: Follow system, which says the language it reads in now, or one of the app's seven, each
 *  named in its own words. A pick shows at once; the screen stays, now in that language. */
export function LanguageScreen(props: { prefs: PrefStore; back: () => void; choose: (l: Lang | '') => void }) {
  const p = useSignal(props.prefs.prefs)
  const options = (): { id: Lang | ''; label: string; detail?: string }[] => [
    { id: '', label: t('followSystem'), detail: langName(systemLang()) },
    ...LANG_NAMES.map(([id, label]) => ({ id, label })),
  ]
  return (
    <View class="flex-1 bg-sidebar pt-[env(safe-area-inset-top)]">
      <View class="flex-row items-center h-12 pl-1 pr-3">
        <IconButton name="chevron-left" label={t('back')} testID="language-back" onPress={props.back} />
        <Text class="text-[16px] font-semibold text-foreground ml-1">{t('language')}</Text>
      </View>
      <Scroll class="flex-1" contentClass="pb-10">
        <View class="mt-3 border-t border-border">
          <Each of={options()} key={(o) => o.id || 'system'}>{(o) => (
            <View role="button" testID={`language-${o().id || 'system'}`} accessibilityState={{ selected: p().language === o().id }} onPress={() => props.choose(o().id)}
              class="flex-row items-center min-h-12 px-4 gap-3 border-b border-border bg-card active:bg-sidebar-accent">
              <View class="flex-1 py-2">
                <Text class="text-[14px] text-foreground">{o().label}</Text>
                <If when={o().detail}><Text class="text-[12px] text-sidebar-foreground mt-0.5">{o().detail}</Text></If>
              </View>
              <If when={p().language === o().id}><Glyph name="check" size={16} class="tint-chart-3" /></If>
            </View>
          )}</Each>
        </View>
      </Scroll>
    </View>
  )
}

/** The connection's state and the path TailPocket measures, asked again every ROUTE_EVERY_MS while it is up and the app
 *  is in front; `ready` runs too each time it asks. */
function useConnection(store: Store, linker: Linker, ready?: () => void) {
  const conn = useSignal(store.conn)
  const route = signal<Route | null>(null)
  const ask = () => {
    if (conn() !== 'ready' || !store.appActive) return
    void linker.path().then((r) => route.set(r))
    ready?.()
  }
  let asking: ReturnType<typeof setInterval> | undefined
  const resume = () => {
    clearInterval(asking)
    asking = undefined
    if (!store.appActive || store.conn.get() !== 'ready') { if (store.conn.get() !== 'ready') route.set(null); return }
    ask()
    asking = setInterval(ask, ROUTE_EVERY_MS)
  }
  resume()
  const offConn = store.conn.sub(resume, 'Settings.ask')
  const offActive = store.activity.sub(resume)
  cleanup(() => { clearInterval(asking); offConn(); offActive() })
  const why = useSignal(store.lostWhy)
  const status = () => connStatus(conn(), why())
  return { conn, route, status }
}

/** The path a TailPocket tunnel takes now, as words; '' before it is measured. */
function pathWord(r: Route | null) {
  if (!r) return ''
  const kind = r.kind || r.route
  if (kind === 'direct') return t('pathDirect')
  if (kind === 'lan') return t('pathLan')
  if (kind === 'derp' || kind === 'relay') {
    // Only a region code is uppercased, never a DERP or custom relay hostname. Legacy hosts only have `relay`.
    const region = r.kind === 'derp' ? r.derpCode?.toUpperCase() || r.derpHost : r.kind === 'relay' ? r.relayHost : r.relay?.toUpperCase()
    return region ? t('pathRelay', { region }) : t('pathRelayPlain')
  }
  return t('pathUnknown')
}

const CONNECTION_TEXT: Record<Connection, [Key, Key]> = {
  auto: ['connectionAuto', 'connectionAutoDetail'], lan: ['connectionLan', 'connectionLanDetail'],
  notailcat: ['connectionNoTailcat', 'connectionNoTailcatDetail'], tailcat: ['connectionTailcat', 'connectionTailcatDetail'],
}

/** Settings › Connection: the routes TailPocket may use to reach the computer, what each costs, and the connection as it
 *  is now. A pick is told to TailPocket at once (ui/root.tsx), and the connection comes back on a tunnel that uses it. */
export function ConnectionScreen(props: { prefs: PrefStore; store: Store; linker: Linker; back: () => void }) {
  const p = useSignal(props.prefs.prefs)
  const { conn, route, status } = useConnection(props.store, props.linker)
  const why = useSignal(props.store.lostWhy)
  const under = () => (conn() === 'ready' ? pathWord(route()) : why() === 'no-route' ? t('lostNoRoute') : '')
  return (
    <View class="flex-1 bg-sidebar pt-[env(safe-area-inset-top)]">
      <View class="flex-row items-center h-12 pl-1 pr-3">
        <IconButton name="chevron-left" label={t('back')} testID="connection-back" onPress={props.back} />
        <Text class="text-[16px] font-semibold text-foreground ml-1">{t('connection')}</Text>
      </View>
      <Scroll class="flex-1" contentClass="pb-10">
        <View class="mt-3 border-t border-border">
          <Each of={CONNECTIONS} key={(c) => c}>{(c) => (
            <View role="button" testID={`connection-${c()}`} accessibilityState={{ selected: p().connection === c() }} onPress={() => props.prefs.set({ connection: c() })}
              class="flex-row items-center min-h-12 px-4 gap-3 border-b border-border bg-card active:bg-sidebar-accent">
              <View class="flex-1 py-2.5">
                <Text class="text-[14px] text-foreground">{t(CONNECTION_TEXT[c()][0])}</Text>
                <Text class="text-[12px] leading-[17px] text-sidebar-foreground mt-0.5">{t(CONNECTION_TEXT[c()][1])}</Text>
              </View>
              <View class="w-4"><If when={p().connection === c()}><Glyph name="check" size={16} class="tint-chart-3" /></If></View>
            </View>
          )}</Each>
        </View>
        <View class="mt-5 border-t border-border">
          <Fact label={t('connectionStatus')} value={status().word} dot={status().dot} under={under()} testID="connection-now" />
        </View>
        <View class="px-4 pt-2"><Text testID="connection-computer-note" class="text-[12px] leading-[17px] text-sidebar-foreground">{t('connectionComputerNote')}</Text></View>
      </Scroll>
    </View>
  )
}

export function SettingsScreen(props: { updates?: Updates; store: Store; prefs: PrefStore; link: Link; linker: Linker; fetcher?: typeof fetch; back: () => void; forget: () => void; openLanguage: () => void; openConnection: () => void; openDiagnostics: () => void; diagnosticStatus: DiagnosticStatus; diagnosticChanged: (s: DiagnosticStatus) => void }) {
  const rev = useRev(props.store)
  const p = useSignal(props.prefs.prefs)
  void diagnostics.refresh().then(props.diagnosticChanged)
  // Turning the progress page on: the user picks which agent on the computer sorts what they say (the one choice in
  // version one); the page turns on once the computer has it.
  const picking = signal<AgentChoice[] | null>(null)
  const pickNote = signal('')
  const run = async (line: string) => {
    const r = await props.store.exec(line, { timeout: 20 })
    if (r.code !== 0) throw new Error(r.stderr.trim() || `exit ${r.code}`)
    return r.stdout
  }
  const startPick = async () => {
    picking.set([]); pickNote.set('')
    try {
      const a = parseAgents(await run(agentsLine())).agents
      if (picking() === null) return // cancelled meanwhile
      picking.set(a)
      if (!a.length) pickNote.set(t('progressNoAgent'))
    } catch (e) {
      pickNote.set(t('progressCantRead', { error: explain(e) }))
    }
  }
  const pick = async (a: AgentChoice) => {
    try {
      await run(pickLine(a.id, lang()))
      picking.set(null)
      props.prefs.set({ progressFirst: true, progressAgent: a.name, progressLang: lang() })
    } catch (e) {
      props.store.toast({ title: t('progressCouldntOn'), body: explain(e), level: 'error' })
    }
  }
  const turnOff = () => {
    picking.set(null)
    props.prefs.set({ progressFirst: false })
    void run(switchLine(false)).catch((e) => props.store.toast({ title: t('progressCouldntOff'), body: explain(e), level: 'error' }))
  }
  const companion = signal('')
  const { conn, route, status } = useConnection(props.store, props.linker, () => {
    if (!companion()) void companionVersion(props.linker, props.fetcher).then((v) => companion.set(v))
  })
  const address = () => props.link.url.replace(/^https?:\/\//, '')
  // Only TailPocket's tunnel is end-to-end (Tailcat v0.6.0's README: "encrypted end-to-end with WireGuard"); the companion serves plain HTTP.
  const how = () => (props.link.via === 'demo' ? t('demoConnection') : props.link.via === 'tailpocket' ? t('tunnelE2E') : props.link.url.startsWith('https:') ? t('httpsEncrypted') : maybeTailnet(address()) ? t('notEncryptedByApp') : t('notEncrypted'))
  const under = () => {
    if (props.link.via === 'demo') return ''
    if (props.link.via !== 'tailpocket') return props.link.url.startsWith('https:') ? t('httpsDetail', { host: address() }) : maybeTailnet(address()) ? t('tailnetDetail', { host: address() }) : t('notEncryptedDetail', { host: address() })
    if (conn() !== 'ready') return ''
    return pathWord(route()) || t('pathUnknown')
  }
  const forget = async () => {
    const r = await AlertDialog.show({ title: t('forgetTitle', { name: props.link.name }), description: t('forgetDetail'),
      actions: [{ id: 'cancel', label: t('cancel'), role: 'cancel' }, { id: 'forget', label: t('forget'), role: 'destructive' }] })
    if (r === 'forget') props.forget()
  }
  // A browser or Android 13+ posts the system's notifications only once the person allows them; a browser asks only from a tap.
  const notify = signal<Permission>('unsupported')
  void notifications().then((n) => notify.set(n))
  const allow = () => void allowNotifications().then((n) => notify.set(n))
  // What Android does with each kind (MainActivity's channels), read again on the way back from Android's settings.
  const channels = signal<Channels | null>(null)
  const readChannels = () => void notifyChannels().then((c) => channels.set(c))
  readChannels()
  cleanup(onFront(() => { readChannels(); void notifications().then((n) => notify.set(n)) }))
  const NOTIFY = (n: Permission) => (n === 'default' ? t('notifyDefault') : n === 'granted' ? t('notifyGranted') : n === 'denied' ? t('notifyDenied') : '')
  const looks: { id: Appearance; label: Key }[] = [{ id: 'system', label: 'system' }, { id: 'light', label: 'light' }, { id: 'dark', label: 'dark' }]
  return (
    <View class="flex-1 bg-sidebar pt-[env(safe-area-inset-top)]">
      <View class="flex-row items-center h-12 pl-1 pr-3">
        <IconButton name="chevron-left" label={t('back')} testID="back" onPress={props.back} />
        <Text class="text-[16px] font-semibold text-foreground ml-1">{t('settings')}</Text>
      </View>
      <Scroll class="flex-1" contentClass="pb-10">
        <SectionLabel label={t('look')} />
        <Row label={t('appearance')}>
          <View class="flex-row rounded-[7px] bg-muted p-0.5">
            {looks.map((l) => (
              <View role="button" testID={`look-${l.id}`} onPress={() => props.prefs.set({ appearance: l.id })} class={`px-2.5 py-1 rounded-[6px] ${p().appearance === l.id ? 'bg-background' : ''}`}>
                <Text class={`text-[12px] ${p().appearance === l.id ? 'text-foreground' : 'text-muted-foreground'}`}>{t(l.label)}</Text>
              </View>
            ))}
          </View>
        </Row>
        <Row label={t('language')} testID="settings-language" onPress={props.openLanguage}>
          <Text testID="settings-language-state" class="text-[13px] text-sidebar-foreground">{p().language ? langName(p().language as Lang) : t('followSystem')}</Text>
          <Glyph name="chevron-right" size={16} />
        </Row>
        <Row label={t('terminalTextSize')} detail={t('points', { n: p().fontSize })}>
          <IconButton name="minus" label={t('smaller')} testID="font-smaller" disabled={p().fontSize <= FONT_MIN} onPress={() => props.prefs.set({ fontSize: p().fontSize - 1 })} />
          <IconButton name="plus" label={t('larger')} testID="font-larger" disabled={p().fontSize >= FONT_MAX} onPress={() => props.prefs.set({ fontSize: p().fontSize + 1 })} />
        </Row>
        <Row label={t('showAgentNames')} detail={t('showAgentNamesDetail')}>
          <Toggle label={t('showAgentNames')} testID="show-agent-names" on={p().showAgentNames} onChange={(on) => props.prefs.set({ showAgentNames: on })} />
        </Row>
        <SectionLabel label={t('alerts')} />
        <Row label={t('agentNeedsYou')} detail={t('agentNeedsYouDetail')}>
          <Toggle label={t('agentNeedsYou')} testID="alert-blocked" on={p().alertBlocked} onChange={(on) => props.prefs.set({ alertBlocked: on })} />
        </Row>
        <If when={channels() && p().alertBlocked}><ChannelLine kind="blocked" label={t('agentNeedsYou')} state={channels()!.blocked} /></If>
        <Row label={t('agentFinished')} detail={t('agentFinishedDetail')}>
          <Toggle label={t('agentFinished')} testID="alert-done" on={p().alertDone} onChange={(on) => props.prefs.set({ alertDone: on })} />
        </Row>
        <If when={channels() && p().alertDone}><ChannelLine kind="done" label={t('agentFinished')} state={channels()!.done} /></If>
        <If when={platform.os === 'android'}>
          <Row label={t('stayConnected')} detail={t('stayConnectedDetail')}>
            <Toggle label={t('stayConnected')} testID="stay-connected" on={p().stayConnected} onChange={(on) => props.prefs.set({ stayConnected: on })} />
          </Row>
          <If when={channels() && p().stayConnected}><ChannelLine kind="connection" label={t('stayConnected')} state={channels()!.connection} /></If>
        </If>
        <If when={notify() !== 'unsupported'}>
          <Row label={t('systemNotifications')} detail={NOTIFY(notify())}>
            <If when={notify() === 'default'}>
              <View role="button" testID="notify-allow" onPress={allow} class="px-2.5 py-1 rounded-[6px] bg-muted active:bg-accent">
                <Text class="text-[12px] text-foreground">{t('allow')}</Text>
              </View>
            </If>
            <If when={notify() === 'denied' && platform.os === 'android'}>
              <View role="button" testID="notify-settings" onPress={() => openChannel('app')} class="px-2.5 py-1 rounded-[6px] bg-muted active:bg-accent">
                <Text class="text-[12px] text-foreground">{t('androidSettings')}</Text>
              </View>
            </If>
          </Row>
          {/* No push server: iOS posts the app's own notifications only until it stops the app in the background. */}
          <If when={platform.os === 'ios'}>
            <Text testID="ios-notify-limit" class="px-4 pb-2 text-[12px] leading-[17px] text-sidebar-foreground">{t('iosNotifyLimit')}</Text>
          </If>
        </If>
        <ChatSetupRows store={props.store} />
        {/* Experimental and off by default. */}
        <SectionLabel label={t('experimental')} />
        <Row label={t('progressPage')} detail={p().progressFirst && p().progressAgent ? `${t('progressPageDetail')} · ${t('progressBy', { agent: p().progressAgent })}` : t('progressPageDetail')}>
          <Toggle label={`${t('progressPage')}, ${t('experimental')}`} testID="progress-first" on={p().progressFirst || picking() !== null} onChange={(on) => { if (on) void startPick(); else turnOff() }} />
        </Row>
        <If when={picking()}>
          <View testID="agent-pick" class="px-4 py-2 border-b border-border bg-card gap-1">
            <Text class="text-[13px] text-foreground">{t('progressPickTitle')}</Text>
            <Text class="text-[12px] text-sidebar-foreground">{t('progressPickDetail')}</Text>
            <If when={picking()!.length} else={<Text class="text-[13px] text-muted-foreground py-2">{pickNote() || t('progressLooking')}</Text>}>
              <Each of={picking()!} key={(a) => a.id}>{(a) => (
                <View role="button" testID={`agent-${a().id}`} disabled={!a().usable} onPress={() => { if (a().usable) void pick(a()) }}
                  class={`min-h-10 py-1.5 justify-center ${a().usable ? 'active:opacity-70' : 'opacity-50'}`}>
                  <Text class="text-[14px] text-foreground">{a().name}</Text>
                  <If when={a().needs ? t('agentTooOld', { need: a().needs, have: a().version }) : a().note}>{(n) => <Text class="text-[12px] text-sidebar-foreground">{n()}</Text>}</If>
                </View>
              )}</Each>
            </If>
            <View role="button" testID="agent-pick-cancel" onPress={() => picking.set(null)} class="h-9 justify-center active:opacity-70">
              <Text class="text-[13px] text-muted-foreground">{t('cancel')}</Text>
            </View>
          </View>
        </If>
        <SectionLabel label={t('computer')} />
        <Fact label={t('computerName')} value={props.link.via === 'demo' ? t('demoComputer') : props.link.name} testID="computer-name" />
        <Fact label={t('connectionStatus')} value={status().word} dot={status().dot} testID="connection-status" />
        {/* Through TailPocket the person picks its routes (ConnectionScreen); other links have one way to the computer. */}
        <If when={props.link.via === 'tailpocket'} else={<Fact label={t('connection')} value={how()} under={under()} testID="connection" />}>
          <View role="button" testID="connection" onPress={props.openConnection}
            class="flex-row items-center min-h-12 px-4 gap-3 border-b border-border bg-card active:bg-sidebar-accent">
            <View class="flex-1 py-2">
              <Text class="text-[14px] text-foreground">{t('connection')}</Text>
              <Text class="text-[12px] text-sidebar-foreground mt-0.5">{how()}</Text>
              <If when={under()}><Text class="text-[12px] text-sidebar-foreground">{under()}</Text></If>
            </View>
            <Text testID="connection-state" class="text-[13px] text-sidebar-foreground">{t(CONNECTION_TEXT[p().connection][0])}</Text>
            <Glyph name="chevron-right" size={16} />
          </View>
        </If>
        <If when={(rev(), props.store.hello)}><Fact label={t('coreVersion')} value={props.store.hello?.version ?? ''} /></If>
        <If when={companion()}><Fact label={t('companionVersion')} value={companion()} /></If>
        <If when={props.updates}><UpdateRows updates={props.updates!} /></If>
        <If when={props.link.via === 'demo'} else={
          <Row label={t('forgetComputer')} testID="forget" onPress={() => void forget()}>
            <Text class="text-[13px] text-destructive">{t('forget')}</Text>
          </Row>
        }>
          <Row label={t('demoLeaveFull')} testID="settings-demo-leave" onPress={props.forget} />
        </If>
        <Row label={t('diagnosticTitle')} testID="settings-diagnostics" onPress={props.openDiagnostics}>
          <Text testID="settings-diagnostics-state" class="text-[13px] text-sidebar-foreground">{t(!props.diagnosticStatus.supported ? 'diagnosticUnsupportedState' : props.diagnosticStatus.enabled ? 'diagnosticOnState' : 'diagnosticOffState')}</Text>
          <Glyph name="chevron-right" size={16} />
        </Row>
        <View class="mt-5 border-t border-border">
          <Row label={t('privacy')} testID="settings-privacy" onPress={openPrivacy} />
        </View>
      </Scroll>
    </View>
  )
}
