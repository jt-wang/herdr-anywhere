// One agent's row, as home, Pinned and the pane's switcher show it (herdr-app defaults/mods/agents): what tells it apart
// (its tab's name or its title) as the main text, the program's name small and grey before it (Settings can hide it:
// the icon says it too), its status in words, what it is doing or did a step under the title (smaller, regular, the title's colour at 80 %),
// and its sub-agents. An idle row is a step dimmer as a whole.
import { colorScheme, computed, Text, View } from 'nsh'
import type { Store } from '../store.ts'
import type { AgentInfo } from '../model/agents.ts'
import { listOf } from '../strings/index.ts'
import { ProgramIcon, StatusMark, statusLabel, useRev } from './kit.tsx'

export const STATUS_TEXT: Record<string, string> = { working: 'text-chart-1', blocked: 'text-chart-2', done: 'text-chart-3', idle: 'text-chart-4' }
/** What an agent is doing: the theme's foreground at about 80 % over the sidebar (#eeeeef on #101114, #1b1b1e on
 *  #f1f1ee), a step under the title and well above the greys. NSH has no `text-foreground/80`: it draws black. */
export const DOING = { dark: '#c4c5c7', light: '#3f3f42' }
/** A status word's colour. Idle's grey (chart-4) is too pale to read on the light theme: there it is muted-foreground. */
export const statusText = (st: string | undefined) => (st === 'idle' && colorScheme() !== 'dark' ? 'text-muted-foreground' : STATUS_TEXT[st ?? ''] ?? 'text-sidebar-foreground')

/** What a row says about an agent besides its program: the same words on every screen that lists it. */
export function agentWords(s: Store) {
  const rev = useRev(s)
  const view = computed(() => { rev(); return s.shown().view })
  // The tab's name or the agent's title, when it says more than the program's name.
  const title = (a: AgentInfo) => { const d = view().detail.get(a.address); return d && d !== a.programName ? d : null }
  // What it is doing or did. "Needs you" already shows what a blocked one asks.
  const line = (a: AgentInfo) => { rev(); const l = s.activityOf(a.address); return a.status === 'blocked' || l === title(a) ? '' : l }
  const subs = (a: AgentInfo) => { rev(); return view().subs.get(a.address) ?? [] }
  // The row's main text: what tells it apart, or with nothing to tell, the program's name.
  const main = (a: AgentInfo) => title(a) ?? a.programName
  // The program's name goes before the main text unless Settings turned it off, or it already is the main text.
  const named = (a: AgentInfo) => !!title(a) && s.showNames()
  // A menu on the row: titled with its main text, its space and (when the row shows it) its program under it.
  const menu = (a: AgentInfo) => ({ title: main(a), description: [a.space_name, ...(named(a) ? [a.programName] : [])].join(' · ') })
  // Where it is ("space › tab"), without the tab when the main text already is the tab's name.
  const where = (a: AgentInfo) => { const p = s.place(a), tail = ` › ${main(a)}`; return p.endsWith(tail) ? p.slice(0, -tail.length) : p }
  // The agent in a terminal, if one runs there.
  const at = (addr: string | undefined) => { rev(); return addr ? s.shown().agents.find((a) => a.address === addr) : undefined }
  return { title, line, subs, main, named, menu, where, at }
}

/** An agent's name as its row says it, on one line: the program's name small and grey (unless Settings hides it), then
 *  the main text in `class`. For headers and rows that lay out the rest themselves. */
export function AgentName(props: { store: Store; a: AgentInfo; class: string; testID?: string }) {
  const words = agentWords(props.store)
  return (
    <View testID={props.testID} class="flex-row items-center gap-1.5 shrink min-w-0">
      <If when={words.named(props.a)}><Text class="shrink-0 text-[12.5px] text-sidebar-foreground" numberOfLines={1}>{props.a.programName}</Text></If>
      <Text class={`shrink ${props.class}`} numberOfLines={1}>{words.main(props.a)}</Text>
    </View>
  )
}

/** An agent's row and its sub-agents' rows under it. The screen gives the row's frame (`class`, which lays out the
 *  padding, height and highlight), its test ids and what a press does. `full`: no space header above the row (Pinned,
 *  other spaces), so it names its space first. */
export function AgentRow(props: {
  store: Store; a: AgentInfo; full?: boolean; testID: string; class: string; iconSize?: number
  subClass: string; subPrefix: string; onPress: () => void; onLongPress?: () => void; onSub: (address: string) => void
}) {
  const s = props.store
  const words = agentWords(s)
  const a = () => props.a
  const size = () => props.iconSize ?? 14
  const idle = () => a().status === 'idle'
  // The program's name before the title, unless Settings turned it off; with no title, the name is the row's main text.
  const named = () => words.named(a())
  return (
    <View>
      <View role="button" testID={props.testID}
        accessibilityLabel={listOf(a().programName, `${a().space_name}${words.title(a()) ? ` · ${words.title(a())}` : ''}`, statusLabel(a().status))}
        onPress={props.onPress} onLongPress={props.onLongPress} class={props.class}>
        <View class="flex-row items-center gap-2">
          <View style={{ opacity: idle() ? (colorScheme() === 'dark' ? 0.55 : 0.7) : 1 }}><ProgramIcon icon={s.program(a().address).icon} size={size()} /></View>
          <If when={props.full}><Text class="shrink-0 max-w-[30%] text-[12.5px] text-sidebar-foreground" numberOfLines={1}>{a().space_name}</Text></If>
          <If when={named()}><Text class="shrink-0 text-[12.5px] text-sidebar-foreground" numberOfLines={1}>{a().programName}</Text></If>
          <Text class={`flex-1 text-[14.5px] font-medium ${idle() ? 'text-muted-foreground' : 'text-foreground'}`} numberOfLines={1}>{words.main(a())}</Text>
          <Text class={`text-[12px] ${statusText(a().status)}`}>{statusLabel(a().status)}</Text>
        </View>
        <If when={words.line(a())}>
          <Text testID={`activity-${a().address}`} class={`text-[13px] ${idle() ? 'text-muted-foreground' : ''}`}
            style={{ paddingLeft: size() + 8, ...(idle() ? {} : { color: DOING[colorScheme() === 'dark' ? 'dark' : 'light'] }) }} numberOfLines={1}>{words.line(a())}</Text>
        </If>
      </View>
      {/* Kept by terminal: each change makes new sub-agent entries, and a row drawn again under the finger lost its press. */}
      <Each of={words.subs(a())} key={(k) => k.term.address}>{(k) => (
        <View role="button" testID={`${props.subPrefix}-${k().term.address}`} onPress={() => props.onSub(k().term.address)} class={`flex-row items-center gap-2 h-8 rounded-[8px] active:bg-sidebar-accent ${props.subClass}`}>
          <Text class="text-[12px] text-sidebar-foreground font-mono">{k().lines.join(' ')}</Text>
          <ProgramIcon icon={s.program(k().term.address).icon} size={13} />
          <Text class="text-[13px] text-muted-foreground flex-1" numberOfLines={1}>{s.terminalName(k().term)}</Text>
          <StatusMark status={k().status} compact size={6} />
        </View>
      )}</Each>
    </View>
  )
}
