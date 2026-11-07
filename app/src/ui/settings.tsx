// Settings: how the app looks, which alerts show, and the computer it is paired with.
import { cleanup, platform, Scroll, signal, Text, View } from 'nsh'
import { AlertDialog } from 'nsh/base'
import type { Store } from '../store.ts'
import { busyWord, runUpdate, useUpdates } from './update.tsx'
import { APP_VERSION, type Updates } from '../updates.ts'
import { companionVersion, type Link, type Linker, type Route } from '../link.ts'
import { FONT_MAX, FONT_MIN, type Appearance, type PrefStore } from '../prefs.ts'
import { allowNotifications, notifications, notifyChannels, onFront, openChannel, type ChannelKind, type Channels, type ChannelState, type Permission } from '../page.ts'
import { Glyph, IconButton, openPrivacy, SectionLabel, useRev, useSignal } from './kit.tsx'
import { t, type Key } from '../strings/index.ts'

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

/** A Tailscale name, or an address in the range Tailscale gives out (100.64.0.0/10, which carriers use too). */
const maybeTailnet = (host: string) => /^100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\./.test(host) || /\.ts\.net(:|$)/.test(host)

/** How often the route is asked again: a direct path can come a little after the relay. */
const ROUTE_EVERY_MS = 10_000

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

function Toggle(props: { on: boolean; label: string; testID: string; onChange: (on: boolean) => void }) {
  return (
    <View role="switch" accessibilityLabel={props.label} accessibilityState={{ checked: props.on }} testID={props.testID} onPress={() => props.onChange(!props.on)}
      class={`w-11 h-[26px] rounded-full p-[3px] ${props.on ? 'bg-chart-3 items-end' : 'bg-input items-start'}`}>
      <View class="w-5 h-5 rounded-full bg-white" />
    </View>
  )
}

export function SettingsScreen(props: { updates?: Updates; store: Store; prefs: PrefStore; link: Link; linker: Linker; fetcher?: typeof fetch; back: () => void; forget: () => void }) {
  const rev = useRev(props.store)
  const p = useSignal(props.prefs.prefs)
  const conn = useSignal(props.store.conn)
  const route = signal<Route | null>(null)
  const companion = signal('')
  const ask = () => {
    if (conn() !== 'ready') return
    void props.linker.path().then((r) => route.set(r))
    if (!companion()) void companionVersion(props.linker, props.fetcher).then((v) => companion.set(v))
  }
  ask()
  const asking = setInterval(ask, ROUTE_EVERY_MS)
  const offConn = props.store.conn.sub(() => ask())
  cleanup(() => { clearInterval(asking); offConn() })
  const status = () => {
    const c = conn()
    return c === 'ready' ? { word: t('connected'), dot: 'bg-chart-3' }
      : c === 'connecting' ? { word: t('connecting'), dot: 'bg-muted-foreground' }
      : c === 'reconnecting' ? { word: t('reconnecting'), dot: 'bg-muted-foreground' }
      : { word: t('notConnected'), dot: 'bg-destructive' }
  }
  const address = () => props.link.url.replace(/^https?:\/\//, '')
  // Only TailPocket's tunnel is end-to-end (Tailcat v0.6.0's README: "encrypted end-to-end with WireGuard"); the companion serves plain HTTP.
  const how = () => (props.link.via === 'demo' ? t('demoConnection') : props.link.via === 'tailpocket' ? t('tunnelE2E') : props.link.url.startsWith('https:') ? t('httpsEncrypted') : maybeTailnet(address()) ? t('notEncryptedByApp') : t('notEncrypted'))
  const under = () => {
    if (props.link.via === 'demo') return ''
    if (props.link.via !== 'tailpocket') return props.link.url.startsWith('https:') ? t('httpsDetail', { host: address() }) : maybeTailnet(address()) ? t('tailnetDetail', { host: address() }) : t('notEncryptedDetail', { host: address() })
    if (conn() !== 'ready') return ''
    const r = route()
    return r?.route === 'direct' ? t('pathDirect') : r?.route === 'relay' ? (r.relay ? t('pathRelay', { region: r.relay.toUpperCase() }) : t('pathRelayPlain')) : t('pathUnknown')
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
        </If>
        <SectionLabel label={t('computer')} />
        <Fact label={t('computerName')} value={props.link.name} />
        <Fact label={t('connectionStatus')} value={status().word} dot={status().dot} testID="connection-status" />
        <Fact label={t('connection')} value={how()} under={under()} testID="connection" />
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
        <View class="mt-5 border-t border-border">
          <Row label={t('privacy')} testID="settings-privacy" onPress={openPrivacy} />
        </View>
      </Scroll>
    </View>
  )
}
