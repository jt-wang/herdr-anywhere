// The progress page (experimental, Settings turns it on), laid out the way Things 3 lays out its lists, in Herdr's own
// colours: what needs the user on top, then the tasks, one row each as the agent list shows an agent: loose ones first,
// then each goal as a heading over its own, which fold (model/progress.ts homeOf). What finished today stays where it
// sits, ticked; older finished and dropped work waits behind one entry at the bottom.
// Version one is view only: the computer files what was said and decides when a
// task is done or asks something; the page shows it and opens the pane that asks.
// Everything comes from `herdr-anywhere track tree --json` on the computer (model/progress.ts); nothing is kept here.
import { cleanup, colorScheme, Scroll, signal, Text, View } from 'nsh'
import type { Store } from '../store.ts'
import { explain } from '../store.ts'
import { blocks, closed, findTask, flagsOf, flat, goalOf, homeOf, visibleRows, langLine, paneOf, panesOf, parseTree, pathOf, plain, stageText, subLine, taskOn, trackLine, type Row, type Task, type Tree, type View as PView } from '../model/progress.ts'
import { lang, t as w } from '../strings/index.ts'
import { Glyph, IconButton, statusLabel, useRev } from './kit.tsx'
import { DOING, statusText } from './agent-row.tsx'
import { ViewSwitch, type ViewSwitchProps } from './view-switch.tsx'
import { alertsFor, Outcomes, type Alert } from '../model/progress-alerts.ts'

export type ProgressActions = {
  open: (v: PView) => void
  back: () => void
  /** The pane's own screen. */
  openPane: (addr: string) => void
  /** The switch at the top, to the agent list and back. */
  view: () => ViewSwitchProps
  settings: () => void
  /** The headings the user folded, by task id, kept in the phone's settings; `fold` folds or unfolds one. */
  folded: () => string[]
  fold: (id: string) => void
}

/** One reading of the tree for every progress screen, with a 30-second foreground fallback. */
export class ProgressData {
  tree = signal<Tree | null>(null)
  error = signal('')
  private users = 0
  private timer: ReturnType<typeof setInterval> | undefined
  private off: (() => void)[] = []
  /** Rings for each task that newly needs the user (model/progress-alerts.ts); root.tsx sets it. */
  onAlert?: (a: Alert) => void
  private outcomes = new Outcomes()
  /** The language the computer was last told, so the sorting agent writes in the phone's (root.tsx keeps it in prefs). */
  told?: { get: () => string; set: (l: string) => void }
  constructor(readonly store: Store, private every = 30_000, private keep?: { load: () => Promise<string | null>; save: (v: string) => void }) {
    void keep?.load().then((v) => { if (v) this.outcomes = Outcomes.from(v) }).catch(() => {})
  }

  /** The user opened a task: a notification for it counts as seen. */
  opened(task: string) {
    this.outcomes.opened(task, Date.now())
    this.keep?.save(this.outcomes.save(Date.now()))
  }

  paneFor(task: string): string | undefined {
    const t = this.tree()
    const x = t ? findTask(t, task) : undefined
    return x ? paneOf(x)?.addr : undefined
  }

  /** The open task a pane works on, for the agent list's 在做 line. */
  taskOn(addr: string): Task | undefined {
    const t = this.tree()
    return t ? taskOn(t, addr) : undefined
  }

  /** Whether a pane works on an open task: its own "needs you" alert is then left to the task's. */
  covers(addr: string | undefined): boolean {
    const t = this.tree()
    if (!t || !addr) return false
    return flat(t).some(({ task: x }) => x.pane_rows.some((p) => p.id === addr && !p.handed_on))
  }

  /** Tells the computer the app's language when it changed since it was last told: what the sorting agent writes in. */
  async tellLang() {
    const l = lang()
    if (!this.told || this.told.get() === l) return
    const r = await this.store.companion(langLine(l), 15)
    if (r.code === 0) this.told.set(l)
  }

  async refresh() {
    try {
      await this.tellLang()
      const r = await this.store.companion(trackLine(['tree', '--json']), 15)
      if (r.code !== 0) throw new Error(r.stderr.trim() || `exit ${r.code}`)
      const next = parseTree(r.stdout)
      const rang = alertsFor(this.tree(), next, this.outcomes, Date.now())
      this.tree.set(next)
      this.error.set('')
      if (rang.length) {
        this.keep?.save(this.outcomes.save(Date.now()))
        for (const a of rang) this.onAlert?.(a)
      }
      this.schedule()
    } catch (e) {
      this.error.set(explain(e))
    }
  }

  private schedule() {
    if (this.timer) clearInterval(this.timer)
    this.timer = undefined
    const tasks = this.tree() && flat(this.tree()!).some(({ task }) => !closed(task.group))
    const every = !this.users || !this.store.connected ? 0 : this.store.appActive ? this.every
      : this.store.backgroundWatch.get() && tasks ? 60_000 : 0
    if (every) this.timer = setInterval(() => void this.refresh(), every)
  }

  use() {
    if (this.users++ === 0) {
      const resume = () => { this.schedule(); if (this.store.appActive && this.store.connected) void this.refresh() }
      this.off = [this.store.activity.sub(resume), this.store.backgroundWatch.sub(() => this.schedule()),
        this.store.conn.sub(resume, 'ProgressData.poll')]
      resume()
    }
    cleanup(() => {
      if (--this.users) return
      this.off.forEach((off) => off()); this.off = []
      this.schedule()
    })
  }
}

// A stage in the agent list's status colours: waiting on you or stuck as "needs you", done-to-check as done, doing as
// working, the rest (queued, done, dropped) as idle.
const AS_STATUS: Record<string, string> = { waiting: 'blocked', stuck: 'blocked', review: 'done', doing: 'working' }
const GROUP_DOT: Record<string, string> = {
  waiting: 'bg-chart-2', stuck: 'bg-chart-2', review: 'bg-chart-2', doing: 'bg-muted-foreground', parked: 'bg-border', done: 'bg-muted-foreground', dropped: 'bg-border',
}
const quiet = (group: string) => group === 'parked' || closed(group)

function StageWord(props: { group: string }) {
  return <Text class={`text-[12px] ${statusText(AS_STATUS[props.group] ?? 'idle')}`}>{stageText(props.group)}</Text>
}

/** A list's label, as the agent list's: small, with a count or note on the right. */
function Section(props: { label: string; right?: string; tone?: string; rightID?: string }) {
  return (
    <View class="flex-row items-center h-10 px-[22px] mt-1 gap-1.5">
      <Text class="text-[12px] font-medium text-sidebar-foreground">{props.label}</Text>
      <View class="flex-1" />
      <If when={props.right}><Text testID={props.rightID} class="text-[11px] text-sidebar-foreground">{props.right}</Text></If>
    </View>
  )
}

const Divider = () => <View class="h-px bg-border mx-4 mt-2" />

function SmallButton(props: { label: string; testID: string; onPress: () => void; primary?: boolean }) {
  return (
    <View role="button" testID={props.testID} onPress={props.onPress}
      class={`h-8 px-3 justify-center rounded-[7px] ${props.primary ? 'bg-primary active:opacity-80' : 'bg-secondary border border-border active:bg-accent'}`}>
      <Text class={`text-[13px] font-medium ${props.primary ? 'text-primary-foreground' : 'text-foreground'}`}>{props.label}</Text>
    </View>
  )
}

function TopBar(props: { title: string; back?: () => void; right?: any }) {
  return (
    // A task's page is where its whole title is read, so a long one wraps.
    <View class="flex-row items-center min-h-11 py-1 px-2 gap-1">
      <If when={props.back}>
        <IconButton name="chevron-left" label={w('back')} testID="progress-back" onPress={() => props.back!()} />
      </If>
      <Text class={`flex-1 text-[17px] leading-[22px] font-semibold text-foreground ${props.back ? '' : 'pl-2'}`} numberOfLines={3}>{props.title}</Text>
      {props.right}
    </View>
  )
}

const CARD = 'mx-3 mb-2 px-3 py-3 rounded-[18px] bg-card border border-border'
const ROW = 'mx-2 px-2 min-h-9 py-1.5 justify-center rounded-[8px] active:bg-sidebar-accent'

/** One task as one row (under a heading, `depth` steps in, lined up with the heading's title), the way the agent list shows an agent: its title, its stage in the status colours, and a line
 *  under it on what it waits for, what is wrong, or what the agent said last. Queued work is a step dimmer. */
function TaskRow(props: { task: Task; actions: ProgressActions; testID: string; depth?: number }) {
  const t = () => props.task
  // Only what needs the user gets a second line: what it waits for, or what is wrong. The rest is on the task's page.
  const alert = () => (closed(t().group) ? '' : t().waiting || flagsOf(t())[0] || '')
  return (
    <View role="button" testID={props.testID} onPress={() => props.actions.open({ name: 'task', id: t().id })}
      class={ROW} style={{ marginLeft: props.depth === undefined ? 8 : 24 + props.depth * 16 }}>
      <View class="flex-row items-center gap-2">
        <If when={closed(t().group)}><Text class="text-[14px] text-chart-3">{t().group === 'dropped' ? '–' : '✓'}</Text></If>
        <Text class={`flex-1 text-[15px] leading-[21px] ${quiet(t().group) ? 'text-muted-foreground' : 'text-foreground'}`} numberOfLines={2}>{t().outcome}</Text>
        <If when={!closed(t().group)}><StageWord group={t().group} /></If>
      </View>
      <If when={alert()}><Text class="text-[12px] leading-[18px] text-muted-foreground" numberOfLines={2}>{plain(alert())}</Text></If>
    </View>
  )
}

/** The arrow before a heading that folds what is under it, or unfolds it: the same › and ▾ as the "done" entry's. */
function Fold(props: { id: string; actions: ProgressActions }) {
  const shut = () => props.actions.folded().includes(props.id)
  return (
    <View role="button" testID={`fold-${props.id}`} accessibilityLabel={shut() ? w('unfold') : w('fold')} onPress={() => props.actions.fold(props.id)}
      class="w-7 h-8 items-center justify-center rounded-[6px] active:bg-sidebar-accent">
      <Text class="text-[16px] text-foreground">{shut() ? '›' : '▾'}</Text>
    </View>
  )
}

/** A task with open tasks of its own, under a heading: its title and how many of its tasks are done. */
function SubHeading(props: { task: Task; actions: ProgressActions; depth: number }) {
  const t = () => props.task
  return (
    <View class="flex-row items-center mt-1" style={{ marginLeft: 4 + props.depth * 16 }}>
      <Fold id={t().id} actions={props.actions} />
      <View role="button" testID={`task-${t().id}`} onPress={() => props.actions.open({ name: 'task', id: t().id })}
        class="flex-1 flex-row items-center gap-2 min-h-9 py-1.5 pr-2 mr-2 rounded-[8px] active:bg-sidebar-accent">
        <Text class="flex-1 text-[15px] leading-[21px] font-medium text-foreground" numberOfLines={2}>{t().outcome}</Text>
        <Text class="text-[11.5px] text-sidebar-foreground">{`${t().done}/${t().total}`}</Text>
      </View>
    </View>
  )
}

/** A bigger task as a heading, the way a project heads its to-dos: its title and how many of its tasks are done, then
 *  its open tasks. */
function Group(props: { task: Task; rows: Row[]; actions: ProgressActions }) {
  const t = () => props.task
  return (
    <View class="mt-3">
      <View class="flex-row items-center pl-1 pb-1">
        <Fold id={t().id} actions={props.actions} />
        <View role="button" testID={`top-${t().id}`} onPress={() => props.actions.open({ name: 'task', id: t().id })} class="flex-1 flex-row items-center pr-4 gap-2 active:opacity-70">
          <Text class="flex-1 text-[15.5px] font-semibold text-foreground" numberOfLines={2}>{t().outcome}</Text>
          <Text class="text-[12px] text-sidebar-foreground">{`${t().done}/${t().total}`}</Text>
        </View>
      </View>
      <Each of={props.actions.folded().includes(t().id) ? [] : visibleRows(props.rows, new Set(props.actions.folded()))} key={(r) => r.task.id}>{(r) => (
        <If when={r().head} else={<TaskRow task={r().task} actions={props.actions} testID={`task-${r().task.id}`} depth={r().depth} />}>
          <SubHeading task={r().task} actions={props.actions} depth={r().depth} />
        </If>
      )}</Each>
    </View>
  )
}

/** What needs the user: where it sits, the question, and the pane that asks. */
function NeedsRow(props: { task: Task; goal: string; actions: ProgressActions }) {
  const t = () => props.task
  const p = () => paneOf(t())
  return (
    <View class="flex-row items-center mx-2 pr-2">
      <View role="button" testID={`needs-task-${t().id}`} onPress={() => props.actions.open({ name: 'task', id: t().id })}
        class="flex-1 px-2 py-1.5 rounded-[8px] active:bg-sidebar-accent">
        <Text class="text-[15px] leading-[21px] text-foreground" numberOfLines={2}>{t().outcome}</Text>
        <Text class="text-[12px] leading-[18px] text-muted-foreground" numberOfLines={2}>
          <If when={props.goal}><Text class="text-sidebar-foreground">{`${props.goal} · `}</Text></If>
          {plain(subLine(t()))}
        </Text>
      </View>
      <If when={p()}>
        <SmallButton testID={`needs-task-${t().id}-open`} label={w('openNamed', { name: p()?.name ?? p()?.addr ?? '' })} onPress={() => props.actions.openPane(p()!.addr)} />
      </If>
    </View>
  )
}

function Status(props: { data: ProgressData }) {
  const err = props.data.error
  return (
    <If when={err()}>
      <View testID="progress-error" class="mx-4 my-2 p-3 rounded-[10px] border border-border bg-background">
        <Text class="text-[13px] text-foreground">{w('cantReadProgress')}</Text>
        <Text class="text-[12px] text-muted-foreground" numberOfLines={4}>{err()}</Text>
      </View>
    </If>
  )
}

export function ProgressHome(props: { data: ProgressData; actions: ProgressActions }) {
  props.data.use()
  const tree = props.data.tree
  const showDone = signal(false)
  const home = () => { const t = tree(); return t ? homeOf(t) : undefined }
  const empty = () => !!home() && !home()!.loose.length && !home()!.groups.length && !home()!.after.length
  return (
    <View class="flex-1">
      <View class="flex-row items-center h-[57px] pl-3 pr-2 gap-2 border-b border-border">
        <View class="flex-1"><ViewSwitch {...props.actions.view()} /></View>
        <IconButton name="settings" label={w('settings')} testID="settings" onPress={props.actions.settings} />
      </View>
      <Status data={props.data} />
      <Scroll class="flex-1" testID="progress-body">
        <If when={tree()?.organizerWaits}>
          <View testID="organizer-waits" class={CARD}>
            <Text class="text-[15px] font-semibold text-foreground">{w('organizerWaitsTitle')}</Text>
            <Text class="text-[13px] text-muted-foreground mt-1">{w('organizerWaitsBody')}</Text>
            <View class="flex-row mt-2">
              <SmallButton testID="organizer-waits-open" primary label={w('openPaneBtn')} onPress={() => props.actions.openPane(tree()!.organizerWaits!.addr)} />
            </View>
          </View>
        </If>
        <If when={tree()?.needsYou.length}>
          <Section label={w('secNeedsYou')} right={String(tree()!.needsYou.length)} />
          <Each of={tree()!.needsYou} key={(t) => t.id}>{(t) => <NeedsRow task={t()} goal={goalOf(tree()!, t())} actions={props.actions} />}</Each>
          <Divider />
        </If>
        <Section label={w('secInProgress')} rightID="queue-line" right={tree()?.queue ? w('queueLeft', { n: tree()!.queue }) : undefined} />
        <Each of={home()?.loose ?? []} key={(t) => t.id}>{(t) => <TaskRow task={t()} actions={props.actions} testID={`task-${t().id}`} />}</Each>
        <Each of={home()?.groups ?? []} key={(g) => g.task.id}>{(g) => <Group task={g().task} rows={g().rows} actions={props.actions} />}</Each>
        <If when={home()?.after.length}><View class="mt-3"><Each of={home()!.after} key={(t) => t.id}>{(t) => <TaskRow task={t()} actions={props.actions} testID={`task-${t().id}`} />}</Each></View></If>
        <If when={empty()}>
          <Text testID="progress-empty" class="mx-4 my-2 text-[13px] text-muted-foreground">{w('progressEmpty')}</Text>
        </If>
        <If when={home()?.done.length}>
          <View role="button" testID="progress-done-toggle" onPress={() => showDone.set(!showDone())} class="h-10 px-4 mt-3 justify-center active:opacity-70">
            <Text class="text-[13px] text-muted-foreground">{`${w('doneCount', { n: home()!.done.length })} ${showDone() ? '▾' : '›'}`}</Text>
          </View>
          <If when={showDone()}><Each of={home()!.done} key={(t) => t.id}>{(t) => <TaskRow task={t()} actions={props.actions} testID={`task-${t().id}`} />}</Each></If>
        </If>
        <View class="h-24" />
      </Scroll>
    </View>
  )
}

/** What the agent said (Markdown, as agents write it) as paragraphs and list items, without the marks. */
function Said(props: { id: string; text: string }) {
  return (
    <View testID={`said-${props.id}`} class="gap-1.5">
      <Each of={blocks(props.text)}>{(b) => (
        <If when={b().mark} else={<Text class={`text-[14px] text-foreground ${b().head ? 'font-semibold mt-1' : ''}`}>{b().text}</Text>}>
          <View class={`flex-row gap-1.5 ${b().depth ? 'pl-4' : ''}`}>
            <Text class="text-[14px] text-muted-foreground min-w-[14px]">{b().mark ?? ''}</Text>
            <Text class="flex-1 text-[14px] text-foreground">{b().text}</Text>
          </View>
        </If>
      )}</Each>
    </View>
  )
}

export function TaskScreen(props: { data: ProgressData; id: string; actions: ProgressActions }) {
  props.data.use()
  props.data.opened(props.id)
  const task = () => { const t = props.data.tree(); return t ? findTask(t, props.id) : undefined }
  const path = () => { const t = props.data.tree(); return t ? pathOf(t, props.id) : [] }
  const panes = () => { const t = task(); return t ? panesOf(t) : [] }
  // A live pane as the agent list shows it: its status and what it is doing.
  const rev = useRev(props.data.store)
  const live = (addr?: string) => { rev(); const a = props.data.store.agents().find((x) => x.address === addr); return { status: a?.status, line: props.data.store.activityOf(addr) } }
  return (
    <View class="flex-1">
      <TopBar title={task()?.outcome ?? ''} back={props.actions.back} />
      <Status data={props.data} />
      <Scroll class="flex-1">
        <If when={task()}>{(t) => (
          <View class="px-4 gap-2">
            <If when={path().length}><Text class="text-[12px] text-sidebar-foreground">{path().join(' › ')}</Text></If>
            <StageWord group={t().group} />
            <If when={t().std}><Text class="text-[13px] text-muted-foreground">{w('doneWhen', { std: t().std ?? '' })}</Text></If>
            <If when={subLine(t())}><Said id={t().id} text={subLine(t())} /></If>
            <Each of={flagsOf(t()).slice(subLine(t()) === flagsOf(t())[0] ? 1 : 0)}>{(f) => <Text class="text-[12px] leading-[18px] text-muted-foreground">{f()}</Text>}</Each>
            <If when={t().children.length}>
              <Text class="text-[12.5px] text-muted-foreground mt-1">{w('tasksUnder')}</Text>
              <Each of={t().children} key={(c) => c.id}>{(c) => (
                <View role="button" testID={`sub-${c().id}`} onPress={() => props.actions.open({ name: 'task', id: c().id })}
                  class="flex-row items-center gap-2 py-1.5 active:opacity-70">
                  <View class={`w-2 h-2 rounded-full ${GROUP_DOT[c().group] ?? 'bg-border'}`} />
                  <Text class="flex-1 text-[14px] text-foreground" numberOfLines={1}>{c().outcome}</Text>
                  <Text class="text-[12px] text-sidebar-foreground">{stageText(c().group)}</Text>
                </View>
              )}</Each>
            </If>
            <Each of={panes()}>{(p) => (
              <View role="button" testID={`task-pane-${p().addr ?? p().label}`} onPress={() => {
                if (p().row.found && p().row.state === 'live' && p().addr) props.actions.openPane(p().addr!)
              }} class="py-1.5 active:opacity-70">
                <View class="flex-row items-center gap-2">
                  <Glyph name="terminal" size={13} />
                  <Text class="flex-1 text-[13.5px] text-foreground" numberOfLines={1}>{p().label}</Text>
                  <If when={p().kind === 'live'}><Text class={`text-[12px] ${statusText(live(p().addr).status)}`}>{statusLabel(live(p().addr).status ?? p().row.status)}</Text></If>
                  <If when={p().kind === 'moved'}><Text class="text-[12px] text-sidebar-foreground">{w('paneMovedOn')}</Text></If>
                  <If when={p().kind === 'missing'}><Text class="text-[12px] text-sidebar-foreground">{w('paneMissing')}</Text></If>
                </View>
                <If when={p().kind === 'live' && live(p().addr).line}>
                  <Text class="text-[13px] pl-[21px]" style={{ color: DOING[colorScheme() === 'dark' ? 'dark' : 'light'] }} numberOfLines={1}>{live(p().addr).line}</Text>
                </If>
              </View>
            )}</Each>
            <Each of={t().evidence}>{(e) => <Text class="text-[12.5px] text-muted-foreground font-mono" numberOfLines={1}>{e()}</Text>}</Each>
          </View>
        )}</If>
        <View class="h-24" />
      </Scroll>
    </View>
  )
}
