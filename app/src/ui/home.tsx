// The first screen: every agent that waits on you, across the spaces, the longest wait first, with its own choices;
// then the desktop sidebar's spaces with their git and their agents, and the status bar (herdr-app defaults/mods/spaces,
// mods/agents, mods/workspace-status).
import { colorScheme, computed, Image, Scroll, signal, Text, View, type YNode } from 'nsh'
import { AlertDialog } from 'nsh/base'
import { ViewSwitch, type ViewSwitchProps } from './view-switch.tsx'
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
import { AgentName, AgentRow, agentWords } from './agent-row.tsx'
import { EdgeScroll, Hold, shifts, targetIndex, type RowBox } from './pin-drag.ts'
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
  /** With the progress page on: the task a pane works on, for its 在做 line, and opening that task. */
  doing?: (address: string) => { id: string; outcome: string } | undefined
  /** With the progress page on: the switch at the top, to the progress page and back. */
  view?: () => ViewSwitchProps | undefined
}

// The theme's ansi2 and ansi1 (herdr-app settings.rs, graphite and paper): ahead and behind.
const AHEAD = { dark: '#a6bd91', light: '#1a7f37' }
const BEHIND = { dark: '#dc8c95', light: '#c62a2a' }
const tone = () => (colorScheme() === 'dark' ? 'dark' : 'light')

function Header(props: { label: string; right?: any }) {
  return (
    <View class="flex-row items-center justify-between gap-3 h-9 pl-4 pr-2 mt-1">
      <Text class="shrink-0 text-[11.5px] font-medium text-sidebar-foreground">{props.label}</Text>
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
  const words = agentWords(s)
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
            <View class="flex-1 flex-row min-w-0"><AgentName store={s} a={props.a} class="text-[14px] font-semibold text-foreground" /></View>
            <If when={wait()}><Text class="text-[11px] text-sidebar-foreground">{wait()}</Text></If>
          </View>
          <Text class="text-[12px] text-sidebar-foreground" numberOfLines={1}>{(rev(), words.where(props.a))}</Text>
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

/** An agent row's menu: pin or unpin it on the computer, or close it. */
function agentMenu(s: Store) {
  const words = agentWords(s)
  return async (a: AgentInfo) => {
    const r = await AlertDialog.show({ ...words.menu(a), presentation: 'actionSheet', actions: [
      { id: 'pin', label: a.pinned ? t('unpin') : t('pinToTop') },
      { id: 'close', label: t('close'), role: 'destructive' }, { id: 'cancel', label: t('cancel'), role: 'cancel' }] })
    if (r === 'close') s.closePane(a.pane)
    if (r === 'pin') s.pin(a.address, !a.pinned).catch((e) => s.toast({ title: String(e?.message ?? e), level: 'error' }))
  }
}

/** Agent rows with their status words and sub-agents (ui/agent-row.tsx). Long-press: pin or unpin it on the computer,
 *  or close it. `full`: no space header above (Pinned), so the row names its space, as the desktop's Pinned rows do. */
function AgentRows(props: { store: Store; agents: () => AgentInfo[]; full?: boolean; idPrefix: string; actions: HomeActions }) {
  const s = props.store
  const menu = agentMenu(s)
  return (
    <Each of={props.agents()} key={(a) => a.address}>{(a) => (
      <View>
        <AgentRow store={s} a={a()} full={props.full} testID={`${props.idPrefix}-${a().address}`}
          class={`mx-2 ${props.full ? 'pl-2' : 'pl-9'} pr-2 min-h-9 py-1.5 justify-center rounded-[8px] active:bg-sidebar-accent`}
          subPrefix="sub" subClass={`mx-2 ${props.full ? 'pl-7' : 'pl-14'} pr-2`}
          onPress={() => props.actions.openAgent(a())} onLongPress={() => void menu(a())} onSub={props.actions.peek} />
        <If when={props.actions.doing?.(a().address)}>{(d) => (
          <View testID={`doing-${a().address}`} class={`mx-2 ${props.full ? 'pl-[30px]' : 'pl-[58px]'} pr-2 pb-1.5 -mt-1`}>
            <Text class="text-[12px] text-chart-1" numberOfLines={1}>{t('doingLine', { task: d().outcome })}</Text>
          </View>
        )}</If>
      </View>
    )}</Each>
  )
}

/** A space's agents under it; a pinned one is under Pinned instead, as on the desktop. */
function SpaceAgents(props: { store: Store; space: Space; actions: HomeActions }) {
  const rev = useRev(props.store)
  const agents = () => { rev(); return props.store.shown().agents.filter((a) => a.space === props.space.id && !a.pinned) }
  return <AgentRows store={props.store} agents={agents} idPrefix="space-agent" actions={props.actions} />
}

/** The list around Pinned, which a drag scrolls when the finger nears its top or bottom edge (HomeScreen). */
export type ListEdge = {
  /** At the lift: where the list is on screen. */
  begin: () => void
  /** The finger is at `pageY`: scrolls while it is near an edge, telling `scrolled` how far the list has gone since begin. */
  follow: (pageY: number, scrolled: (by: number) => void) => void
  end: () => void
}

/** The agents pinned on the computer (herdr-core's `herdr.pinned` label), on top, as the desktop's Pinned group, in the
 *  order they were pinned (model/pin-order.ts). Held still, a row lifts and follows the finger while the list stands
 *  still; let go, it stays where it is, on every phone. Held still and let go, its menu (ui/pin-drag.ts). */
function PinnedSection(props: { store: Store; actions: HomeActions; holding?: (on: boolean) => void; edge?: ListEdge }) {
  const s = props.store
  const rev = useRev(s)
  const words = agentWords(s)
  const menu = agentMenu(s)
  const pinned = computed(() => { rev(); return s.pinnedRows() })
  const drag = signal<{ addr: string; from: number; finger: number; scrolled: number } | null>(null)
  const dropped = signal('')
  const boxes = new Map<string, RowBox>()
  const boxList = () => pinned().map((a) => boxes.get(a.address) ?? { y: 0, h: 0 })
  const dyOf = (d: { finger: number; scrolled: number }) => d.finger + d.scrolled
  const to = () => { const d = drag(); return d ? targetIndex(boxList(), d.from, dyOf(d)) : -1 }
  const offset = (addr: string) => {
    const d = drag()
    if (!d) return 0
    if (d.addr === addr) return dyOf(d)
    return shifts(boxList(), d.from, to())[pinned().findIndex((a) => a.address === addr)] ?? 0
  }
  const end = () => { drag.set(null); props.edge?.end(); props.holding?.(false) }
  let flash: ReturnType<typeof setTimeout> | undefined
  const fail = (e: any) => s.toast({ title: String(e?.message ?? e), level: 'error' })
  const move = (a: AgentInfo, to: number) => {
    clearTimeout(flash)
    dropped.set(a.address)
    flash = setTimeout(() => dropped.set(''), 1200)
    s.movePin(a.address, to).catch(fail)
  }
  const Row = (p: { a: () => AgentInfo }) => {
    const addr = () => p.a().address
    const index = () => pinned().findIndex((x) => x.address === addr())
    const hold = new Hold({
      setTimeout: (f, ms) => setTimeout(f, ms), clearTimeout: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
      lift: () => {
        if (index() < 0) return
        dropped.set('')
        drag.set({ addr: addr(), from: index(), finger: 0, scrolled: 0 })
        props.holding?.(true)
        props.edge?.begin()
      },
      move: (dy) => { const d = drag(); if (d) drag.set({ ...d, finger: dy }) },
      drop: () => {
        const d = drag(), target = to()
        // Moved first, then let go: the row's new place shows at once (store.movePin), with no frame at the old one.
        if (d && target >= 0 && target !== d.from) move(p.a(), target)
        end()
      },
      menu: () => { end(); void menu(p.a()) },
      cancel: end,
    })
    const lifted = () => drag()?.addr === addr()
    const actions = () => [
      ...(index() > 0 ? [{ name: 'up', label: t('moveUp') }] : []),
      ...(index() < pinned().length - 1 ? [{ name: 'down', label: t('moveDown') }] : []),
      { name: 'unpin', label: t('unpin') }, { name: 'close', label: t('close') },
    ]
    const act = (e: { actionName: string }) => {
      if (e.actionName === 'up') move(p.a(), index() - 1)
      else if (e.actionName === 'down') move(p.a(), index() + 1)
      else if (e.actionName === 'unpin') s.pin(addr(), false).catch(fail)
      else if (e.actionName === 'close') s.closePane(p.a().pane)
    }
    return (
      <View onLayout={(b) => boxes.set(addr(), { y: b.origin.y, h: b.size.height })}
        style={{ transform: lifted() ? [{ translateY: offset(addr()) }, { scale: 1.03 }] : [{ translateY: offset(addr()) }], zIndex: lifted() ? 1 : 0 }}
        class={lifted() ? 'mx-2 rounded-[10px] bg-background border border-border shadow-lg' : dropped() === addr() ? 'mx-2 rounded-[8px] bg-sidebar-accent' : ''}>
        <AgentRow store={s} a={p.a()} full testID={`pinned-agent-${addr()}`}
          class="mx-2 pl-2 pr-2 min-h-9 py-1.5 justify-center rounded-[8px] active:bg-sidebar-accent"
          subPrefix="sub" subClass="mx-2 pl-7 pr-2"
          onPress={() => { if (hold.pressed()) props.actions.openAgent(p.a()) }} onSub={props.actions.peek}
          touch={{
            onStartShouldSetResponder: () => true,
            onResponderGrant: (e: { pageY: number }) => { hold.grant(e.pageY); return false },
            onResponderMove: (e: { pageY: number }) => {
              hold.move(e.pageY)
              if (lifted()) props.edge?.follow(e.pageY, (by) => { const d = drag(); if (d) drag.set({ ...d, scrolled: by }) })
            },
            onResponderRelease: () => hold.release(),
            onResponderTerminate: () => hold.terminate(),
            onResponderTerminationRequest: () => !hold.wantsToKeep(),
            accessibilityActions: actions(), onAccessibilityAction: act,
          }} />
        <If when={props.actions.doing?.(addr())}>{(dd) => (
          <View testID={`doing-${addr()}`} class="mx-2 pl-[30px] pr-2 pb-1.5 -mt-1">
            <Text class="text-[12px] text-chart-1" numberOfLines={1}>{t('doingLine', { task: dd().outcome })}</Text>
          </View>
        )}</If>
      </View>
    )
  }
  return (
    <If when={pinned().length}>
      <View>
        <Header label={t('pinned')} right={drag() ? <Text testID="pinned-hint" class="shrink pr-2 text-[11px] text-sidebar-foreground" numberOfLines={1}>{t('dragHint')}</Text> : undefined} />
        <View>
          <Each of={pinned()} key={(a) => a.address}>{(a) => <Row a={a} />}</Each>
        </View>
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
            <If when={groups().some((x) => x.machine)}><Text class="px-4 pt-2 pb-1 text-[11px] text-sidebar-foreground">{g().machine || here()}</Text></If>
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
  // While a Pinned row is held the list stands still (ui/pin-drag.ts), and scrolls itself when the finger nears an edge.
  const holding = signal(false)
  let list: YNode | undefined
  let at = 0, viewH = 0, contentH = 0, top = 0
  // Near an edge the content moves under the held row; the list scrolls there once the finger is up (ui/pin-drag.ts).
  const shown = signal(0)
  const scroller = new EdgeScroll({
    setInterval: (f, ms) => setInterval(f, ms), clearInterval: (id) => clearInterval(id as ReturnType<typeof setInterval>),
    list: () => ({ at, top, viewH, contentH }),
    show: (by) => shown.set(by),
    scrollTo: (y) => { at = y; list?.scrollTo({ y, animated: false }) },
  })
  const edge: ListEdge = {
    begin: () => { scroller.begin(); void list?.measure().then((m) => { if (m) { top = m.pagePosition.y; viewH = m.bounds.size.height } }) },
    follow: (pageY, scrolled) => scroller.follow(pageY, scrolled),
    end: () => scroller.end(),
  }
  return (
    <View class="flex-1 bg-sidebar pt-[env(safe-area-inset-top)]">
      <View class={`flex-row items-center h-12 ${props.actions.view?.() ? 'pl-3' : 'pl-4'} pr-1.5 gap-2`}>
        <If when={props.actions.view?.()} else={<>
          <Image src={icon} accessibilityLabel="Herdr Anywhere" style={{ width: 22, height: 22 }} />
          <Text class="text-[16px] font-semibold text-foreground flex-1" numberOfLines={1}>Herdr</Text>
        </>}>
          <View class="flex-1"><ViewSwitch {...props.actions.view!()!} /></View>
        </If>
        <IconButton name="plus" label={t('new')} testID="new" onPress={props.actions.newMenu} />
        <IconButton name="settings" label={t('settings')} testID="settings" onPress={props.actions.settings} />
      </View>
      <ConnBanner store={props.store} />
      <Scroll class="flex-1" contentClass="pb-6" scrollable={!holding()} ref={(n: YNode) => { list = n }} scrollEventThrottle={16}
        onScroll={(e) => { at = e.offset.y; contentH = e.contentSize.height; viewH = e.size.height }} onLayout={(b) => { viewH = b.size.height }}>
        <View onLayout={(b) => { contentH = b.size.height }} style={{ transform: [{ translateY: -shown() }] }}>
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
        <PinnedSection store={props.store} actions={props.actions} holding={(on) => holding.set(on)} edge={edge} />
        <SpacesSection store={props.store} actions={props.actions} />
        </View>
      </Scroll>
      <View class="pb-[env(safe-area-inset-bottom)] bg-sidebar"><StatusLine store={props.store} /></View>
    </View>
  )
}
