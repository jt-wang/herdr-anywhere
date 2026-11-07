// The first screen: every agent that waits on you, across the spaces, the longest wait first, with its own choices;
// then the desktop sidebar's spaces with their git and their agents, and the status bar (herdr-app defaults/mods/spaces,
// mods/agents, mods/workspace-status).
import { colorScheme, computed, Image, Scroll, signal, Text, View } from 'nsh'
import { AlertDialog } from 'nsh/base'
import type { Store } from '../store.ts'
import type { AgentInfo, SubRow } from '../model/agents.ts'
import { duration } from '../model/agents.ts'
import { asks } from '../model/answers.ts'
import { shownOrder, type Option } from '../model/menu.ts'
import type { Space } from '../model/model.ts'
import { addresses } from '../model/layout.ts'
import { gitTail } from '../model/marks.ts'
import { ChecksQuestion, UpdateBanner } from './update.tsx'
import type { Updates } from '../updates.ts'
import type { PrefStore } from '../prefs.ts'
import { ChoiceRow, ConnBanner, Glyph, IconButton, ProgramIcon, SpaceBadge, StatusMark, useRev } from './kit.tsx'
import { AgentRow, agentWords } from './agent-row.tsx'
import { t } from '../strings/index.ts'
import icon from '../../assets/brand/herdr-anywhere.png'

export type HomeActions = {
  openSpace: (spaceId: string) => void
  openAgent: (a: AgentInfo) => void
  peek: (address: string) => void
  newMenu: () => void
  /** A new tab in this space, in its folder (sheets.tsx); starting it opens the space. */
  addAgent: (spaceId: string) => void
  settings: () => void
  /** In Try it: says the data is made up, and leaves it. */
  leaveDemo?: () => void
  rename: (kind: 'space', id: string, current: string) => void
}

// The theme's ansi2 and ansi1 (herdr-app settings.rs, graphite and paper): ahead and behind.
const AHEAD = { dark: '#a6bd91', light: '#1a7f37' }
const BEHIND = { dark: '#dc8c95', light: '#c62a2a' }
const tone = () => (colorScheme() === 'dark' ? 'dark' : 'light')

function Header(props: { label: string; right?: any }) {
  return (
    <View class="flex-row items-center justify-between h-9 pl-4 pr-2 mt-1">
      <Text class="text-[11.5px] font-medium text-sidebar-foreground">{props.label}</Text>
      {props.right}
    </View>
  )
}

/** The desktop's workspace-status bar: which computer, and how many agents (how many need you). */
export function StatusLine(props: { store: Store }) {
  const rev = useRev(props.store)
  const s = props.store
  const conn = () => { rev(); return s.conn.get() }
  const counts = () => { rev(); return s.counts() }
  const problem = () => { rev(); const c = conn(); return c === 'connecting' ? t('connecting') : c !== 'ready' ? t('notConnected') : '' }
  return (
    <View role="button" testID="status-line" onPress={() => s.reconnectNow()} class="flex-row items-center gap-2 h-9 px-4 border-t border-border bg-sidebar">
      <Glyph name="monitor" size={13} />
      <Text class="text-[12px] text-muted-foreground shrink" numberOfLines={1}>{t('thisComputer')}</Text>
      <View class="flex-1" />
      <If when={!problem()} else={
        <View class="flex-row items-center gap-1.5"><View class="w-1.5 h-1.5 rounded-full bg-chart-2" /><Text class="text-[12px] text-muted-foreground" numberOfLines={1}>{problem()}</Text></View>
      }>
        <View class="flex-row items-center gap-1.5">
          <View class="w-1.5 h-1.5 rounded-full bg-sidebar-foreground" />
          <Text class="text-[12px] text-muted-foreground" numberOfLines={1}>
            {counts().agents ? t('agents', { n: counts().agents }) : t('noAgentsRunning')}
            <If when={counts().needsYou}><Text class="text-chart-2">{` · ${t('needYou', { n: counts().needsYou })}`}</Text></If>
          </Text>
        </View>
      </If>
    </View>
  )
}

/** One agent that waits on you: which, how long, where, what it asks, its own choices (model/menu.ts) and Open. */
function WaitingRow(props: { store: Store; a: AgentInfo; now: number; actions: HomeActions }) {
  const s = props.store
  const rev = useRev(s)
  const addr = props.a.address
  const status = () => { rev(); return s.term(addr)?.status }
  const menu = () => { rev(); return s.menuFor(addr) }
  const since = () => s.agentList.since(props.a)
  const wait = () => (since() ? duration(Math.max(0, props.now - since())) : '')
  const sending = signal(false)
  const open = () => props.actions.openAgent(props.a)
  const pick = async (o: Option) => {
    if (sending()) return
    sending.set(true)
    try { await s.answer(addr, { keys: o.keys }) } catch { return } finally { sending.set(false) }
    // A choice that wants text goes on in the pane, the composer ready for it.
    if (o.typed) { s.focusComposerOn(addr); open() }
  }
  return (
    <View testID={`needs-${addr}`} class="flex-row gap-2.5 mx-2 px-2 py-2">
      <View class="pt-[3px]"><ProgramIcon icon={s.program(addr).icon} size={16} /></View>
      <View class="flex-1 min-w-0 gap-[2px]">
        <View role="button" accessibilityLabel={t('waitingOpen', { program: props.a.programName, place: s.place(props.a), age: wait() })} onPress={open} class="active:opacity-70 gap-[2px]">
          <View class="flex-row items-center gap-2">
            <Text class="flex-1 text-[14px] font-semibold text-foreground" numberOfLines={1}>{(rev(), s.paneName(addr))}</Text>
            <If when={wait()}><Text class="text-[11px] text-sidebar-foreground">{wait()}</Text></If>
          </View>
          <Text class="text-[12px] text-sidebar-foreground" numberOfLines={1}>{(rev(), s.place(props.a))}</Text>
          <If when={asks(status())}><Text testID={`asks-${addr}`} class="text-[12.5px] text-muted-foreground" numberOfLines={2}>{asks(status())}</Text></If>
        </View>
        <View class="flex-row items-center gap-1.5 mt-1.5">
          <ChoiceRow testID={`needs-${addr}-choices`}>
            <If when={menu()}>{(m) => shownOrder(m()).map((o) => (
              <View role="button" testID={`needs-${addr}-choice-${o.n}`} accessibilityLabel={`${o.n}. ${o.full}`} disabled={sending()} onPress={() => void pick(o)}
                onLongPress={() => s.toast({ title: `${o.n}. ${o.full}`, level: 'info' })}
                class={`h-8 max-w-[200px] px-3 justify-center rounded-[7px] ${o.selected ? 'bg-primary active:opacity-80' : 'bg-secondary border border-border active:bg-accent'} ${sending() ? 'opacity-50' : ''}`}>
                <Text class={`text-[13px] font-medium ${o.selected ? 'text-primary-foreground' : 'text-foreground'}`} numberOfLines={1}>{`${o.n}. ${o.label}`}</Text>
              </View>
            ))}</If>
          </ChoiceRow>
          <View role="button" testID={`needs-${addr}-open`} accessibilityLabel={t('open')} onPress={open}
            class="h-8 px-3 justify-center rounded-[7px] bg-secondary border border-border active:bg-accent">
            <Text class="text-[13px] font-medium text-foreground">{t('open')}</Text>
          </View>
        </View>
      </View>
    </View>
  )
}

function NeedsYouSection(props: { store: Store; actions: HomeActions }) {
  const rev = useRev(props.store)
  const queue = computed(() => { rev(); return props.store.queue() })
  const now = () => { rev(); return Date.now() }
  return (
    <If when={queue().length}>
      <View>
        <View class="flex-row items-center h-9 px-4 mt-1 gap-1.5">
          <Text class="text-[11.5px] font-medium text-chart-2">{t('needsYou')}</Text>
          <Text class="text-[11.5px] font-medium text-sidebar-foreground">{String(queue().length)}</Text>
          <View class="flex-1" />
          <Text class="text-[11px] text-sidebar-foreground">{t('oldestFirst')}</Text>
        </View>
        <Each of={queue()} key={(a) => a.address}>{(a) => <WaitingRow store={props.store} a={a()} now={now()} actions={props.actions} />}</Each>
        <View class="h-px bg-border mx-4 mt-2" />
      </View>
    </If>
  )
}

/** Agent rows with their status words and sub-agents (ui/agent-row.tsx). Long-press: pin or unpin it on the computer,
 *  or close it. `full`: no space header above (Pinned), so the row names its space, as the desktop's Pinned rows do. */
function AgentRows(props: { store: Store; agents: () => AgentInfo[]; full?: boolean; idPrefix: string; actions: HomeActions }) {
  const s = props.store
  const words = agentWords(s)
  const menu = async (a: AgentInfo) => {
    const r = await AlertDialog.show({ ...words.menu(a), presentation: 'actionSheet', actions: [
      { id: 'pin', label: a.pinned ? t('unpin') : t('pinToTop') },
      { id: 'close', label: t('close'), role: 'destructive' }, { id: 'cancel', label: t('cancel'), role: 'cancel' }] })
    if (r === 'close') s.closePane(a.pane)
    if (r === 'pin') s.pin(a.address, !a.pinned).catch((e) => s.toast({ title: String(e?.message ?? e), level: 'error' }))
  }
  return (
    <Each of={props.agents()} key={(a) => a.address}>{(a) => (
      <AgentRow store={s} a={a()} full={props.full} testID={`${props.idPrefix}-${a().address}`}
        class={`mx-2 ${props.full ? 'pl-2' : 'pl-9'} pr-2 min-h-9 py-1.5 justify-center rounded-[8px] active:bg-sidebar-accent`}
        subPrefix="sub" subClass={`mx-2 ${props.full ? 'pl-7' : 'pl-14'} pr-2`}
        onPress={() => props.actions.openAgent(a())} onLongPress={() => void menu(a())} onSub={props.actions.peek} />
    )}</Each>
  )
}

/** A space's agents under it; a pinned one is under Pinned instead, as on the desktop. */
function SpaceAgents(props: { store: Store; space: Space; actions: HomeActions }) {
  const rev = useRev(props.store)
  const agents = () => { rev(); return props.store.agents().filter((a) => a.space === props.space.id && !a.pinned) }
  return <AgentRows store={props.store} agents={agents} idPrefix="space-agent" actions={props.actions} />
}

/** The agents pinned on the computer (herdr-core's `herdr.pinned` label), on top, as the desktop's Pinned group. */
function PinnedSection(props: { store: Store; actions: HomeActions }) {
  const rev = useRev(props.store)
  const pinned = computed(() => { rev(); return props.store.agentsView().groups.find((g) => g.id === 'pinned')?.members ?? [] })
  return (
    <If when={pinned().length}>
      <View>
        <Header label={t('pinned')} />
        <AgentRows store={props.store} agents={pinned} full idPrefix="pinned-agent" actions={props.actions} />
      </View>
    </If>
  )
}

function SpaceRow(props: { store: Store; space: Space; color: string; actions: HomeActions }) {
  const s = props.store
  const rev = useRev(s)
  const name = () => { rev(); return s.spaceName(props.space) }
  const status = () => { rev(); return s.spaceStatus(props.space) }
  const git = () => {
    rev()
    for (const c of props.space.tabs) for (const a of addresses(c.root)) { const f = s.model.folderInfo.get(a); if (f?.branch) return f }
    return undefined
  }
  const active = () => { rev(); return s.model.activeSpace()?.id === props.space.id }
  const menu = async () => {
    const r = await AlertDialog.show({ title: name(), presentation: 'actionSheet', actions: [
      { id: 'rename', label: t('rename') }, { id: 'close', label: t('closeSpace'), role: 'destructive' }, { id: 'cancel', label: t('cancel'), role: 'cancel' }] })
    if (r === 'rename') props.actions.rename('space', props.space.id, props.space.name)
    else if (r === 'close') {
      const ok = await AlertDialog.show({ title: t('closeTitle', { name: name() }), description: t('closeDetail'), actions: [
        { id: 'cancel', label: t('cancel'), role: 'cancel' }, { id: 'close', label: t('close'), role: 'destructive' }] })
      if (ok === 'close') s.closeSpace(props.space.id)
    }
  }
  return (
    <View role="button" testID={`space-${props.space.id}`} accessibilityLabel={name()} onPress={() => props.actions.openSpace(props.space.id)} onLongPress={menu}
      class={`flex-row items-center gap-2.5 mx-2 pl-2 h-11 rounded-[8px] active:bg-sidebar-accent ${active() ? 'bg-accent' : ''}`}>
      <SpaceBadge name={name()} color={props.color} />
      <Text class="text-[14px] text-foreground shrink-0 max-w-[60%]" numberOfLines={1}>{name()}</Text>
      <View class="flex-1 flex-row items-center justify-end gap-1.5 min-w-0">
        <If when={git()}>{(g) => (
          <View class="flex-row items-center gap-1.5 min-w-0">
            <If when={gitTail(name(), g())}><Text class="text-[12px] text-sidebar-foreground shrink" numberOfLines={1}>{gitTail(name(), g())}</Text></If>
            <If when={g().ahead}><Text class="text-[12px]" style={{ color: AHEAD[tone()] }}>{`↑${g().ahead}`}</Text></If>
            <If when={g().behind}><Text class="text-[12px]" style={{ color: BEHIND[tone()] }}>{`↓${g().behind}`}</Text></If>
          </View>
        )}</If>
        <If when={status() === 'blocked' || status() === 'done'}><StatusMark status={status()} compact size={7} /></If>
      </View>
      <IconButton name="plus" label={t('addAgent')} testID={`add-agent-${props.space.id}`} onPress={() => props.actions.addAgent(props.space.id)} />
    </View>
  )
}

function SpacesSection(props: { store: Store; actions: HomeActions }) {
  const rev = useRev(props.store)
  const groups = computed(() => {
    rev()
    const by = new Map<string, Space[]>()
    for (const sp of props.store.model.spaces) {
      const k = sp.machine ?? ''
      by.set(k, [...(by.get(k) ?? []), sp])
    }
    return [...by.entries()].map(([machine, spaces]) => ({ machine, spaces }))
  })
  const marks = computed(() => { rev(); return props.store.spaceMarks() })
  const here = () => { rev(); return props.store.hello?.core?.name ?? t('thisComputer') }
  const add = (
    <View role="button" testID="space-new" accessibilityLabel={t('newSpace')} onPress={props.actions.newMenu} class="w-9 h-8 items-center justify-center rounded-[6px] active:bg-sidebar-accent">
      <Glyph name="plus" size={14} />
    </View>
  )
  return (
    <View>
      <Header label={t('spaces')} right={add} />
      <If when={groups().length} else={
        <View role="button" testID="first-space" onPress={props.actions.newMenu} class="mx-4 my-2 rounded-[8px] border border-dashed border-border p-4 items-center active:bg-sidebar-accent">
          <Text class="text-[14px] text-foreground">{t('startHint')}</Text>
          <Text class="text-[12px] text-sidebar-foreground mt-1">{t('startHintDetail')}</Text>
        </View>
      }>
        <Each of={groups()} key={(g) => g.machine}>{(g) => (
          <View>
            <If when={groups().length > 1}><Text class="px-4 pt-2 pb-1 text-[11px] text-sidebar-foreground">{g().machine || here()}</Text></If>
            <Each of={g().spaces} key={(sp) => sp.id}>{(sp) => (
              <View>
                <SpaceRow store={props.store} space={sp()} color={marks()[sp().id]} actions={props.actions} />
                <SpaceAgents store={props.store} space={sp()} actions={props.actions} />
              </View>
            )}</Each>
          </View>
        )}</Each>
      </If>
    </View>
  )
}

export function HomeScreen(props: { store: Store; actions: HomeActions; updates?: Updates; prefs?: PrefStore }) {
  return (
    <View class="flex-1 bg-sidebar pt-[env(safe-area-inset-top)]">
      <View class="flex-row items-center h-12 pl-4 pr-1.5 gap-2">
        <Image src={icon} accessibilityLabel="Herdr Anywhere" style={{ width: 22, height: 22 }} />
        <Text class="text-[16px] font-semibold text-foreground flex-1" numberOfLines={1}>Herdr</Text>
        <IconButton name="plus" label={t('new')} testID="new" onPress={props.actions.newMenu} />
        <IconButton name="settings" label={t('settings')} testID="settings" onPress={props.actions.settings} />
      </View>
      <ConnBanner store={props.store} />
      <Scroll class="flex-1" contentClass="pb-6">
        <If when={props.updates && props.prefs}><ChecksQuestion updates={props.updates!} prefs={props.prefs!} /><UpdateBanner updates={props.updates!} prefs={props.prefs!} /></If>
        <If when={props.actions.leaveDemo}>
          <View testID="demo-notice" class="mx-3 mt-1 mb-1 flex-row items-center gap-3 rounded-(--radius) bg-muted pl-3 pr-1 py-1">
            <Text class="flex-1 text-[13px] leading-[18px] text-muted-foreground">{t('demoNotice')}</Text>
            <View role="button" testID="demo-leave" onPress={() => props.actions.leaveDemo!()} class="px-3 py-2 rounded-[6px] active:bg-accent">
              <Text class="text-[13px] font-medium text-foreground">{t('demoLeave')}</Text>
            </View>
          </View>
        </If>
        <NeedsYouSection store={props.store} actions={props.actions} />
        <PinnedSection store={props.store} actions={props.actions} />
        <SpacesSection store={props.store} actions={props.actions} />
      </Scroll>
      <View class="pb-[env(safe-area-inset-bottom)] bg-sidebar"><StatusLine store={props.store} /></View>
    </View>
  )
}
