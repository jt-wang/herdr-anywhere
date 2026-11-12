// A space. Wide screens draw it as the desktop does: the tab bar, the panes on the canvas, the status bar (herdr-app
// defaults/mods/tabs, mods/pane, mods/workspace-status). A phone gives the screen to the focused pane: a two-line
// header (the agent, where it is, Needs you), its terminal edge to edge, and one row under it (ui/pane-bar.tsx); the
// title opens the switcher, and a flick across the terminal goes to the next or previous tab.
import { cleanup, computed, effect, fontScale, keyboardFrame, safeAreaInsets, Scroll, signal, Text, View, windowSize } from 'nsh'
import { AlertDialog, Sheet } from 'nsh/base'
import { keyboardGone } from './keyboard.ts'
import type { Store } from '../store.ts'
import { layoutRects } from '../store.ts'
import { addresses, find, panes, terminalOf, type Node } from '../model/layout.ts'
import { focusedPaneOf, shownNode, spaceActiveTab, type Tab } from '../model/model.ts'
import { gitTail } from '../model/marks.ts'
import { ConnBanner, Glyph, IconButton, ProgramIcon, SpaceBadge, StatusDot, StatusMark, shortPath, useRev } from './kit.tsx'
import { t } from '../strings/index.ts'
import { TerminalView, WIDE } from './terminal.tsx'
import { attachToTerminal, KEY_ROW_MAX_SCALE, PaneBar } from './pane-bar.tsx'
import { AgentName, AgentRow, agentWords } from './agent-row.tsx'
import { headerChipTight, showsAWord } from './header-fit.ts'
import { startable } from '../model/programs.ts'
import type { TerminalSession } from './terminal-session.ts'
import type { TypeIn } from '../prefs.ts'

export type SpaceActions = {
  back: () => void
  newTab: (spaceId: string) => void
  /** A new tab in the space's own folder: the folder line in the sheet still changes it. */
  addAgent: (spaceId: string) => void
  rename: (kind: 'space' | 'tab', id: string, current: string) => void
  fontSize: () => number
  /** Where a pane opens typing: the phone's last kept choice. */
  typeIn: () => TypeIn
  keepTypeIn: (m: TypeIn) => void
  /** Opens a terminal wherever it is (another space included). */
  open: (address: string) => void
  settings: () => void
}

/** The program a tab is shown with: its agent's, else its first terminal's (defaults/mods/tabs). */
function tabIcon(s: Store, c: Tab) {
  const all = addresses(c.root)
  return s.program(all.find((a) => s.program(a).agent) ?? all[0]).icon
}

function TabBar(props: { store: Store; spaceId: string; actions: SpaceActions }) {
  const s = props.store
  const rev = useRev(s)
  const space = () => { rev(); return s.model.space(props.spaceId) }
  const active = () => { rev(); const sp = space(); return sp && spaceActiveTab(sp)?.id }
  const menu = async (c: Tab) => {
    const r = await AlertDialog.show({ title: s.tabLabel(space()!, c), presentation: 'actionSheet', actions: [
      { id: 'rename', label: t('rename') }, { id: 'close', label: t('closeTab'), role: 'destructive' }, { id: 'cancel', label: t('cancel'), role: 'cancel' }] })
    if (r === 'rename') props.actions.rename('tab', c.id, c.name)
    else if (r === 'close') s.closeTab(c.id)
  }
  return (
    <Scroll horizontal class="grow-0 bg-sidebar border-b border-border" contentClass="flex-row items-center gap-[3px] px-2 py-[5px]" scrollbar={false}>
      <Each of={space()?.tabs ?? []} key={(c) => c.id}>{(c) => {
        const on = () => active() === c().id
        const st = () => { rev(); return s.tabStatus(c()) }
        return (
          <View role="button" testID={`tab-${c().id}`} accessibilityLabel={s.tabLabel(space()!, c())} onPress={() => s.selectTab(c().id)} onLongPress={() => void menu(c())}
            class={`flex-row items-center gap-[7px] h-[29px] px-2.5 rounded-[7px] border ${on() ? 'bg-secondary border-border' : 'border-transparent active:bg-sidebar-accent'}`}>
            <ProgramIcon icon={(rev(), tabIcon(s, c()))} size={14} />
            <Text class={`text-[13px] max-w-[170px] ${on() ? 'text-foreground' : 'text-muted-foreground'}`} numberOfLines={1}>{(rev(), s.tabLabel(space()!, c()))}</Text>
            <If when={st() === 'working'}><StatusMark status="working" size={6} /></If>
            <If when={st() === 'blocked' || st() === 'done'}><StatusDot status={st()} size={6} /></If>
            <If when={on()}>
              <View role="button" testID={`close-tab-${c().id}`} accessibilityLabel={t('closeTab')} onPress={() => s.closeTab(c().id)} class="-mr-1 w-5 h-5 items-center justify-center rounded-[4px] active:bg-sidebar-accent">
                <Glyph name="close" size={12} class="tint-sidebar-foreground" />
              </View>
            </If>
          </View>
        )
      }}</Each>
      <View role="button" testID="new-tab" accessibilityLabel={t('newTab')} onPress={() => props.actions.newTab(props.spaceId)} class="w-[29px] h-[29px] items-center justify-center rounded-[7px] active:bg-sidebar-accent">
        <Glyph name="plus" size={14} />
      </View>
    </Scroll>
  )
}

function PaneHeader(props: { store: Store; node: Node; tab: Tab; focused: boolean; narrow: boolean; split: (pane: string, direction: 'right' | 'down') => void }) {
  const s = props.store
  const rev = useRev(s)
  const words = agentWords(s)
  const addr = () => terminalOf(props.node)
  const name = () => { rev(); const a = addr(); return a ? s.paneName(a) : (() => { const c = props.node.content; return c && 'mod' in c ? c.mod : t('panel') })() }
  const cwd = () => { rev(); const t = s.term(addr()); return t?.cwd ? shortPath(t.cwd, s.home) : '' }
  const doing = () => { rev(); return s.activityOf(addr()) }
  const zoomed = () => { rev(); return props.tab.zoomed === props.node.id }
  const many = () => { rev(); return panes(props.tab.root).length > 1 }
  return (
    <View class="flex-row items-center h-[34px] pl-2.5 pr-0.5 gap-2 border-b border-border bg-background">
      <ProgramIcon icon={(rev(), s.program(addr()).icon)} size={14} />
      <View testID={`pane-name-${props.node.id}`} class="shrink-0 max-w-[45%]">
        <If when={words.at(addr())} else={<Text class="text-[12.5px] font-medium text-foreground" numberOfLines={1}>{name()}</Text>}>
          {(a) => <AgentName store={s} a={a()} class="text-[12.5px] font-medium text-foreground" />}
        </If>
      </View>
      <Text testID={`cwd-${props.node.id}`} class="flex-1 text-[12.5px] text-sidebar-foreground" numberOfLines={1} ellipsizeMode={doing() ? 'tail' : 'head'} accessibilityLabel={doing() || cwd()}>{props.narrow ? '' : doing() || cwd()}</Text>
      <If when={(rev(), s.statusOf(addr()))}>{(st) => <StatusMark status={st()} size={7} />}</If>
      <If when={!props.narrow}>
        <SmallButton name="split-right" label={t('splitRight')} testID={`split-right-${props.node.id}`} onPress={() => props.split(props.node.id, 'right')} />
        <SmallButton name="split-down" label={t('splitDown')} testID={`split-down-${props.node.id}`} onPress={() => props.split(props.node.id, 'down')} />
      </If>
      <If when={many()}><SmallButton name={zoomed() ? 'unzoom' : 'zoom'} label={zoomed() ? t('unzoom') : t('zoom')} testID={`zoom-${props.node.id}`} onPress={() => s.zoom(props.node.id)} /></If>
      <SmallButton name="close" label={t('closePane')} testID={`close-${props.node.id}`} onPress={() => s.closePane(props.node.id)} />
    </View>
  )
}

function SmallButton(props: { name: Parameters<typeof Glyph>[0]['name']; label: string; testID: string; onPress: () => void }) {
  return (
    <View role="button" accessibilityLabel={props.label} testID={props.testID} onPress={props.onPress} class="w-8 h-8 items-center justify-center rounded-[6px] active:bg-accent">
      <Glyph name={props.name} size={14} class="tint-sidebar-foreground" />
    </View>
  )
}

function PaneBody(props: { store: Store; spaceId: string; node: Node; actions: SpaceActions; keys: boolean; onSession?: (s: TerminalSession) => void; tap?: () => void }) {
  const addr = terminalOf(props.node)
  return addr
    ? <TerminalView store={props.store} address={addr} fontSize={props.actions.fontSize()} keys={props.keys} onSession={props.onSession}
        swipe={(d) => props.store.stepTab(props.spaceId, d)} tap={props.tap} />
    : <View class="flex-1 items-center justify-center p-6"><Text class="text-[13px] text-sidebar-foreground text-center">{t('panelMod')}</Text></View>
}

/** A pane's frame on the canvas: rounded, its border the accent while it has focus (defaults/frame). */
const frame = (focused: boolean) => `rounded-[10px] overflow-hidden border ${focused ? 'border-chart-5' : 'border-border'} bg-background`

/** A phone: the focused pane, its header above it and its row under it. */
function PhoneSpace(props: { store: Store; spaceId: string; tab: Tab; actions: SpaceActions; sessions: Map<string, TerminalSession>; back: () => void; menu: () => void; split: (pane: string, direction: 'right' | 'down') => void }) {
  const s = props.store
  const rev = useRev(s)
  const node = () => { rev(); const f = focusedPaneOf(props.tab); return f ? find(props.tab.root, f) : undefined }
  const session = signal<TerminalSession | undefined>(undefined)
  const switching = signal(false)
  const whole = () => { rev(); return s.shownWhole.has(props.tab.id) && panes(props.tab.root).length > 1 }
  return (
    <View class="flex-1 bg-background">
      <PhoneHeader store={s} spaceId={props.spaceId} tab={props.tab} node={node()} actions={props.actions} back={props.back} menu={props.menu}
        openSwitcher={() => void keyboardGone().then(() => switching.set(true))} />
      <ConnBanner store={s} />
      <If when={whole()} else={
        <View class="flex-1">
          {/* keyed by pane: another pane is another terminal */}
          <Each of={node() ? [node()!] : []} key={(n) => n.id}>{(n) => (
            <PaneBody store={s} spaceId={props.spaceId} node={n()} actions={props.actions} keys={false}
              onSession={(t) => { session.set(t); props.sessions.set(t.address, t); cleanup(() => { if (session() === t) session.set(undefined); props.sessions.delete(t.address) }) }} />
          )}</Each>
          <Each of={session() ? [session()!] : []}>{(t) => {
            const p = () => { rev(); return s.program(t().address) }
            return <PaneBar store={s} session={t()} program={p().name} agent={!!p().agent} typeIn={props.actions.typeIn} keep={props.actions.keepTypeIn} />
          }}</Each>
        </View>
      }>
        <WidePanes store={s} spaceId={props.spaceId} tab={props.tab} actions={props.actions} sessions={props.sessions} split={props.split} />
      </If>
      <Switcher store={s} spaceId={props.spaceId} open={switching} actions={props.actions} />
    </View>
  )
}

/** The most the header's text grows with the system's text size: it has a fixed height and both lines stay whole at
 *  1.3, as the key row's labels do (checked at Android's largest size, font scale 2.0, at 360 dp). */
const HEADER_MAX_SCALE = KEY_ROW_MAX_SCALE

/** Two short lines: the agent and its status (the title opens the switcher), then where it is; +N waiting and ⋯. */
function PhoneHeader(props: { store: Store; spaceId: string; tab: Tab; node: Node | undefined; actions: SpaceActions; back: () => void; menu: () => void; openSwitcher: () => void }) {
  const s = props.store
  const rev = useRev(s)
  const addr = () => (props.node ? terminalOf(props.node) : undefined)
  const words = agentWords(s)
  // An agent by what home's row leads with; anything else by its pane's name.
  const name = () => { rev(); const a = addr(), ag = words.at(a); return ag ? words.main(ag) : a ? s.paneName(a) : t('panel') }
  const status = () => { rev(); return s.statusOf(addr()) }
  const place = () => {
    rev(); const ag = words.at(addr()); if (ag) return words.where(ag)
    const sp = s.model.space(props.spaceId); return sp ? `${s.spaceName(sp)} › ${s.tabLabel(sp, props.tab)}` : ''
  }
  // The others waiting on you, the longest wait first.
  const waiting = () => { rev(); return s.queue().filter((a) => a.address !== addr()) }
  const activity = () => { rev(); return s.activityOf(addr()) || place() }
  // Line 2 drops the "·" and what the agent is doing once the platform laid out less than its first word ("· …"). It
  // tries again when the line's width or the words change; until then the program's name gets the room.
  const lineWidth = signal(0)
  const cutFor = signal('')
  const cut = () => cutFor() === `${lineWidth()}|${activity()}`
  let activityWidth: number | undefined
  const tight = () => headerChipTight(fontScale())
  return (
    <View testID="pane-header" class="flex-row items-center h-[46px] pl-0.5 pr-1 gap-0.5 bg-sidebar border-b border-border">
      <IconButton name="chevron-left" label={t('back')} testID="back" onPress={props.back} />
      <View role="button" testID="switcher" accessibilityLabel={t('switchPane', { name: name(), place: place() })} onPress={props.openSwitcher} class="flex-1 min-w-0 justify-center gap-[1px] active:opacity-70">
        <View class={`flex-row items-center ${tight() ? 'gap-1' : 'gap-1.5'}`}>
          {/* The title in a chip, as the tab bar draws a tab: it reads as this pane's name, not as words to act on. At the
              largest text sizes the chip keeps its colour but loses its border and most of its padding, so the title
              keeps the room it had without one. */}
          <View testID="pane-title" class={`flex-row items-center shrink min-w-0 rounded-[6px] bg-secondary ${tight() ? 'gap-1 pl-1 pr-0.5' : 'gap-1.5 pl-1.5 pr-1 border border-border'}`}>
            <ProgramIcon icon={(rev(), s.program(addr()).icon)} size={15} />
            <Text maxFontSizeMultiplier={HEADER_MAX_SCALE} class="text-[14.5px] font-semibold text-foreground shrink" numberOfLines={1}>{name()}</Text>
            <Glyph name="chevron-down" size={13} />
          </View>
          <If when={status() === 'blocked'} else={<If when={status()}>{(st) => <StatusMark status={st()} size={7} />}</If>}>
            <StatusDot status="blocked" size={7} />
          </If>
        </View>
        {/* Line 2: Needs you (a pill), the program's name (unless Settings hides it, as on home's row), then what it is doing, which
            gives way first when the line is short. Line 1 is the title alone, so a narrow phone has room for it beside
            +N waiting. */}
        <View class="flex-row items-center gap-1 min-w-0 overflow-hidden" onLayout={(b) => lineWidth.set(Math.round(b.size.width))}>
          <If when={status() === 'blocked'}>
            {/* A pill, as +N waiting is, so it stands apart from the title above and the grey words after it. */}
            <View testID="header-needs-you" class="shrink-0 mr-1 px-1.5 py-px rounded-[5px] bg-chart-2/15">
              <Text maxFontSizeMultiplier={HEADER_MAX_SCALE} class="text-[11px] font-medium text-chart-2" numberOfLines={1}>{t('needsYou')}</Text>
            </View>
          </If>
          <If when={(rev(), words.at(addr()) && words.named(words.at(addr())!))}>
            <Text maxFontSizeMultiplier={HEADER_MAX_SCALE} testID="pane-program" class="shrink text-[11.5px] text-sidebar-foreground" numberOfLines={1}>{(rev(), words.at(addr())?.programName ?? '')}</Text>
            <If when={!cut()}>
              <Text maxFontSizeMultiplier={HEADER_MAX_SCALE} class="text-[11.5px] text-sidebar-foreground">·</Text>
            </If>
          </If>
          <If when={!cut()}>
            <Text maxFontSizeMultiplier={HEADER_MAX_SCALE} testID="pane-activity" class="text-[11.5px] text-sidebar-foreground" style={{ flexShrink: 100 }} numberOfLines={1}
              onLayout={(b) => { activityWidth = b.size.width; if (!showsAWord(activity(), [{ text: activity() }], activityWidth)) cutFor.set(`${lineWidth()}|${activity()}`) }}
              onTextLayout={(e) => { if (!showsAWord(activity(), e.lines, activityWidth)) cutFor.set(`${lineWidth()}|${activity()}`) }}>{activity()}</Text>
          </If>
        </View>
      </View>
      <If when={waiting().length}>
        <View role="button" testID="next-waiting" accessibilityLabel={t('moreWaitingOpen', { n: waiting().length })} onPress={() => props.actions.open(waiting()[0].address)}
          class="px-2 h-7 justify-center rounded-[6px] bg-chart-2/15 active:opacity-70">
          <Text maxFontSizeMultiplier={HEADER_MAX_SCALE} class="text-[11.5px] font-medium text-chart-2">{t('plusWaiting', { n: waiting().length })}</Text>
        </View>
      </If>
      <IconButton name="plus" label={t('addAgent')} testID="add-agent" onPress={() => void keyboardGone().then(() => props.actions.addAgent(props.spaceId))} />
      <IconButton name="more" label={t('paneActions')} testID="space-more" onPress={props.menu} />
    </View>
  )
}

/** This space's tabs (and the panes of a split one), the agents of the other spaces, and new agents. An agent's row
 *  says what home's does: what tells it apart, its status in words, what it is doing, its sub-agents. */
function Switcher(props: { store: Store; spaceId: string; open: { (): boolean; set: (v: boolean) => void }; actions: SpaceActions }) {
  const s = props.store
  const rev = useRev(s)
  const space = () => { rev(); return s.model.space(props.spaceId) }
  const active = () => { rev(); const sp = space(); return sp && spaceActiveTab(sp)?.id }
  const others = () => { rev(); return s.shown().agents.filter((a) => a.space !== props.spaceId) }
  const fresh = () => {
    rev()
    const have = s.installed.get('')
    return [...startable().filter((p) => p.agent && (!have || have.includes(p.run![0]))).map((p) => ({ key: p.key, name: p.name, icon: p.icon, argv: p.run })),
      { key: 'shell', name: t('shell'), icon: 'terminal', argv: undefined as string[] | undefined }]
  }
  const close = () => props.open.set(false)
  // NSH 0.3.0 on Android sizes a sheet's scroll content far past its rows (measured: 2743×2378 px around rows
  // laid out at 1016×740, read with dumpsys activity), so a drag scrolled them out of sight and left the sheet blank.
  // Rows that fit don't scroll at all; rows that don't stop at the last one.
  const viewport = signal(0)
  const rows = signal(0)
  let scroller: any
  const clamp = (y: number) => { const end = Math.max(0, rows() - viewport()); if (y > end + 1) scroller?.scrollTo({ y: end, animated: false }) }
  const row = 'flex-row items-center gap-2.5 h-11 px-2 rounded-[8px] active:bg-sidebar-accent'
  // An agent's row is home's (ui/agent-row.tsx), as tall as its second line needs.
  const agentRow = 'min-h-11 py-1.5 px-2 justify-center rounded-[8px] active:bg-sidebar-accent'
  const agentAt = (addr: string | undefined) => { rev(); return addr ? s.shown().agents.find((a) => a.address === addr) : undefined }
  // A sub-agent has no pane of its own: open shows what it prints.
  const sub = (addr: string) => { close(); props.actions.open(addr) }
  return (
    <Sheet open={props.open()} side="bottom" onOpenChange={(o) => props.open.set(o)} popupClass="bg-popover rounded-t-[14px] px-3 pt-3 pb-[env(safe-area-inset-bottom)]">
      <Scroll ref={(r: any) => { scroller = r }} class="max-h-[520px]" scrollable={rows() > viewport() + 1} onLayout={(b) => viewport.set(b.size.height)}
        scrollEventThrottle={16} onScroll={(e) => clamp(e.offset.y)} onMomentumEnd={(e) => clamp(e.y)} onScrollDragEnd={(e) => clamp(e.y)}>
      <View class="pb-2" onLayout={(b) => rows.set(b.size.height)}>
        <Text class="px-2 pb-1 text-[11.5px] font-medium text-sidebar-foreground">{(rev(), space() ? s.spaceName(space()!) : '')}</Text>
        <Each of={space()?.tabs ?? []} key={(c) => c.id}>{(c) => {
          const go = () => { s.selectTab(c().id); close() }
          const on = () => active() === c().id
          // A tab of one agent reads as that agent's row on home; a shell, a split tab or a panel as the tab.
          const lone = () => { rev(); const ps = panes(c().root); return ps.length === 1 ? agentAt(terminalOf(ps[0])) : undefined }
          return (
            <View>
              <If when={lone()} else={
                <View role="button" testID={`switch-tab-${c().id}`} onPress={go} class={`${row} ${on() ? 'bg-accent' : ''}`}>
                  <ProgramIcon icon={(rev(), tabIcon(s, c()))} size={16} />
                  <Text class="flex-1 text-[14px] text-foreground" numberOfLines={1}>{(rev(), s.tabLabel(space()!, c()))}</Text>
                  <If when={(rev(), s.tabStatus(c()))}>{(st) => <StatusMark status={st()} size={7} />}</If>
                </View>
              }>{(a) => (
                <AgentRow store={s} a={a()} testID={`switch-tab-${c().id}`} iconSize={16} class={`${agentRow} ${on() ? 'bg-accent' : ''}`}
                  subPrefix="switch-sub" subClass="pl-8 pr-2" onPress={go} onSub={sub} />
              )}</If>
              <If when={panes(c().root).length > 1}>
                <Each of={panes(c().root)} key={(n) => n.id}>{(n) => {
                  const pick = () => { s.selectTab(c().id); s.focusPane(n().id); close() }
                  return (
                    <If when={(rev(), agentAt(terminalOf(n())))} else={
                      <View role="button" testID={`switch-pane-${n().id}`} onPress={pick} class={`${row} pl-8 h-9`}>
                        <ProgramIcon icon={(rev(), s.program(terminalOf(n())).icon)} size={14} />
                        <Text class="flex-1 text-[13px] text-muted-foreground" numberOfLines={1}>{(rev(), terminalOf(n()) ? s.paneName(terminalOf(n())) : t('panel'))}</Text>
                        <If when={(rev(), s.statusOf(terminalOf(n())))}>{(st) => <StatusMark status={st()} compact size={6} />}</If>
                      </View>
                    }>{(a) => (
                      <AgentRow store={s} a={a()} testID={`switch-pane-${n().id}`} class={`${agentRow} pl-8`} subPrefix="switch-sub" subClass="pl-14 pr-2" onPress={pick} onSub={sub} />
                    )}</If>
                  )
                }}</Each>
              </If>
            </View>
          )
        }}</Each>
        <If when={others().length}>
          <Text class="px-2 pt-3 pb-1 text-[11.5px] font-medium text-sidebar-foreground">{t('otherSpaces')}</Text>
          <Each of={others()} key={(a) => a.address}>{(a) => (
            <AgentRow store={s} a={a()} full testID={`switch-agent-${a().address}`} iconSize={16} class={agentRow}
              subPrefix="switch-sub" subClass="pl-8 pr-2" onPress={() => { close(); props.actions.open(a().address) }} onSub={sub} />
          )}</Each>
        </If>
        <Text class="px-2 pt-3 pb-1 text-[11.5px] font-medium text-sidebar-foreground">{t('newTab')}</Text>
        <View class="flex-row flex-wrap gap-1.5 px-1">
          <Each of={fresh()} key={(p) => p.key}>{(p) => (
            <View role="button" testID={`switch-new-${p().key}`} onPress={() => { close(); void s.newTab({ space: props.spaceId, argv: p().argv }).catch((e) => s.toast({ title: t('couldntStart'), body: String(e?.message ?? e), level: 'error' })) }}
              class="flex-row items-center gap-2 h-10 px-3 rounded-[8px] bg-secondary border border-border active:bg-accent">
              <ProgramIcon icon={p().icon} size={15} />
              <Text class="text-[13.5px] text-foreground">{p().name}</Text>
            </View>
          )}</Each>
        </View>
        <View role="button" testID="switch-new-folder" onPress={() => { close(); props.actions.newTab(props.spaceId) }} class={`${row} mt-1`}>
          <Glyph name="folder" size={16} />
          <Text class="flex-1 text-[13.5px] text-muted-foreground">{t('anotherFolder')}</Text>
        </View>
      </View>
      </Scroll>
    </Sheet>
  )
}

/** Under this width a pane's header has room for its name, zoom and close only; ⋯ still splits. */
const NARROW_PANE = 340

/** A tablet, a wide window, or a phone asked to: the split tree, every pane on screen, one key bar for the focused one. */
function WidePanes(props: { store: Store; spaceId: string; tab: Tab; actions: SpaceActions; sessions: Map<string, TerminalSession>; split: (pane: string, direction: 'right' | 'down') => void }) {
  const s = props.store
  const rev = useRev(s)
  const box = signal({ w: 0, h: 0 })
  const sessions = props.sessions
  const rects = computed(() => { rev(); const b = box(); return [...layoutRects(shownNode(props.tab), 0, 0, b.w, b.h)] })
  const focused = () => { rev(); return focusedPaneOf(props.tab) }
  const focusedSession = () => { rev(); const f = focused(); const a = f ? props.store.model.addressOf(f) : undefined; return a ? sessions.get(a) : undefined }
  const GAP = 8
  // A split or a close changes a pane's height for good; on a phone its rows hold still otherwise (terminal-page.ts).
  const count = computed(() => { rev(); return panes(props.tab.root).length })
  effect(() => { count(); for (const t of sessions.values()) t.refit() })
  return (
    <View class="flex-1 bg-sidebar-primary">
      <View class="flex-1 m-1" onLayout={(b) => box.set({ w: b.size.width, h: b.size.height })}>
        <Each of={rects()} key={(r) => r[0]}>{(r) => {
          const n = () => find(props.tab.root, r()[0])
          return (
            <If when={n()}>{(node) => (
              <View onPress={() => s.focusPane(node().id)} class={`absolute ${frame(focused() === node().id)}`}
                style={{ left: r()[1].x + GAP / 2, top: r()[1].y + GAP / 2, width: Math.max(0, r()[1].w - GAP), height: Math.max(0, r()[1].h - GAP) }}>
                <PaneHeader store={s} node={node()} tab={props.tab} focused={focused() === node().id} narrow={r()[1].w < NARROW_PANE} split={props.split} />
                <PaneBody store={s} spaceId={props.spaceId} node={node()} actions={props.actions} keys={false} tap={() => s.focusPane(node().id)}
                  onSession={(t) => { sessions.set(t.address, t); cleanup(() => sessions.delete(t.address)) }} />
              </View>
            )}</If>
          )
        }}</Each>
      </View>
      <Each of={focusedSession() ? [focusedSession()!] : []}>{(t) => <PaneBar store={s} session={t()} program={s.program(t().address).name} agent={!!s.program(t().address).agent} typeIn={props.actions.typeIn} keep={props.actions.keepTypeIn} />}</Each>
    </View>
  )
}

/** The desktop's workspace-status bar for this space: its computer, its branch, the agents. */
function SpaceStatus(props: { store: Store; spaceId: string }) {
  const s = props.store
  const rev = useRev(s)
  const space = () => { rev(); return s.model.space(props.spaceId) }
  const branch = () => {
    rev()
    const sp = space()
    for (const c of sp?.tabs ?? []) for (const a of addresses(c.root)) { const f = s.model.folderInfo.get(a); if (f?.branch) return f.branch }
    return ''
  }
  const counts = () => { rev(); return s.counts() }
  return (
    <View class="flex-row items-center gap-4 h-7 px-3.5 bg-sidebar border-t border-border">
      <View class="flex-row items-center gap-1.5 shrink">
        <Glyph name={space()?.machine ? 'server' : 'monitor'} size={13} />
        <Text class="text-[11.8px] text-muted-foreground" numberOfLines={1}>{space()?.machine ?? t('thisComputer')}</Text>
      </View>
      <If when={branch()}>
        <View class="flex-row items-center gap-1.5 shrink">
          <Glyph name="branch" size={13} />
          <Text class="text-[11.8px] text-muted-foreground" numberOfLines={1}>{branch()}</Text>
        </View>
      </If>
      <View class="flex-row items-center gap-1.5">
        <View class="w-1.5 h-1.5 rounded-full bg-sidebar-foreground" />
        <Text class="text-[11.8px] text-muted-foreground" numberOfLines={1}>
          {`${counts().agents} ${counts().agents === 1 ? 'agent' : 'agents'}`}
          <If when={counts().needsYou}><Text class="text-chart-2">{` · ${t('needYou', { n: counts().needsYou })}`}</Text></If>
        </Text>
      </View>
    </View>
  )
}

export function SpaceScreen(props: { store: Store; spaceId: string; actions: SpaceActions }) {
  const s = props.store
  const rev = useRev(s)
  s.viewingTab = true
  cleanup(() => { s.viewingTab = false })
  const space = () => { rev(); return s.model.space(props.spaceId) }
  const tab = () => { rev(); const sp = space(); return sp && spaceActiveTab(sp) }
  const name = () => { rev(); const sp = space(); return sp ? s.spaceName(sp) : '' }
  const tail = () => {
    rev()
    for (const c of space()?.tabs ?? []) for (const a of addresses(c.root)) { const f = s.model.folderInfo.get(a); if (f?.branch) return gitTail(name(), f) }
    return ''
  }
  const color = () => { rev(); return s.spaceMarks()[props.spaceId] ?? '#22a99a' }
  const wide = () => windowSize().width >= WIDE
  const insets = safeAreaInsets()
  const typing = () => keyboardFrame().height > 0
  const sessions = new Map<string, TerminalSession>()
  /** Panes side by side on a small screen: what it costs, then the choice. */
  const accepts = async () => {
    if (wide()) return true
    return await AlertDialog.show({ title: t('smallSplitTitle'), description: t('smallSplitDetail'), actions: [
      { id: 'cancel', label: t('cancel'), role: 'cancel' }, { id: 'go', label: t('smallSplitGo') }] }) === 'go'
  }
  const split = async (pane: string | undefined, direction: 'right' | 'down') => {
    if (!await accepts()) return
    const current = tab()
    try {
      await s.splitPane(pane, direction)
      if (current && !wide()) s.showWhole(current.id, true)
    } catch (e: any) {
      s.toast({ title: t('couldntStart'), body: String(e?.message ?? e), level: 'error' })
    }
  }
  const menu = async () => {
    const current = tab()
    const pane = current ? focusedPaneOf(current) : undefined
    const phone = !wide()
    const address = pane ? s.model.addressOf(pane) : undefined
    const session = address ? sessions.get(address) : undefined
    const r = await AlertDialog.show({ title: name(), presentation: 'actionSheet', actions: [
      ...(session ? [{ id: 'type', label: t('typeInTerminal') }] : []),
      ...(session?.typeIn.get() === 'terminal' ? [{ id: 'attach', label: t('attach') }] : []),
      ...(phone && pane ? [{ id: 'split-right', label: t('splitRight') }, { id: 'split-down', label: t('splitDown') }, { id: 'close-pane', label: t('closePane'), role: 'destructive' as const }] : []),
      ...(phone && current && panes(current.root).length > 1 ? [s.shownWhole.has(current.id) ? { id: 'show-one', label: t('showOnePane') } : { id: 'show-all', label: t('showAllPanes') }] : []),
      ...(phone && current ? [{ id: 'rename-tab', label: t('renameTab') }] : []),
      { id: 'rename', label: t('renameSpace') }, { id: 'close', label: t('closeSpace'), role: 'destructive' },
      ...(phone ? [{ id: 'settings', label: t('settings') }] : []), { id: 'cancel', label: t('cancel'), role: 'cancel' }] })
    if (r === 'type') session?.focus()
    else if (r === 'attach' && session) void attachToTerminal(s, session)
    else if (r === 'split-right' || r === 'split-down') void split(pane, r === 'split-right' ? 'right' : 'down')
    else if (r === 'show-one' && current) s.showWhole(current.id, false)
    else if (r === 'show-all' && current) { if (await accepts()) s.showWhole(current.id, true) }
    else if (r === 'close-pane' && pane) s.closePane(pane)
    else if (r === 'rename-tab' && current) props.actions.rename('tab', current.id, current.name)
    else if (r === 'settings') props.actions.settings()
    else if (r === 'rename') props.actions.rename('space', props.spaceId, space()?.name ?? '')
    else if (r === 'close') {
      const ok = await AlertDialog.show({ title: t('closeTitle', { name: name() }), description: t('closeDetail'), actions: [
        { id: 'cancel', label: t('cancel'), role: 'cancel' }, { id: 'close', label: t('close'), role: 'destructive' }] })
      if (ok === 'close') { s.closeSpace(props.spaceId); props.actions.back() }
    }
  }
  return (
    // The key bar sits at the bottom: above the keyboard while it is up (NSH moves only scroll views clear of it).
    <View class="flex-1 bg-sidebar pt-[env(safe-area-inset-top)]" style={{ paddingBottom: typing() ? keyboardFrame().height : insets().bottom }}>
      <If when={wide() || !space()}><View class="flex-row items-center h-11 pl-1 pr-1 gap-2">
        <IconButton name="chevron-left" label={t('back')} testID="back" onPress={props.actions.back} />
        <SpaceBadge name={name()} color={color()} size={20} />
        <Text class="text-[15px] font-semibold text-foreground shrink" numberOfLines={1}>{name()}</Text>
        <Text class="flex-1 text-[12px] text-sidebar-foreground" numberOfLines={1}>{tail()}</Text>
        <IconButton name="settings" label={t('spaceActions')} testID="space-more" onPress={() => void menu()} />
      </View></If>
      <If when={space()} else={<View class="flex-1 items-center justify-center"><Text class="text-sidebar-foreground">{t('spaceClosed')}</Text></View>}>
        <If when={wide()}><TabBar store={s} spaceId={props.spaceId} actions={props.actions} /></If>
        <Each of={tab() ? [tab()!] : []} key={(t) => t.id}>{(t) => (
          <If when={wide()} else={<PhoneSpace store={s} spaceId={props.spaceId} tab={t()} actions={props.actions} sessions={sessions} back={props.actions.back} menu={() => void menu()} split={(p, d) => void split(p, d)} />}>
            <WidePanes store={s} spaceId={props.spaceId} tab={t()} actions={props.actions} sessions={sessions} split={(p, d) => void split(p, d)} />
          </If>
        )}</Each>
        <If when={wide() && !typing()}><SpaceStatus store={s} spaceId={props.spaceId} /></If>
      </If>
    </View>
  )
}
