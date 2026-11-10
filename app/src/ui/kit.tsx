// What every screen shares: the store as NSH signals, herdr's status glyphs and icons, buttons.
// NSH 0.3.0 makes classes only of shadcn's token names, so herdr's other colors (herdr-app settings.rs) sit on
// these (nsh.json): sidebar = chrome (bars), sidebar-primary = canvas (behind cards), sidebar-accent = hover,
// sidebar-foreground = dim text, accent = selected, secondary = raised; chart-1..4 = working, blocked, done,
// idle; chart-5 = herdr's accent.
import { cleanup, colorScheme, Image, openUrl, Scroll, signal, Text, View, type Accessor } from 'nsh'
import type { Store } from '../store.ts'
import type { Shown } from '../model/model.ts'
import { duration } from '../model/agents.ts'
import { PROGRAM_ICONS as ICONS } from '../data/icons.ts'
import { iconTinted } from '../model/programs.ts'
import { UI } from '../data/ui.ts'
import { t, type Key } from '../strings/index.ts'
import { PHONE_UI } from './phone-glyphs.ts'

/** The store's change counter as a signal: read `rev()` in a view to follow the store. */
export function useRev(store: Store): Accessor<number> {
  const rev = signal(store.rev.get())
  const offs = [store.rev.sub((v) => rev.set(v)), store.conn.sub(() => rev.update((n) => n + 1)), store.machines.sub(() => rev.update((n) => n + 1))]
  cleanup(() => offs.forEach((off) => off()))
  return rev
}

export function useSignal<T>(s: { get(): T; sub(f: (v: T) => void): () => unknown }): Accessor<T> {
  const v = signal(s.get())
  const off = s.sub((x) => v.set(x))
  cleanup(() => { off() })
  return v
}

/** While the line is down: why, in words (store.ts LOST_TEXT), and how old what the screen shows is. */
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
  const down = () => { rev(); const c = s.conn.get(); return c === 'reconnecting' || c === 'failed' }
  const age = () => { rev(); tick(); const ms = s.staleFor(); return ms === undefined ? '' : ms < 60000 ? t('lastUpdatedNow') : t('lastUpdated', { age: duration(ms) }) }
  return (
    <If when={down()}>
      <View testID="conn-banner" class="flex-row gap-2 px-4 py-2 bg-popover border-b border-border">
        <View class="w-1.5 h-1.5 mt-[7px] rounded-full bg-chart-2" />
        <View class="flex-1 gap-[1px]">
          <Text class="text-[12.5px] text-foreground">{atAddress() ? t('lostRefusedAt', { address: atAddress() }) : error() || t('notConnected')}</Text>
          <If when={toDo()}>
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
          <If when={age()}><Text class="text-[11.5px] text-sidebar-foreground">{age()}</Text></If>
        </View>
      </View>
    </If>
  )
}

const STATUS_LABEL: Record<string, Key> = { blocked: 'needsYou', done: 'done', working: 'working', idle: 'idle', unknown: 'unknown' }
export const statusLabel = (s: Shown | string | undefined) => (s ? (STATUS_LABEL[s] ? t(STATUS_LABEL[s]) : s) : '')

/** herdr's status dot: needs you (red), done (blue), working (yellow), idle (grey ring). */
export function StatusDot(props: { status: Shown | string | undefined; size?: number }) {
  const size = () => props.size ?? 8
  const color = () => ({ blocked: 'bg-chart-2', done: 'bg-chart-3', working: 'bg-chart-1' } as Record<string, string>)[props.status ?? ''] ?? ''
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

export function IconButton(props: { name: keyof typeof GLYPHS; label: string; testID?: string; onPress: () => void; active?: boolean; disabled?: boolean }) {
  return (
    <View role="button" accessibilityLabel={props.label} testID={props.testID} disabled={props.disabled} onPress={props.onPress}
      class={`w-10 h-10 items-center justify-center rounded-(--radius) active:bg-accent ${props.active ? 'bg-accent' : ''} ${props.disabled ? 'opacity-40' : ''}`}>
      <Glyph name={props.name} class={props.active ? 'tint-foreground' : 'tint-muted-foreground'} />
    </View>
  )
}

const PRIVACY_URL = 'https://herdr-anywhere.jingtao.io/privacy/'
export const openPrivacy = () => void openUrl(PRIVACY_URL).catch(() => {})

export function Button(props: { label: string; testID?: string; onPress: () => void; kind?: 'primary' | 'plain' | 'outline' | 'danger'; disabled?: boolean }) {
  const k = () => props.kind ?? 'primary'
  return (
    <View role="button" testID={props.testID} disabled={props.disabled} onPress={props.onPress}
      class={`rounded-(--radius) px-4 py-3 items-center ${k() === 'primary' ? 'bg-primary active:opacity-80' : k() === 'outline' ? 'border border-input active:bg-accent' : 'active:bg-accent'} ${props.disabled ? 'opacity-50' : ''}`}>
      <Text class={`text-[15px] font-medium ${k() === 'primary' ? 'text-primary-foreground' : k() === 'danger' ? 'text-destructive' : 'text-foreground'}`}>{props.label}</Text>
    </View>
  )
}

export function SectionLabel(props: { label: string; right?: any }) {
  return (
    <View class="flex-row items-center justify-between px-4 pt-5 pb-1.5">
      <Text class="text-[11.5px] font-medium text-sidebar-foreground">{props.label}</Text>
      {props.right}
    </View>
  )
}

/** herdr's agent status mark (defaults/mods/agents): a spinner while working, "Needs you" when blocked (a "!" where
 * a header already says so), a blue dot when done, a grey ring when idle. */
export function StatusMark(props: { status: Shown | string | undefined; compact?: boolean; size?: number }) {
  const size = () => props.size ?? 8
  return (
    <If when={props.status === 'working'} else={
      <If when={props.status === 'blocked'} else={<StatusDot status={props.status} size={size()} />}>
        <If when={!props.compact} else={
          <View accessibilityLabel={t('needsYou')} testID="status-blocked" class="w-[15px] h-[15px] items-center justify-center rounded-[4px] bg-chart-2/15">
            <Text class="text-[10.5px] font-bold text-chart-2">!</Text>
          </View>
        }>
          <View accessibilityLabel={t('needsYou')} testID="status-blocked" class="px-1.5 h-[18px] justify-center rounded-[5px] bg-chart-2/15">
            <Text class="text-[10.5px] font-semibold text-chart-2">{t('needsYou')}</Text>
          </View>
        </If>
      </If>
    }>
      <Image src={UI.spinner} accessibilityLabel={t('working')} testID="status-working" class="tint-chart-1 animate-spin" style={{ width: size() + 6, height: size() + 6 }} />
    </If>
  )
}

/** A space's badge (defaults/mods/spaces): its first letter, white on its colour (model/marks.ts). */
export function SpaceBadge(props: { name: string; color: string; size?: number }) {
  const n = () => props.name.trim() || '?'
  const size = () => props.size ?? 18
  return (
    <View class="items-center justify-center" style={{ width: size(), height: size(), borderRadius: size() * 5 / 18, backgroundColor: props.color }}>
      <Text class="text-white font-bold" style={{ fontSize: size() * 10.5 / 18 }}>{[...n()][0].toUpperCase()}</Text>
    </View>
  )
}

export { shortPath } from './paths.ts'

// Strips of the bar's own color: maskImage draws no fade on Android (NSH 0.3.0, API 35 emulator).
const FADE = ['bg-sidebar/20', 'bg-sidebar/45', 'bg-sidebar/70', 'bg-sidebar/90']

/** A row of buttons that scrolls sideways: while more lies past its end, the end fades, so a button cut there reads as more to scroll to. */
export function ChoiceRow(props: { testID?: string; children?: any }) {
  const shown = signal(0), content = signal(0), offset = signal(0)
  const more = () => content() - offset() - shown() > 1
  return (
    <View class="flex-1 min-w-0">
      <Scroll horizontal scrollEventThrottle={16} scrollbar={false} onLayout={(b) => shown.set(b.size.width)} onScroll={(e) => offset.set(e.offset.x)}>
        <View class="flex-row gap-1.5" onLayout={(b) => content.set(b.size.width)}>{props.children}</View>
      </Scroll>
      <If when={more()}>
        <View testID={props.testID ? `${props.testID}-more` : undefined} pointerEvents="none" class="absolute right-0 top-0 bottom-0 flex-row">
          {FADE.map((c) => <View class={`w-[7px] h-full ${c}`} />)}
        </View>
      </If>
    </View>
  )
}
