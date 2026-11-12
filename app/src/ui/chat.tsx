// The chat view over a pane's terminal: the agent's turns as the computer reads them from the agent's own record
// (model/chat.ts). It only reads; the composer under it still types into the terminal. When the computer isn't sure,
// the view shows why and the way back, and none of the turns.
import { cleanup, effect, Image, Keyboard, Scroll, signal, Text, untracked, Video, View } from 'nsh'
import { Sheet, TextView } from 'nsh/base'
import type { Store } from '../store.ts'
import { changeRows, clockText, pathsIn, sizeText, foldLine, outsideFold, refreshEvery, shows, unknownText, type ChatAnswer, type ChatSetup, type ChatChange, type ChatDetail, type ChatFile, type ChatPicture, type ChatRun, type ChatStep, type ChatTurn } from '../model/chat.ts'
import { Glyph, IconButton, useRev } from './kit.tsx'
import { AskCard } from './ask-card.tsx'
import { askCard } from '../model/ask.ts'
import { t } from '../strings/index.ts'
import { openFile, videoFrame, type VideoFrame } from '../page.ts'

const STEP_GLYPH: Record<string, string> = { read: 'file-text', edit: 'square-pen', run: 'terminal', search: 'search', web: 'monitor', agent: 'layers', think: 'brain' }

export function ChatView(props: { store: Store; address: string; status: () => string | undefined; back: () => void }) {
  const s = props.store
  const answer = signal<ChatAnswer | undefined>(undefined)
  const offline = signal(false)
  let alive = true
  let timer: ReturnType<typeof setTimeout> | undefined
  const ask = async () => {
    try {
      const a = await s.chat!.get(props.address)
      if (!alive) return
      answer.set(a)
      offline.set(false)
    } catch {
      if (alive) offline.set(true)
    }
  }
  const schedule = () => {
    clearTimeout(timer)
    const every = refreshEvery(props.status())
    if (every) timer = setTimeout(() => void ask().then(() => { if (alive) schedule() }), every)
  }
  // Ask now and on each change of status; keep asking while the agent works or waits.
  effect(() => {
    props.status()
    untracked(() => { void ask(); schedule() })
  })
  cleanup(() => { alive = false; clearTimeout(timer) })

  let scroller: any
  const viewport = signal(0)
  let pinned = true
  let content = 0
  const toEnd = (height: number) => { content = height; if (pinned) scroller?.scrollTo({ y: Math.max(0, height - viewport()), animated: false }) }

  // The record falls behind the screen for a moment at each new prompt: the turns already shown stay up, marked, so
  // what was opened stays open. Any other doubt shows no turn at all.
  let lastTurns: ChatTurn[] | undefined
  const turns = () => { const a = answer(); if (a?.turns) lastTurns = a.turns; return a?.turns ?? (a?.unknown === 'behind' ? lastTurns : undefined) }
  const behind = () => answer()?.unknown === 'behind' && !!lastTurns
  // Said above the turns only when it lasts: over 10 s behind.
  const behindLong = signal(false)
  let behindTimer: ReturnType<typeof setTimeout> | undefined
  effect(() => {
    const b = behind()
    untracked(() => {
      if (!b) { clearTimeout(behindTimer); behindTimer = undefined; behindLong.set(false) }
      else if (!behindTimer) behindTimer = setTimeout(() => behindLong.set(true), 10_000)
    })
  })
  cleanup(() => clearTimeout(behindTimer))
  const unknown = () => { const a = answer(); return a && (!a.chat ? 'gone' : behind() ? undefined : a.unknown) }
  // The decision card: the agent is blocked and its screen's menu was read; the last turn's question, if it is open.
  const rev = useRev(s)
  const card = () => {
    rev()
    const last = turns()?.at(-1)
    return askCard(s.term(props.address)?.status, s.menuFor(props.address), last?.end === 'open' ? last.ask : undefined)
  }
  return (
    <View testID="chat-view" class="flex-1 bg-background">
      <If when={offline()}>
        <View class="px-3 py-1.5 bg-popover border-b border-border">
          <Text testID="chat-offline" class="text-[12px] text-muted-foreground">{t('chatOffline')}</Text>
        </View>
      </If>
      <If when={!offline() && behind() && behindLong()}>
        <View class="px-3 py-1.5 bg-popover border-b border-border">
          <Text testID="chat-behind" class="text-[12px] text-muted-foreground">{t('chatBehind')}</Text>
        </View>
      </If>
      <If when={unknown()} else={
        <Scroll ref={(r: any) => { scroller = r }} class="flex-1" contentClass="px-3 py-3 gap-4"
          /* After a prompt sent from the composer its box keeps focus with the keyboard down, and a Scroll that doesn't
             persist taps spent every tap on that focus: a fold or a step didn't open (seen on Android). */
          keyboardShouldPersistTaps="handled" onLayout={(b) => viewport.set(b.size.height)}
          scrollEventThrottle={64} onScroll={(e) => { pinned = (e?.offset?.y ?? 0) + viewport() >= content - 40 }}>
          <View class="gap-4" onLayout={(b) => toEnd(b.size.height)}>
            <If when={turns()?.length} else={<If when={answer()}><Text class="text-[13px] text-muted-foreground text-center py-6">{t('chatEmpty')}</Text></If>}>
              <Each of={turns() ?? []} key={(x) => x.id}>{(x) => <TurnView store={s} address={props.address} turn={x()} carded={!!card()} back={props.back} />}</Each>
              <If when={card()}>{(c) => <AskCard store={s} address={props.address} agent={s.program(props.address).name} card={c} back={props.back} />}</If>
              <If when={behind()}><Text testID="chat-updating" class="text-[12.5px] text-muted-foreground">{t('chatUpdating')}</Text></If>
            </If>
          </View>
        </Scroll>
      }>{(why) => (
        <If when={why() === 'setup-needed' || why() === 'setup-not-loaded' ? answer()?.setup : undefined} else={
          <View class="flex-1 items-center justify-center px-8 gap-4">
            <Text testID="chat-unknown" class="text-[14px] text-muted-foreground text-center">{why() === 'gone' ? t('chatUnsure') : unknownText(why(), s.agents().find((x) => x.address === props.address)?.programName)}</Text>
            <View role="button" testID="chat-terminal" onPress={props.back} class="h-10 px-4 justify-center rounded-[8px] bg-secondary border border-border active:bg-accent">
              <Text class="text-[13.5px] text-foreground">{t('showTerminal')}</Text>
            </View>
          </View>
        }>{(agent) => <SetupPage store={s} address={props.address} agent={agent()} needed={why() === 'setup-needed'} status={props.status} back={props.back} again={() => void ask()} />}</If>
      )}</If>
    </View>
  )
}

/** A chat that needs its agent's setup (ChatSetup): the first time, the phone asks by itself, once; otherwise the page
 *  says what is missing, with Install, and the way back to the terminal. Installed while the agent runs: what to type in
 *  it, and a button that types it when the agent is idle. */
function SetupPage(props: { store: Store; address: string; agent: string; needed: boolean; status: () => string | undefined; back: () => void; again: () => void }) {
  const s = props.store
  const setup = signal<ChatSetup | undefined>(undefined)
  const asking = signal(false)
  const changed = signal(false)
  const busy = signal(false)
  const failed = signal('')
  const name = () => setup()?.name ?? props.agent
  void s.chat!.setups().then((all) => {
    setup.set(all.find((x) => x.agent === props.agent))
    if (props.needed && !s.chatSetupAsked.has(props.agent)) { s.chatSetupAsked.add(props.agent); asking.set(true) }
  }, () => {})
  const install = async (force = false) => {
    busy.set(true)
    failed.set('')
    try {
      const r = await s.chat!.setSetup(props.agent, true, force)
      if (r.setup) setup.set(r.setup)
      if (r.result === 'changed') { asking.set(false); changed.set(true); return }
      changed.set(false)
      asking.set(false)
      if (r.result === 'no-agent') failed.set(t('setupNoAgent', { name: name() }))
      else if (r.result === 'failed') failed.set(t('setupFailed'))
      else props.again()
    } catch (e) {
      failed.set(t('setupFailed'))
    } finally {
      busy.set(false)
    }
  }
  const idle = () => { const st = props.status(); return st !== 'working' && st !== 'blocked' }
  return (
    <View class="flex-1 px-5 pt-8 gap-3">
      <If when={props.needed} else={
        <View testID="chat-setup-loaded" class="gap-3">
          <Text class="text-[14px] text-foreground">{setup()?.reload ? t('setupReload', { name: name(), reload: setup()!.reload! }) : t('setupRestart', { name: name() })}</Text>
          <If when={setup()?.reload && idle()}>
            <View role="button" testID="chat-setup-reload" onPress={() => void s.compose(props.address, setup()!.reload!).catch(() => {})}
              class="self-start h-10 px-4 justify-center rounded-[8px] bg-primary active:opacity-80">
              <Text class="text-[13.5px] text-primary-foreground">{t('setupTypeReload', { reload: setup()!.reload! })}</Text>
            </View>
          </If>
        </View>
      }>
        <Text class="text-[16px] font-semibold text-foreground">{t('setupNeededTitle', { name: name() })}</Text>
        <Text class="text-[14px] text-muted-foreground">{t('setupNeededBody', { name: name() })}</Text>
        <View role="button" testID="chat-setup-install" disabled={busy()} onPress={() => void install()}
          class={`self-start h-10 px-4 justify-center rounded-[8px] bg-primary ${busy() ? 'opacity-60' : 'active:opacity-80'}`}>
          <Text class="text-[13.5px] text-primary-foreground">{t('setupInstall')}</Text>
        </View>
      </If>
      <If when={failed()}><Text testID="chat-setup-failed" class="text-[13px] text-destructive">{failed()}</Text></If>
      <View role="button" testID="chat-terminal" onPress={props.back} class="self-start h-10 justify-center active:opacity-70">
        <Text class="text-[13.5px] text-muted-foreground">{t('showTerminal')}</Text>
      </View>
      <Sheet open={asking()} side="bottom" onOpenChange={(o) => { if (!o) asking.set(false) }} popupClass="bg-background rounded-t-[16px] pb-[env(safe-area-inset-bottom)]">
        <View testID="chat-setup-ask" class="px-5 pt-5 pb-4 gap-3">
          <Text class="text-[17px] font-semibold text-foreground">{t('setupAskTitle', { name: name() })}</Text>
          <Text class="text-[14px] text-muted-foreground">{t('setupAskBody', { name: name() })}</Text>
          <If when={setup()?.file}><Text selectable class="text-[12px] font-mono text-foreground bg-secondary rounded-[6px] px-2 py-1.5">{setup()!.file}</Text></If>
          <View class="gap-1">
            <Text class="text-[13px] text-muted-foreground">· {t('setupFactOne', { name: name() })}</Text>
            <Text class="text-[13px] text-muted-foreground">· {t('setupFactNet')}</Text>
            <Text class="text-[13px] text-muted-foreground">· {t('setupFactRemove')}</Text>
          </View>
          <View role="button" testID="chat-setup-yes" disabled={busy()} onPress={() => void install()} class="h-11 items-center justify-center rounded-[10px] bg-primary active:opacity-80">
            <Text class="text-[15px] font-medium text-primary-foreground">{t('setupInstall')}</Text>
          </View>
          <View role="button" testID="chat-setup-later" onPress={() => asking.set(false)} class="h-10 items-center justify-center active:opacity-70">
            <Text class="text-[15px] text-foreground">{t('setupLater')}</Text>
          </View>
        </View>
      </Sheet>
      <Sheet open={changed()} side="bottom" onOpenChange={(o) => { if (!o) changed.set(false) }} popupClass="bg-background rounded-t-[16px] pb-[env(safe-area-inset-bottom)]">
        <View testID="chat-setup-changed" class="px-5 pt-5 pb-4 gap-3">
          <Text class="text-[17px] font-semibold text-foreground">{t('setupChangedTitle')}</Text>
          <Text class="text-[14px] text-muted-foreground">{t('setupChangedBody', { file: setup()?.file ?? '' })}</Text>
          <View role="button" testID="chat-setup-replace" disabled={busy()} onPress={() => void install(true)} class="h-11 items-center justify-center rounded-[10px] bg-primary active:opacity-80">
            <Text class="text-[15px] font-medium text-primary-foreground">{t('setupReplace')}</Text>
          </View>
          <View role="button" testID="chat-setup-keep" onPress={() => changed.set(false)} class="h-10 items-center justify-center active:opacity-70">
            <Text class="text-[15px] text-foreground">{t('setupKeep')}</Text>
          </View>
        </View>
      </Sheet>
    </View>
  )
}

function TurnView(props: { store: Store; address: string; turn: ChatTurn; carded: boolean; back: () => void }) {
  const expanded = signal(false)
  const turn = () => props.turn
  const show = () => shows(turn(), expanded())
  return (
    <View class="gap-2">
      <If when={turn().from === 'you'} else={
        <View class="gap-1">
          <Text class="text-[11.5px] text-muted-foreground">{t('chatFromOther')}</Text>
          <Text selectable class="text-[13px] text-muted-foreground">{turn().said}</Text>
        </View>
      }>
        <View class="self-end max-w-[85%] items-end gap-1.5">
          <Each of={turn().images ?? []}>{(p) => <ChatImage store={props.store} address={props.address} picture={p()} end />}</Each>
          <If when={turn().said}>
            <View class="px-3 py-2 rounded-[12px] bg-secondary">
              <Text selectable class="text-[14px] text-foreground">{turn().said}</Text>
            </View>
          </If>
        </View>
      </If>
      <Each of={outsideFold(turn(), expanded())} key={(x) => x.id}>{(x) => <StepView store={props.store} address={props.address} step={x()} />}</Each>
      <If when={show().fold}>
        <View role="button" testID={`fold-${turn().id}`} onPress={() => expanded.set(!expanded())} class="flex-row items-center gap-1.5 py-1 active:opacity-70">
          <Glyph name={expanded() ? 'chevron-down' : 'chevron-right'} size={13} />
          <Text class="text-[12.5px] text-muted-foreground">{foldLine(turn())}</Text>
        </View>
      </If>
      <If when={show().steps}>
        <View class="gap-0.5 pl-1">
          <Each of={turn().steps} key={(x) => x.id}>{(x) => <StepView store={props.store} address={props.address} step={x()} />}</Each>
        </View>
      </If>
      <If when={turn().reply}>
        <TextView testID={`reply-${turn().id}`} markdown={turn().reply} selectable links="open" class="text-[14px] text-foreground" />
        <FileCards store={props.store} address={props.address} text={turn().reply} />
      </If>
      {/* A question the phone can't answer from here (its screen's menu isn't read): its words, numbered, and the terminal. */}
      <If when={turn().end === 'open' && !props.carded ? turn().ask : undefined}>{(a) => (
        <View testID={`ask-${turn().id}`} class="gap-1.5 px-3 py-2.5 rounded-[10px] border border-chart-2/40 bg-chart-2/10">
          <Text class="text-[14px] font-medium text-foreground">{a().questions?.[0]?.text ?? a().text}</Text>
          <Each of={a().questions?.[0]?.options.map((o) => o.label) ?? a().options} key={(o) => o}>{(o, i) => <Text class="text-[13.5px] text-foreground">{`${i() + 1}. ${o()}`}</Text>}</Each>
          <View class="flex-row items-center gap-3 pt-0.5">
            <Text class="flex-1 text-[12px] text-muted-foreground">{t('chatAnswerInTerminal')}</Text>
            <View role="button" testID={`ask-terminal-${turn().id}`} onPress={props.back} class="h-8 px-3 justify-center rounded-[8px] bg-secondary border border-border active:bg-accent">
              <Text class="text-[12.5px] text-foreground">{t('showTerminal')}</Text>
            </View>
          </View>
        </View>
      )}</If>
      <If when={turn().end === 'open' && !turn().ask && !props.carded}><Text class="text-[12.5px] text-muted-foreground">{t('chatWorking')}</Text></If>
      <If when={turn().end === 'stopped'}><Text class="text-[12.5px] text-muted-foreground">{t('chatStopped')}</Text></If>
    </View>
  )
}

function StepView(props: { store: Store; address: string; step: ChatStep }) {
  // A message of yours the agent took while it worked: where it took it, as yours; nothing more to fetch.
  if (props.step.kind === 'you') return (
    <View testID={`you-${props.step.id}`} class="self-end max-w-[85%] items-end gap-1 py-1.5">
      <View class="px-3 py-2 rounded-[12px] bg-secondary"><Text selectable class="text-[14px] text-foreground">{props.step.title}</Text></View>
      <Text class="text-[11.5px] text-muted-foreground">{t('chatTakenMidTurn')}</Text>
    </View>
  )
  const detail = signal<ChatDetail | null | undefined>(undefined)
  const open = signal(false)
  const toggle = async () => {
    if (open()) { open.set(false); return }
    open.set(true)
    if (detail() === undefined) detail.set(await props.store.chat!.step(props.address, props.step.id).catch(() => null))
  }
  return (
    <View>
      <View role="button" testID={`step-${props.step.id}`} onPress={() => void toggle()} class="flex-row items-center gap-2 min-h-[30px] active:opacity-70">
        <If when={STEP_GLYPH[props.step.kind]} else={<View style={{ width: 13 }} />}>{(g) => <Glyph name={g() as any} size={13} />}</If>
        <Text class={`flex-1 text-[13px] ${props.step.kind === 'note' || props.step.kind === 'think' ? 'text-muted-foreground italic' : 'text-foreground'}`} numberOfLines={2}>{props.step.title}</Text>
        <If when={props.step.failed}><Text class="text-[11.5px] text-destructive">{t('chatFailed')}</Text></If>
        <If when={!props.step.failed && (props.step.added || props.step.deleted)}>
          <Text class="text-[11.5px] text-muted-foreground">{`+${props.step.added ?? 0} −${props.step.deleted ?? 0}`}</Text>
        </If>
      </View>
      <If when={open() && detail() !== undefined}>
        <View class="ml-5 mb-1 px-2.5 py-2 rounded-[8px] bg-popover border border-border gap-2">
          <If when={detail()} else={<Text class="text-[12px] text-muted-foreground">{t('stepGone')}</Text>}>{(d) => (
            <View class="gap-2">
              <If when={d().run} else={<If when={d().change} else={<If when={d().read} else={
                <>
                  <Text selectable class={props.step.kind === 'note' || props.step.kind === 'think' ? 'text-[13px] text-foreground' : 'text-[11.5px] font-mono text-foreground'}>{d().input}</Text>
                  <If when={d().output}><Text selectable class="text-[11.5px] font-mono text-muted-foreground">{d().output}</Text></If>
                </>
              }>{(r) => <Text testID={`read-${props.step.id}`} class="text-[12px] text-muted-foreground">{t('chatReadLines', { from: r().from, to: r().from + Math.max(r().count, 1) - 1, total: r().total })}</Text>}</If>
              }>{(c) => <ChangeView id={props.step.id} change={c()} />}</If>
              }>{(r) => <RunView id={props.step.id} run={r()} />}</If>
              <Each of={d().images ?? []}>{(p) => <ChatImage store={props.store} address={props.address} picture={p()} />}</Each>
            </View>
          )}</If>
        </View>
      </If>
    </View>
  )
}

const FILE_GLYPH: Record<ChatFile['kind'], string> = { video: 'monitor', pdf: 'file-text', text: 'file-text', image: 'file', other: 'file' }
const FILE_WORD: Record<ChatFile['kind'], () => string> = { video: () => t('fileVideo'), pdf: () => t('filePdf'), text: () => t('fileText'), image: () => t('fileImage'), other: () => t('fileOther') }
/** How much of a text file the app shows. */
const TEXT_MAX = 512 << 10
/** The frames made of videos (null: being made), kept while the app runs. */
const FRAMES = new Map<string, VideoFrame | null>()

/** The files a reply names that the computer has, as cards under it: what each is, its name and size; a video with a
 *  frame of it and its length when the phone can make one. Text opens in the app, a picture in the picture sheet, a
 *  video in the player; anything else in another app on the phone. */
function FileCards(props: { store: Store; address: string; text: string }) {
  const files = signal<ChatFile[]>([])
  const open = signal<ChatFile | null>(null)
  effect(() => {
    const paths = pathsIn(props.text)
    untracked(() => { if (paths.length) void props.store.chat!.files(props.address, paths).then((f) => files.set(f), () => {}) })
  })
  // A frame of each video, made once per file as the computer has it now.
  const key = (f: ChatFile) => `${props.address}|${f.path}|${f.size}`
  const frames = signal<Record<string, VideoFrame>>({})
  effect(() => {
    for (const f of files()) untracked(() => {
      if (f.kind !== 'video') return
      const k = key(f)
      if (FRAMES.has(k)) { const had = FRAMES.get(k); if (had) frames.set({ ...frames(), [k]: had }); return }
      FRAMES.set(k, null)
      void props.store.chat!.fileLink(props.address, f.path).then((url) => (url ? videoFrame(url) : null), () => null).then((v) => {
        if (!v) { FRAMES.delete(k); return }
        FRAMES.set(k, v)
        frames.set({ ...frames(), [k]: v })
      })
    })
  })
  const playing = signal<ChatFile | null>(null)
  const press = (f: ChatFile) => {
    // Right after a prompt the keyboard is still up, and it stayed over the player and the sheets (seen on iOS).
    Keyboard.dismiss()
    if (f.kind === 'text') open.set(f)
    else if (f.kind === 'video') playing.set(f)
    else if (f.kind === 'image') props.store.picture.set({ path: f.path, terminal: props.address })
    else void props.store.chat!.fileLink(props.address, f.path).then((url) => (url ? openFile({ url, name: f.name, kind: f.kind }) : 'failed' as const), () => 'failed' as const).then((r) => {
      if (r === 'no_app') props.store.toast({ title: t('fileNoApp'), level: 'error' })
      else if (r === 'failed') props.store.toast({ title: t('fileCantGet'), level: 'error' })
    })
  }
  return (
    <If when={files().length}>
      <View class="gap-2">
        <Each of={files()} key={(f) => f.asked}>{(f) => (
          <If when={f().kind === 'video' && frames()[key(f())]} else={
          <View role="button" testID={`file-${f().asked}`} accessibilityLabel={f().name} onPress={() => press(f())}
            class="flex-row items-center gap-2.5 px-2.5 py-2 rounded-[10px] border border-border bg-card active:bg-accent">
            <View class="w-9 h-11 rounded-[6px] bg-secondary items-center justify-center"><Glyph name={FILE_GLYPH[f().kind] as any} size={18} /></View>
            <View class="flex-1 min-w-0 gap-0.5">
              <Text class="text-[13.5px] font-medium text-foreground" numberOfLines={1}>{f().name}</Text>
              <Text class="text-[11.5px] text-muted-foreground" numberOfLines={1}>{`${FILE_WORD[f().kind]()} · ${sizeText(f().size)}`}</Text>
            </View>
            <Glyph name="chevron-right" size={15} />
          </View>}>
            <View role="button" testID={`file-${f().asked}`} accessibilityLabel={f().name} onPress={() => press(f())}
              class="rounded-[10px] border border-border bg-card overflow-hidden active:opacity-80">
              <View class="w-full aspect-video bg-black">
                <Image testID={`video-frame-${f().asked}`} src={frames()[key(f())]?.image ?? ''} resizeMode="cover" class="w-full h-full" />
                <View class="absolute inset-0 items-center justify-center" pointerEvents="none">
                  <View class="w-12 h-12 rounded-full bg-black/60 items-center justify-center"><Glyph name="play" size={20} class="tint-white" /></View>
                </View>
                <If when={(frames()[key(f())]?.duration ?? 0) > 0}>
                  <Text class="absolute right-2 bottom-2 px-1.5 py-0.5 rounded-[4px] bg-black/70 text-[11.5px] text-white">{clockText(frames()[key(f())]?.duration ?? 0)}</Text>
                </If>
              </View>
              <View class="px-2.5 py-2 gap-0.5">
                <Text class="text-[13.5px] font-medium text-foreground" numberOfLines={1}>{f().name}</Text>
                <Text class="text-[11.5px] text-muted-foreground" numberOfLines={1}>{`${FILE_WORD[f().kind]()} · ${sizeText(f().size)}`}</Text>
              </View>
            </View>
          </If>
        )}</Each>
        <TextSheet store={props.store} address={props.address} file={open} />
        <VideoSheet store={props.store} address={props.address} file={playing} />
      </View>
    </If>
  )
}

/** A video from the computer, played in the app as it comes (the player reads it in parts, by a link for that file). */
function VideoSheet(props: { store: Store; address: string; file: { (): ChatFile | null; set: (v: ChatFile | null) => void } }) {
  const src = signal<string | null | undefined>(undefined)
  const failed = signal(false)
  effect(() => {
    const f = props.file()
    untracked(() => {
      src.set(undefined)
      failed.set(false)
      if (f) void props.store.chat!.fileLink(props.address, f.path).then((u) => { if (props.file() === f) src.set(u) }, () => { if (props.file() === f) src.set(null) })
    })
  })
  return (
    <Sheet open={props.file() !== null} side="bottom" onOpenChange={(o) => { if (!o) props.file.set(null) }} popupClass="bg-black pb-[env(safe-area-inset-bottom)] h-full">
      <View class="flex-row items-center gap-2 pl-4 pr-1 pt-[env(safe-area-inset-top)]">
        <View class="flex-1 min-w-0">
          <Text class="text-[14px] font-semibold text-white" numberOfLines={1}>{props.file()?.name ?? ''}</Text>
          <Text class="text-[11.5px] text-white/60" numberOfLines={1}>{sizeText(props.file()?.size ?? 0)}</Text>
        </View>
        <View role="button" accessibilityLabel={t('close')} testID="video-close" onPress={() => props.file.set(null)} class="w-11 h-11 items-center justify-center active:opacity-60">
          <Glyph name="close" size={18} class="tint-white" />
        </View>
      </View>
      <View class="flex-1 justify-center">
        <If when={src() && !failed()} else={<If when={src() === null || failed()}><Text class="text-[13px] text-white/70 text-center px-6">{t('videoCantPlay')}</Text></If>}>
          <Video testID="video-player" src={src() ?? ''} controls resizeMode="contain" class="w-full h-full" onError={() => failed.set(true)} />
        </If>
      </View>
      <Text selectable class="px-4 py-3 text-[11.5px] font-mono text-white/60" numberOfLines={2}>{props.file()?.path ?? ''}</Text>
    </Sheet>
  )
}

/** A text file in the app: Markdown as Markdown, anything else as it is, in a monospaced font. */
function TextSheet(props: { store: Store; address: string; file: { (): ChatFile | null; set: (v: ChatFile | null) => void } }) {
  const text = signal<string | null | undefined>(undefined)
  effect(() => {
    const f = props.file()
    untracked(() => {
      text.set(undefined)
      if (f) void props.store.chat!.fileText(props.address, f.path, TEXT_MAX).then((x) => { if (props.file() === f) text.set(x) }, () => { if (props.file() === f) text.set(null) })
    })
  })
  const markdown = () => /\.(md|markdown)$/i.test(props.file()?.name ?? '')
  return (
    <Sheet open={props.file() !== null} side="bottom" onOpenChange={(o) => { if (!o) props.file.set(null) }} popupClass="bg-popover rounded-t-[14px] pb-[env(safe-area-inset-bottom)] h-[85%]">
      <View class="flex-row items-center gap-2 pl-4 pr-1 py-1 border-b border-border">
        <Text class="flex-1 text-[14px] font-semibold text-foreground" numberOfLines={1}>{props.file()?.name ?? ''}</Text>
        <IconButton name="close" label={t('close')} testID="file-close" onPress={() => props.file.set(null)} />
      </View>
      <Scroll class="flex-1" contentClass="px-4 py-3">
        <If when={text() !== undefined}>
          <If when={text() !== null} else={<Text class="text-[13px] text-muted-foreground">{t('fileCantRead')}</Text>}>
            <If when={markdown()} else={<Text selectable class="text-[12px] font-mono text-foreground">{text() ?? ''}</Text>}>
              <TextView markdown={text() ?? ''} selectable links="open" class="text-[14px] text-foreground" />
            </If>
          </If>
        </If>
      </Scroll>
    </Sheet>
  )
}

/** How many of a command's last lines show before a tap shows them all, and how many lines of a change. */
const RUN_TAIL = 8
const CHANGE_SHOWN = 30

/** A command: itself, whether it failed (and its exit code), then the end of what it printed; the rest a tap away. */
function RunView(props: { id: string; run: ChatRun }) {
  const all = signal(false)
  const lines = () => (props.run.output ? props.run.output.split('\n') : [])
  return (
    <View testID={`run-${props.id}`} class="gap-1.5">
      <View class="flex-row gap-1">
        <Text class="text-[11.5px] font-mono text-muted-foreground">$</Text>
        <Text selectable class="flex-1 text-[11.5px] font-mono text-foreground">{props.run.command}</Text>
      </View>
      <If when={props.run.failed}>
        <Text class="text-[12px] text-destructive">{props.run.code ? t('chatExitCode', { code: props.run.code }) : t('chatFailed')}</Text>
      </If>
      <If when={lines().length}>
        <Text selectable class="text-[11.5px] font-mono text-muted-foreground">{(all() ? lines() : lines().slice(-RUN_TAIL)).join('\n')}</Text>
      </If>
      <If when={!all() && lines().length > RUN_TAIL}>
        <View role="button" testID={`run-all-${props.id}`} onPress={() => all.set(true)} class="py-1 active:opacity-70">
          <Text class="text-[12px] text-foreground">{t('chatShowAll', { n: props.run.lines })}</Text>
        </View>
      </If>
    </View>
  )
}

/** A change to a file (its line says how many lines were added and removed): its lines, numbered as in the file, an added one
 *  tinted blue and a removed one red (told apart by their marks too, not by colour alone). */
function ChangeView(props: { id: string; change: ChatChange }) {
  const all = signal(false)
  const rows = () => changeRows(props.change)
  return (
    <View testID={`change-${props.id}`} class="gap-1.5">
      <View class="rounded-[6px] overflow-hidden border border-border">
        <Each of={all() ? rows() : rows().slice(0, CHANGE_SHOWN)} key={(r) => r.key}>{(r) => (
          <View class={`flex-row ${r().mark === '+' ? 'bg-chart-3/10' : r().mark === '-' ? 'bg-destructive/10' : ''}`}>
            <Text class="w-[34px] pr-1.5 text-right text-[11px] font-mono text-muted-foreground">{r().mark === 'gap' ? '' : String(r().num)}</Text>
            <Text class="w-[12px] text-[11px] font-mono text-muted-foreground">{r().mark === 'gap' || r().mark === ' ' ? '' : r().mark === '-' ? '−' : '+'}</Text>
            <Text selectable class="flex-1 pr-1.5 text-[11px] font-mono text-foreground">{r().text}</Text>
          </View>
        )}</Each>
      </View>
      <If when={!all() && rows().length > CHANGE_SHOWN}>
        <View role="button" testID={`change-all-${props.id}`} onPress={() => all.set(true)} class="py-1 active:opacity-70">
          <Text class="text-[12px] text-foreground">{t('chatShowAll', { n: rows().length })}</Text>
        </View>
      </If>
      <If when={props.change.cut && (all() || rows().length <= CHANGE_SHOWN)}>
        <Text class="text-[12px] text-muted-foreground">{t('chatChangeCut', { n: rows().length })}</Text>
      </If>
    </View>
  )
}

/** One image of the record under the path it came from (when the record says; selectable, to copy), fetched as a
 *  data: URL when it shows; a box of its own size, at most 220 × 220. `end`: in your bubble's column, on the right. */
function ChatImage(props: { store: Store; address: string; picture: ChatPicture; end?: boolean }) {
  const id = props.picture.id
  const url = signal<string | null | undefined>(undefined)
  const size = signal({ width: 160, height: 120 })
  void props.store.chat!.image(props.address, id).catch(() => null).then((u) => url.set(u))
  const fit = (w: number, h: number) => { const k = Math.min(1, 220 / Math.max(w, h, 1)); size.set({ width: Math.max(24, w * k), height: Math.max(24, h * k) }) }
  return (
    <View class={`gap-1 ${props.end ? 'items-end' : 'items-start'}`}>
      <If when={props.picture.source}>{(s) => (
        <Text selectable testID={`image-source-${id}`} class={`text-[11.5px] font-mono text-muted-foreground ${props.end ? 'text-right' : ''}`}>{s()}</Text>
      )}</If>
      <If when={url()} else={<View class="rounded-[8px] bg-secondary border border-border" style={{ width: 64, height: 64 }} />}>{(u) => (
        <Image testID={`image-${id}`} src={u()} resizeMode="contain" onLoad={(e) => fit(e.width, e.height)}
          class="rounded-[8px] border border-border" style={size()} />
      )}</If>
    </View>
  )
}
