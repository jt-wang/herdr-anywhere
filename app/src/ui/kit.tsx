// What every screen shares: the store as NSH signals, herdr's status glyphs and icons, buttons.
// Shared with DSH: black/white canvas, neutral surfaces and status words. chart-2 is only the small attention
// glyph; ring/chart-5 is actual focus. Normal work, connection and completion are not accent colours.
import { cleanup, Clipboard, colorScheme, effect, Image, openUrl, platform, signal, Text, untracked, View, type Accessor, type ViewStyle } from 'nsh'
import { AlertDialog as NativeAlertDialog, Sheet as NativeSheet, type SheetProps } from 'nsh/base'
import { SIGN_IN_CHECK_URL, type Conn, type Store, type ConnSubscriberSite } from '../store.ts'
import type { Shown } from '../model/model.ts'
import { duration } from '../model/agents.ts'
import { PROGRAM_ICONS as ICONS } from '../data/icons.ts'
import { iconTinted } from '../model/programs.ts'
import { UI } from '../data/ui.ts'
import { t, type Key } from '../strings/index.ts'
import { PHONE_UI } from './phone-glyphs.ts'

/** Local popups cover chat without changing its route. Count nested and exiting sheets too. */
export const modalDepth = signal(0)

export function Sheet(props: SheetProps) {
  let counted = false
  const claim = () => { if (!counted) { counted = true; modalDepth.update((n) => n + 1) } }
  const release = () => { if (counted) { counted = false; modalDepth.update((n) => n - 1) } }
  effect(() => { const open = props.open ?? props.defaultOpen; if (open) untracked(claim) })
  cleanup(release)
  return <NativeSheet {...props}
    onOpenChange={(open, reason) => { if (open) claim(); props.onOpenChange?.(open, reason) }}
    onClose={() => { if (props.open !== true) release(); props.onClose?.() }} />
}

export const AlertDialog = {
  async show(...args: Parameters<typeof NativeAlertDialog.show>) {
    modalDepth.update((n) => n + 1)
    try { return await NativeAlertDialog.show(...args) }
    finally { modalDepth.update((n) => n - 1) }
  },
}

/** The store's change counter as a signal: read `rev()` in a view to follow the store. */
export function useRev(store: Store): Accessor<number> {
  const rev = signal(store.rev.get())
  const offs = [store.rev.sub(() => rev.update((n) => n + 1)), store.conn.sub(() => rev.update((n) => n + 1), 'kit.useRev'), store.machines.sub(() => rev.update((n) => n + 1))]
  cleanup(() => offs.forEach((off) => off()))
  return rev
}

export function useSignal<T>(s: { get(): T; sub(f: (v: T) => void, site?: ConnSubscriberSite): () => unknown }): Accessor<T> {
  const v = signal(s.get())
  const off = s.sub((x) => v.set(x), 'kit.useSignal')
  cleanup(() => { off() })
  return v
}

/** While the line is down: why, in words (store.ts LOST_TEXT), and how old what the screen shows is. */
/** The connection's word and dot, the same on every screen. With no route in common the phone does not get through
 *  until a setting changes, so it is not connected, not reconnecting. */
export function connStatus(c: Conn, why: string): { word: string; dot: string } {
  return c === 'ready' ? { word: t('connected'), dot: 'bg-chart-3' }
    // Stuck (store.ts STUCK_MS): the banner says it can't reach the computer, so this says not connected, still trying.
    : (c === 'connecting' || c === 'reconnecting') && why === 'stuck' ? { word: t('notConnectedRetrying'), dot: 'bg-destructive' }
    : c === 'connecting' ? { word: t('connecting'), dot: 'bg-muted-foreground' }
    : c === 'reconnecting' && why !== 'no-route' ? { word: t('reconnecting'), dot: 'bg-muted-foreground' }
    : { word: t('notConnected'), dot: 'bg-destructive' }
}

export function ConnBanner(props: { store: Store }) {
  const s = props.store
  const rev = useRev(s)
  const error = useSignal(s.error)
  const why = useSignal(s.lostWhy)
  // The age moves on while nothing else does.
  const tick = signal(0)
  const timer = setInterval(() => tick.update((n) => n + 1), 15000)
  cleanup(() => clearInterval(timer))
  // Not running there, or no answer through the tunnel: what to run (restart.ts) and pairing again. A name or a proxy
  // (pair --url) gives no command: the address the phone uses, to start it there as it was set up.
  const toDo = () => why() === 'no-answer' || why() === 'refused'
  const atAddress = () => (why() === 'refused' && !s.restart?.command ? s.restart?.address ?? '' : '')
  // Down a while for no telling reason (store.ts STUCK_MS): what may be in the way, and what to do, over any first try.
  const stuck = () => why() === 'stuck'
  const first = useFirstCause(s)
  const options = signal(false)
  const connecting = () => { rev(); return s.conn.get() === 'connecting' && !stuck() }
  const down = () => { rev(); const c = s.conn.get(); return c === 'connecting' || c === 'reconnecting' || c === 'failed' }
  const age = () => { rev(); tick(); const ms = s.staleFor(); return ms === undefined || ms < 60000 ? '' : t('lastUpdated', { age: duration(ms) }) }
  return (
    <If when={down()}>
      <View testID="conn-banner" class="flex-row gap-2 px-4 py-2 bg-popover border-b border-border">
        <View class="w-1.5 h-1.5 mt-[7px] rounded-full bg-chart-2" />
        <View class="flex-1 gap-[1px]">
          <Text class="text-[12.5px] text-foreground">{connecting() ? t('connecting') : atAddress() ? t('lostRefusedAt', { address: atAddress() })
            : why() === 'refused' && s.restart?.pair ? t('lostRefusedPair') : error() || t('notConnected')}</Text>
          <If when={!connecting() && toDo()}>
            <View class="mt-1 flex-row items-center gap-2">
              <If when={s.restart?.command} else={<View class="flex-1" />}>
                <Text selectable testID="conn-command" class="flex-1 text-[12.5px] font-mono text-foreground">{s.restart?.command}</Text>
              </If>
              <If when={s.pairAgain}>
                <View role="button" testID="pair-again" onPress={() => s.pairAgain?.()} class="rounded-(--radius) border border-input px-2.5 py-1 active:bg-accent">
                  <Text class="text-[12.5px] font-medium text-foreground">{t('pairAgain')}</Text>
                </View>
              </If>
            </View>
          </If>
          <If when={!connecting() && why() === 'no-route' && s.openConnection}>
            <View class="mt-1 flex-row gap-2">
              <If when={s.useAutomatic}>
                <View role="button" testID="conn-use-auto" onPress={() => s.useAutomatic?.()} class="rounded-(--radius) border border-input px-2.5 py-1 active:bg-accent">
                  <Text class="text-[12.5px] font-medium text-foreground">{t('connectionUseAuto')}</Text>
                </View>
              </If>
              <View role="button" testID="conn-open-connection" onPress={() => s.openConnection?.()} class="rounded-(--radius) border border-input px-2.5 py-1 active:bg-accent">
                <Text class="text-[12.5px] font-medium text-foreground">{t('connection')}</Text>
              </View>
            </View>
          </If>
          {/* One line for the likeliest cause; the others, each with what to do, are in the sheet. */}
          <If when={stuck()}>
            <Text testID="stuck-likely" class="text-[12.5px] text-muted-foreground">{t(LIKELY[first()])}</Text>
            <View class="mt-1.5 flex-row gap-2">
              <View role="button" testID="conn-retry" onPress={() => s.retryNow()} class="rounded-(--radius) bg-primary px-3 py-1 active:opacity-80">
                <Text class="text-[12.5px] font-medium text-primary-foreground">{t('tryAgain')}</Text>
              </View>
              <BannerButton testID="conn-options" label={t('stuckOptions')} onPress={() => options.set(true)} />
            </View>
          </If>
          <If when={age()}><Text testID="conn-age" class="text-[11.5px] text-sidebar-foreground">{age()}</Text></If>
        </View>
      </View>
      <StuckSheet store={s} open={() => options() && stuck()} close={() => options.set(false)} />
    </If>
  )
}

/** What may be in the way, in the order the stuck hint gives it when TailPocket's last failed try says nothing. */
const CAUSES = ['network', 'companion', 'route', 'keys'] as const
type Cause = typeof CAUSES[number]
/** The cause TailPocket's last failed try points at (link.ts DIAL_REASONS): something answered for the computer and
 *  didn't take this phone, Companion isn't running there (or the computer was reset, which pairing again is for); the
 *  relay wouldn't carry it, another way to connect. Only an order: none of it is authenticated. */
const FIRST: Record<string, Cause> = { refused: 'companion', 'handshake-closed': 'companion', 'relay-down': 'route', 'no-answer': 'network' }
const LIKELY: Record<Cause, Key> = { network: 'stuckLikely', companion: 'stuckLikelyCompanion', route: 'stuckLikelyRelay', keys: 'stuckLikely' }
function useFirstCause(s: Store): () => Cause {
  const failure = s.dialFailure ? useSignal(s.dialFailure) : () => ''
  return () => FIRST[failure()] ?? 'network'
}

/** The stuck banner's Other options: each thing that may be in the way, with what to do about it, the likeliest first. */
function StuckSheet(props: { store: Store; open: () => boolean; close: () => void }) {
  const s = props.store
  const first = useFirstCause(s)
  const order = () => [first(), ...CAUSES.filter((c) => c !== first())]
  const then = (f?: () => void) => () => { props.close(); f?.() }
  const copy = () => void Clipboard.writeText(s.restart?.command ?? '').then(() => s.toast({ title: t('copied'), level: 'info' }), () => {})
  return (
    <Sheet open={props.open()} side="bottom" onOpenChange={(o) => { if (!o) props.close() }}
      popupClass={`${PANEL} px-4 pt-3 pb-[env(safe-area-inset-bottom)]`} popupStyle={PANEL_STYLE} backdropClass={PANEL_BACKDROP}>
      <View testID="stuck-sheet" class="gap-3 pb-2">
        <Text class="text-[15px] font-semibold text-foreground">{t('stuckSheetTitle')}</Text>
        <Each of={order()} key={(c) => c}>{(c) => (
          c() === 'network' ? (
            <StuckCause testID="stuck-network" title={t('stuckWhyNetwork')} how={t('stuckWhyNetworkHow')}>
              <BannerButton testID="conn-sign-in" label={t('checkSignIn')} onPress={then(() => void openUrl(SIGN_IN_CHECK_URL).catch(() => {}))} />
            </StuckCause>
          ) : c() === 'companion' ? (
            <StuckCause testID="stuck-companion" title={t('stuckWhyCompanion')}
              how={s.restart?.command ? t('stuckWhyCompanionHow') : t('stuckWhyCompanionAt', { address: s.restart?.address ?? '' })}>
              <If when={s.restart?.command}>
                <View class="flex-row items-center gap-2">
                  <Text selectable testID="conn-command" class="flex-1 text-[12.5px] font-mono text-foreground">{s.restart?.command}</Text>
                  <BannerButton testID="conn-copy" label={t('copyPath')} onPress={copy} />
                </View>
              </If>
            </StuckCause>
          ) : c() === 'route' ? (
            <If when={s.openConnection}>
              <StuckCause testID="stuck-route" title={t('stuckWhyRoute')} how={t('stuckWhyRouteHow')}>
                <BannerButton testID="conn-open-connection" label={t('connection')} onPress={then(s.openConnection)} />
              </StuckCause>
            </If>
          ) : (
            <If when={s.pairAgain}>
              <StuckCause testID="stuck-keys" title={t('stuckWhyKeys')} how={t('stuckWhyKeysHow')}>
                <BannerButton testID="pair-again" label={t('pairAgain')} onPress={then(s.pairAgain)} />
              </StuckCause>
            </If>
          )
        )}</Each>
        <Button label={t('close')} testID="stuck-close" kind="plain" onPress={props.close} />
      </View>
    </Sheet>
  )
}

function StuckCause(props: { testID: string; title: string; how: string; children?: any }) {
  return (
    <View testID={props.testID} class="gap-1 border-t border-border pt-3">
      <Text class="text-[14px] font-medium text-foreground">{props.title}</Text>
      <Text class="text-[12.5px] text-muted-foreground">{props.how}</Text>
      <View class="flex-row">{props.children}</View>
    </View>
  )
}

function BannerButton(props: { testID: string; label: string; onPress: () => void }) {
  return (
    <View role="button" testID={props.testID} onPress={props.onPress} class="rounded-(--radius) border border-input px-2.5 py-1 active:bg-accent">
      <Text class="text-[12.5px] font-medium text-foreground">{props.label}</Text>
    </View>
  )
}

const STATUS_LABEL: Record<string, Key> = { blocked: 'needsYou', done: 'done', working: 'working', idle: 'idle', unknown: 'unknown' }
export const statusLabel = (s: Shown | string | undefined) => (s ? (STATUS_LABEL[s] ? t(STATUS_LABEL[s]) : s) : '')

/** Quiet, static list/header summaries; only a genuine needs-you mark gets the attention colour. */
export function StatusDot(props: { status: Shown | string | undefined; size?: number; attention?: boolean }) {
  const size = () => props.size ?? 8
  const color = () => props.status === 'blocked' ? props.attention === false ? 'bg-muted-foreground' : 'bg-chart-2' : props.status === 'done' || props.status === 'working' ? 'bg-muted-foreground' : ''
  return (
    <View accessibilityLabel={statusLabel(props.status)} testID={`status-${props.status ?? 'none'}`}
      class={`rounded-full ${color() || 'border border-chart-4'}`} style={{ width: size(), height: size() }} />
  )
}

/** The icon file a program is drawn with. An icon in its own colours that is white where the light theme's background
 *  is near white (Kimi's K) has an `<icon>-light` file, drawn on the light theme. Checked by drawing every
 *  untinted icon on #f1f1ee: only Kimi's needed one; Codex's and Kiro's white sits on their own coloured tile. */
export function programIconKey(icon: string | undefined, scheme: 'light' | 'dark'): string {
  if (!icon || !ICONS[icon]) return 'shell'
  return scheme === 'light' && ICONS[`${icon}-light`] ? `${icon}-light` : icon
}

export function ProgramIcon(props: { icon: string | undefined; size?: number }) {
  const src = () => ICONS[programIconKey(props.icon, colorScheme() === 'dark' ? 'dark' : 'light')]
  // As herdr's tabs draw them: a one-colour icon in the muted text colour, the others in their own colours.
  return <Image src={src()} class={iconTinted(props.icon) ? 'tint-muted-foreground' : undefined} style={{ width: props.size ?? 16, height: props.size ?? 16 }} />
}

const GLYPHS: Record<string, any> = { ...UI, ...PHONE_UI }

export function Glyph(props: { name: keyof typeof GLYPHS; size?: number; class?: string }) {
  return <Image src={GLYPHS[props.name]} class={props.class ?? 'tint-muted-foreground'} style={{ width: props.size ?? 18, height: props.size ?? 18 }} />
}

export const TOUCH_TARGET = platform.os === 'android' ? 48 : 44

export function IconButton(props: { name: keyof typeof GLYPHS; label: string; testID?: string; onPress: () => void; active?: boolean; disabled?: boolean }) {
  return (
    <View role="button" accessibilityLabel={props.label} testID={props.testID} disabled={props.disabled} onPress={props.onPress}
      style={{ width: TOUCH_TARGET, height: TOUCH_TARGET }}
      class={`items-center justify-center rounded-(--radius) active:bg-accent focus-visible:ring-2 ring-ring transition-colors duration-100 motion-reduce:transition-none ${props.active ? 'bg-accent' : ''} ${props.disabled ? 'opacity-40' : ''}`}>
      <Glyph name={props.name} class={props.active ? 'tint-foreground' : 'tint-muted-foreground'} />
    </View>
  )
}

const PRIVACY_URL = 'https://herdr-anywhere.jingtao.io/privacy/'
export const openPrivacy = () => void openUrl(PRIVACY_URL).catch(() => {})

/** The app's panels from the bottom (a menu, Add an agent, renaming, the switcher…): a rounded panel with an edge,
 *  over the screen dimmed behind it, so it stands apart from a terminal of the same colour in light and dark. */
export const appleMaterial = platform.os === 'ios' || platform.os === 'macos'
/** Only the floating controls use Apple material; conversation and Android/web surfaces stay opaque. */
export const COMPOSER_SURFACE = appleMaterial ? 'bg-card/80' : 'bg-card'
export const COMPOSER_STYLE: ViewStyle = appleMaterial ? { glassEffect: 'regular', backdropFilter: 'blur(12px)' } : {}
export const PANEL = `w-full max-w-[680px] self-center border-t border-x border-border ${platform.os === 'android' ? 'rounded-t-[28px]' : 'rounded-t-[24px]'} ${appleMaterial ? 'bg-popover/85' : 'bg-popover'}`
/** Native Liquid Glass only on supported Apple OS versions, system material before that. No Android/web blur. */
export const PANEL_STYLE: ViewStyle = appleMaterial ? { glassEffect: 'regular', backdropFilter: 'blur(16px)' } : {}
export const PANEL_BACKDROP = 'bg-black/40'

export function Button(props: { label: string; testID?: string; onPress: () => void; kind?: 'primary' | 'plain' | 'outline' | 'danger'; disabled?: boolean }) {
  const k = () => props.kind ?? 'primary'
  return (
    <View role="button" testID={props.testID} disabled={props.disabled} onPress={props.onPress}
      style={{ minHeight: TOUCH_TARGET }}
      class={`rounded-(--radius) px-4 py-3 items-center justify-center transition-colors duration-100 motion-reduce:transition-none ${k() === 'primary' ? 'bg-primary active:opacity-80' : k() === 'outline' ? 'border border-input active:bg-accent' : 'active:bg-accent'} ${props.disabled ? 'opacity-50' : ''}`}>
      <Text class={`text-[15px] font-medium ${k() === 'primary' ? 'text-primary-foreground' : k() === 'danger' ? 'text-destructive' : 'text-foreground'}`}>{props.label}</Text>
    </View>
  )
}

export function SectionLabel(props: { label: string; right?: any }) {
  return (
    <View class="flex-row items-center justify-between px-[22px] pt-5 pb-1.5">
      <Text class="text-[12px] font-medium text-sidebar-foreground">{props.label}</Text>
      {props.right}
    </View>
  )
}

/** Summaries never loop. Active work animates only the foreground conversation's current-step text. */
export function StatusMark(props: { status: Shown | string | undefined; compact?: boolean; size?: number; attention?: boolean }) {
  const size = () => props.size ?? 8
  return (
      <If when={props.status === 'blocked'} else={<StatusDot status={props.status} size={size()} />}>
        <If when={!props.compact} else={
          <View accessibilityLabel={t('needsYou')} testID="status-blocked" class="w-[15px] h-[15px] items-center justify-center">
            <Text class={`text-[10.5px] font-bold ${props.attention === false ? 'text-muted-foreground' : 'text-chart-2'}`}>!</Text>
          </View>
        }>
          <View accessibilityLabel={t('needsYou')} testID="status-blocked" class="flex-row gap-1 items-center h-[18px]">
            <Text class={`text-[10.5px] font-semibold ${props.attention === false ? 'text-muted-foreground' : 'text-chart-2'}`}>!</Text>
            <Text class="text-[12px] text-muted-foreground">{t('needsYou')}</Text>
          </View>
        </If>
      </If>
  )
}

/** Keep the identifying initial without turning every space into a competing accent. */
export function SpaceBadge(props: { name: string; color: string; size?: number }) {
  const n = () => props.name.trim() || '?'
  const size = () => props.size ?? 18
  return (
    <View class="items-center justify-center bg-secondary" style={{ width: size(), height: size(), borderRadius: size() * 5 / 18 }}>
      <Text class="text-muted-foreground font-medium" style={{ fontSize: size() * 10.5 / 18 }}>{[...n()][0].toUpperCase()}</Text>
    </View>
  )
}

export { shortPath } from './paths.ts'

/** The choices, wrapped onto as many lines as they take, each label whole: what a choice allows ("during this
 *  session") or refuses is read before it is tapped, not cut off at the screen's edge. */
export function ChoiceRow(props: { testID?: string; children?: any }) {
  // Taken once: read in the JSX below, `props.children` would be made again (every button with it) each time what it
  // reads changes, such as the menu after any agent's report, and a button under a finger would lose its press.
  const children = props.children
  return <View testID={props.testID} class="flex-1 min-w-0 flex-row flex-wrap gap-1.5">{children}</View>
}
