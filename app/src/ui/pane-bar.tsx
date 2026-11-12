// The row under a pane's terminal on a phone: the composer; for a blocked agent, its own choices as its screen shows
// them (model/menu.ts), or the keys when they can't be read; and, while the keyboard is up, the key row under them.
import { cleanup, effect, Keyboard, keyboardFrame, Picker, platform, signal, Text, View } from 'nsh'
import { AlertDialog, Input } from 'nsh/base'
import type { Store } from '../store.ts'
import { explain } from '../store.ts'
import { MAX_FILE } from '../link.ts'
import { machineOfAddress } from '../model/model.ts'
import { asWord, withPath } from './attach.ts'
import { asks } from '../model/answers.ts'
import { shownOrder, type Option } from '../model/menu.ts'
import type { BarKey } from './terminal-page.ts'
import type { TerminalSession } from './terminal-session.ts'
import type { TypeIn } from '../prefs.ts'
import { ChoiceRow, Glyph, useRev, useSignal } from './kit.tsx'
import { t } from '../strings/index.ts'


export type RowKey = BarKey | 'ctrl' | 'shift'
const ROW_KEYS: { key: RowKey; label: string }[] = [
  { key: 'esc', label: 'esc' }, { key: 'shift', label: '⇧' }, { key: 'tab', label: 'tab' }, { key: 'ctrl', label: 'ctrl' },
  { key: 'ctrlc', label: '^C' }, { key: 'left', label: '←' }, { key: 'up', label: '↑' }, { key: 'down', label: '↓' }, { key: 'right', label: '→' }, { key: 'enter', label: '↵' },
]
/** Shift and Ctrl are one tap each and go with the next key, so the keys they change say what they are about to send. */
/** One Esc on screen. The composer's and the fallback keys' are there with the keyboard down too, to stop an agent; only
 *  with choices on screen does the row need its own. */
export const rowKeys = (mode: 'compose' | 'choices' | 'keys', here: TypeIn = 'composer') => ROW_KEYS.filter((k) => k.key !== 'esc' || mode === 'choices' || (mode === 'compose' && here === 'terminal'))

/** The composer grows with its text up to this, then scrolls. */
const COMPOSER_MIN = 36
const COMPOSER_MAX = 132

const FALLBACK_KEYS: { key: string; label: string }[] = [
  { key: 'esc', label: 'esc' }, { key: 'up', label: '↑' }, { key: 'down', label: '↓' }, { key: 'left', label: '←' }, { key: 'right', label: '→' }, { key: 'enter', label: 'enter' },
]

/** How long Codex has to start and send its first report before its silence is worth a line. */
const CODEX_QUIET_MS = 5000

export function PaneBar(props: { store: Store; session: TerminalSession; program: string; agent: boolean; typeIn: () => TypeIn; keep: (m: TypeIn) => void }) {
  const s = props.store
  const rev = useRev(s)
  const addr = props.session.address
  const status = () => { rev(); return s.term(addr)?.status }
  const blocked = () => status()?.state === 'blocked'
  // Codex before 0.163 sends no status, so it never alerts and looks like one that has nothing to say.
  const waited = signal(false)
  const quietTimer = setTimeout(() => waited.set(true), CODEX_QUIET_MS)
  cleanup(() => clearTimeout(quietTimer))
  const codexQuiet = () => { rev(); return waited() && s.program(addr).key === 'codex' && !s.term(addr)?.reported }
  const menu = () => { rev(); return s.menuFor(addr) }
  const read = () => { rev(); return s.menuRead(addr) }
  // After a choice that wants text (here or on home), or "type instead": the composer, until the agent stops being blocked.
  const focusFirst = s.wantsComposerFocus(addr)
  const typing = signal(focusFirst)
  const composing = signal(false)
  effect(() => { if (!blocked()) typing.set(false) })
  const mode = () => {
    if (!blocked() || typing()) return 'compose'
    if (menu()) return 'choices'
    if (read() && status()?.kind !== 'question') return 'keys'
    return 'compose'
  }
  // Where this pane types: the phone's kept choice when it opened; a switch here is for this pane until it is kept.
  props.session.typeIn.set(props.typeIn())
  const here = useSignal(props.session.typeIn)
  const raised = signal(false)
  // Opened typing in the terminal: the keyboard comes up there once the page can take it.
  if (here() === 'terminal') props.session.focus()
  const switchTo = (m: TypeIn) => {
    props.session.typeIn.set(m)
    if (m === 'terminal') props.session.focus()
    else raised.set(true)
    if (m !== props.typeIn()) s.toast({ title: m === 'terminal' ? t('typingInTerminal') : t('typingInBox'), body: t('tapToKeep'), level: 'info',
      action: { run: () => { props.keep(m); s.toast({ title: m === 'terminal' ? t('keptTerminal') : t('keptBox'), level: 'info' }) } } })
  }
  const pick = async (o: Option) => {
    try { await s.answer(addr, { keys: o.keys }) } catch {}
    if (o.typed) typing.set(true)
  }
  const hint = () => (blocked() && status()?.kind === 'question' && !menu() ? asks(status()) : '') || (props.agent ? t('tellProgram', { name: props.program }) : t('typeCommand'))
  return (
    <View class="bg-sidebar border-t border-border">
      <If when={codexQuiet()}><Text testID="codex-quiet" class="px-3 pt-1.5 text-[12px] text-sidebar-foreground">{t('codexQuiet')}</Text></If>
      <If when={mode() === 'choices' ? menu() : undefined}>{(m) => (
        <View testID="answers" class="flex-row items-center gap-1.5 pl-2 pr-1 py-1">
          <ChoiceRow testID="choices">
            {shownOrder(m()).map((o) => (
              <View role="button" testID={`choice-${o.n}`} accessibilityLabel={`${o.n}. ${o.full}`} onPress={() => void pick(o)}
                onLongPress={() => s.toast({ title: `${o.n}. ${o.full}`, level: 'info' })}
                class={`h-9 max-w-[230px] px-3.5 justify-center rounded-[8px] ${o.selected ? 'bg-primary active:opacity-80' : 'bg-secondary border border-border active:bg-accent'}`}>
                <Text class={`text-[14px] font-medium ${o.selected ? 'text-primary-foreground' : 'text-foreground'}`} numberOfLines={1}>{`${o.n}. ${o.label}`}</Text>
              </View>
            ))}
          </ChoiceRow>
          <BarButton glyph="keyboard" label={t('typeInstead')} testID="show-composer" onPress={() => { typing.set(true); if (here() === 'terminal') props.session.focus() }} />
        </View>
      )}</If>
      <If when={mode() === 'keys'}>
        <View testID="keys-fallback" class="px-2 py-1 gap-1">
          <Text class="text-[11.5px] text-sidebar-foreground px-0.5">{t('cantReadChoices', { name: props.program })}</Text>
          <View class="flex-row gap-1">
            {FALLBACK_KEYS.map((k) => <Key label={k.label} testID={`key-${k.key}`} onPress={() => void s.answer(addr, { keys: [k.key] }).catch(() => {})} />)}
          </View>
        </View>
      </If>
      <If when={mode() === 'compose' && here() === 'composer'}>
        <Composer toTerminal={() => switchTo('terminal')} keepTerminal={() => keyboardFrame().height > 0 && !composing()} store={s} session={props.session} hint={hint()} focus={raised() || focusFirst || typing() || (blocked() && status()?.kind === 'question' && !menu())}
          focused={(f) => { composing.set(f); if (f) s.focusComposerOn(undefined) }} />
      </If>
      <If when={keyboardFrame().height > 0 || (mode() === 'compose' && here() === 'terminal')}>
        <KeyRow keys={rowKeys(mode(), here())} session={props.session} keepTerminal={() => !composing()} toBox={mode() === 'compose' && here() === 'terminal' ? () => switchTo('composer') : undefined} />
      </If>
    </View>
  )
}

/** Attach: a photo or a file from the phone, sent to the machine the terminal is on; `land` gets each one's upload
 *  and puts its path where typing goes (the input box, or the terminal). */
export async function attachFiles(store: Store, addr: string, land: (name: string, go: () => Promise<string>) => Promise<void>) {
  const api = store.files
  if (!api) return
  const failed = (body: string) => store.toast({ title: t('couldntAttach'), body, level: 'error' })
  const choice = await AlertDialog.show({ title: t('attach'), presentation: 'actionSheet', actions: [
    { id: 'photo', label: t('attachPhoto') }, { id: 'file', label: t('attachFile') }, { id: 'cancel', label: t('cancel'), role: 'cancel' }] })
  const on = machineOfAddress(addr)
  try {
    if (choice === 'photo') {
      // An agent needs no 48 megapixels: scaled on the phone, which also drops the location.
      for (const { file } of await Picker.pickImages({ multiple: true, limit: 9, maxDimension: 2048, quality: 0.85 })) await land(file.name, () => api.sendPhoto(file, on))
    } else if (choice === 'file') {
      for (const f of await api.pick()) {
        if (f.size > MAX_FILE) failed(t('fileTooBig', { name: f.name }))
        else await land(f.name, () => api.sendPicked(f, on))
      }
    }
  } catch (e) { failed(explain(e)) }
}

/** Attach while typing in the terminal: each path goes in as typed text, followed by a space. */
export function attachToTerminal(store: Store, session: TerminalSession) {
  return attachFiles(store, session.address, async (name, go) => {
    store.toast({ title: t('uploading'), level: 'info' })
    try { session.paste(asWord(await go()) + ' ') } catch (e) { store.toast({ title: t('couldntAttach'), body: `${name}: ${explain(e)}`, level: 'error' }) }
  })
}

const CJK = /[\u3000-\u30ff\u3400-\u9fff\uac00-\ud7af\uff00-\uffef]/
/** The input box's text went from `before` to `after` by a word picked in Chinese, Japanese or Korean: the changed
 *  part now holds such a word, where before it held none (the pinyin's letters, or nothing). */
export function pickedWord(before: string, after: string): boolean {
  const a = Array.from(before), b = Array.from(after)
  let p = 0
  while (p < a.length && p < b.length && a[p] === b[p]) p++
  let e = 0
  while (e < a.length - p && e < b.length - p && a[a.length - 1 - e] === b[b.length - 1 - e]) e++
  const was = a.slice(p, a.length - e).join(''), now = b.slice(p, b.length - e).join('')
  return CJK.test(now) && !CJK.test(was)
}
/** An invisible character the box holds for a moment after such a pick: see the Composer. */
const NUDGE = '\u200b'

function Composer(props: { toTerminal: () => void; keepTerminal: () => boolean; store: Store; session: TerminalSession; hint: string; focus: boolean; focused: (f: boolean) => void }) {
  const addr = props.session.address
  const text = signal('')
  const pending = useSignal(props.store.pending)
  const uploading = signal(0)
  const waiting = () => pending().filter((p) => p.address === addr).length
  const tall = signal(COMPOSER_MIN)
  const send = () => {
    const typed = text().replace(new RegExp(NUDGE, 'g'), '')
    if (!typed.trim()) return
    text.set('')
    tall.set(COMPOSER_MIN)
    props.session.claim()
    void props.store.compose(addr, typed).catch((e) => props.store.toast({ title: t('couldntSend'), body: String(e?.message ?? e), level: 'error' }))
  }
  const failed = (body: string) => props.store.toast({ title: t('couldntAttach'), body, level: 'error' })
  // One after another: a path joins the text when its file has landed, on the machine the terminal is on.
  const land = async (name: string, go: () => Promise<string>) => {
    uploading.set(uploading() + 1)
    try { text.set(withPath(text(), await go())) } catch (e) { failed(`${name}: ${explain(e)}`) }
    uploading.set(uploading() - 1)
  }
  const attach = () => attachFiles(props.store, addr, land)
  return (
    <View>
      <If when={uploading()}><Text testID="uploading" class="px-3 pt-1 text-[11.5px] text-sidebar-foreground">{t('uploading')}</Text></If>
      <If when={waiting()}>
        <Text testID="pending" class="px-3 pt-1 text-[11.5px] text-sidebar-foreground">{t('pendingSend', { n: waiting() })}</Text>
      </If>
      <View testID="composer" class="flex-row items-end gap-1.5 px-2 py-1">
        <Key label="esc" testID="key-esc" grow={false} onPress={() => {
          props.session.key('esc')
          // Typing in the terminal goes on there, as after the row's keys.
          if (props.keepTerminal()) props.session.focus()
        }} />
        <Input testID="compose-input" value={text()} onChange={(t) => {
            const before = text()
            text.set(t)
            // Gboard (Android) keeps a word picked in pinyin as its own and spends the next backspace on it, deleting
            // nothing (API 35 emulator: 你好 and one backspace sent 你好). The box sets its text twice, as
            // anything else that changes it would, which restarts the keyboard's input (Android's setText) and lets the
            // word go.
            if (platform.os === 'android' && pickedWord(before, t)) {
              text.set(t + NUDGE)
              setTimeout(() => { if (text() === t + NUDGE) text.set(t) }, 30)
            }
          }} placeholder={props.hint} autoFocus={props.focus}
          multiline submitBehavior="newline" onFocus={() => { props.session.claim(); props.focused(true) }} onBlur={() => props.focused(false)}
          onContentSizeChange={(e) => tall.set(Math.max(COMPOSER_MIN, Math.min(COMPOSER_MAX, e.height + 16)))} style={{ height: text() ? tall() : COMPOSER_MIN }}
          class="flex-1 rounded-[8px] border border-input bg-background px-3 py-2 text-[14px] text-foreground placeholder:text-sidebar-foreground" />
        <BarButton glyph="terminal" label={t('typeInTerminal')} testID="to-terminal" onPress={props.toTerminal} />
        <BarButton glyph="plus" label={t('attach')} testID="attach" onPress={() => void attach()} />
        <If when={text().trim()}>
          <BarButton glyph="send" label={t('send')} testID="compose-send" primary onPress={send} />
        </If>
      </View>
    </View>
  )
}

/** How the key row draws its keys. `word`: ctrl and ^C; `symbol`: ⌃ and ⌃C, as ⇧ is for Shift. `label`: each key as wide
 *  as its label needs, the rest of the row shared out; `equal`: all as wide as each other. */
export type KeyRowLook = { labels: 'word' | 'symbol'; size: 'label' | 'equal' }
export const KEY_ROW_LOOK: KeyRowLook = { labels: 'symbol', size: 'label' }
const SYMBOLS: Partial<Record<RowKey, string>> = { ctrl: '⌃', ctrlc: '⌃C' }
/** Key labels follow the system's text size up to this: a key row that grows with it leaves the row (a keyboard's own
 *  keys don't grow either). */
export const KEY_ROW_MAX_SCALE = 1.3

/** Directly above the keyboard: the keys a phone keyboard lacks, straight to the pane, and one to put it away.
 *  `onFit` hears, after each layout, whether every label is on one line and whole (e2e/key-row.test.tsx). */
export function KeyRow(props: { keys: { key: RowKey; label: string }[]; session: TerminalSession; keepTerminal: () => boolean; toBox?: () => void
  look?: KeyRowLook; keyboardUp?: () => boolean; onFit?: (fits: boolean, cramped: string[]) => void }) {
  const look = props.look ?? KEY_ROW_LOOK
  const ctrl = useSignal(props.session.ctrl)
  const shift = useSignal(props.session.shift)
  const held = (k: RowKey) => (k === 'ctrl' && ctrl()) || (k === 'shift' && shift())
  const named = (k: { key: RowKey; label: string }) => (look.labels === 'symbol' && SYMBOLS[k.key]) || k.label
  // Held Shift and Ctrl show on their own keys only: a mark on every other label made the row wrap on a 375 pt phone.
  const label = named
  const up = () => props.keyboardUp ? props.keyboardUp() : keyboardFrame().height > 0
  const press = (k: RowKey) => {
    if (k === 'ctrl') props.session.toggleCtrl()
    else if (k === 'shift') props.session.toggleShift()
    else props.session.key(k)
    // Typing in the terminal goes on there after a key (Enter included); typing in the composer stays in it.
    if (props.keepTerminal()) props.session.focus()
  }
  const cramped = new Map<string, boolean>()
  const laid = (k: { key: RowKey; label: string }) => (e: { lines: { text: string }[] }) => {
    if (!props.onFit) return
    cramped.set(k.key, !(e.lines.length === 1 && e.lines[0].text.trim() === label(k)))
    const bad = [...cramped].filter(([, c]) => c).map(([key]) => key)
    props.onFit(bad.length === 0, bad)
  }
  const side = 'w-8 h-8 items-center justify-center rounded-[6px] bg-secondary active:bg-accent'
  return (
    <View testID="key-row" class="flex-row items-center gap-[3px] px-1 pb-1">
      {/* Kept by key: the pane redraws whenever any agent changes, and a key drawn again under the finger lost its press. */}
      <Each of={props.keys} key={(k) => k.key}>{(item) => {
        const k = item()
        return (
          <View role="button" focusable={false} testID={`row-${k.key}`} accessibilityLabel={k.key === 'ctrl' ? 'ctrl' : k.key === 'ctrlc' ? 'ctrl C' : k.label} onPress={() => press(k.key)}
            class={`${look.size === 'label' ? 'flex-auto px-1' : 'flex-1'} h-8 items-center justify-center rounded-[6px] ${held(k.key) ? 'bg-primary' : 'bg-secondary active:bg-accent'}`}>
            <Text maxFontSizeMultiplier={KEY_ROW_MAX_SCALE} numberOfLines={1} adjustsFontSizeToFit minimumFontScale={0.7} onTextLayout={laid(k)}
              class={`${k.key === 'enter' || k.key === 'shift' ? 'text-[16px]' : 'text-[12px]'} font-mono ${held(k.key) ? 'text-primary-foreground' : 'text-foreground'}`}>{label(k)}</Text>
          </View>
        )
      }}</Each>
      <If when={props.toBox}>
        <View role="button" focusable={false} testID="to-box" accessibilityLabel={t('typeInBox')} onPress={() => props.toBox?.()} class={side}>
          <Glyph name="square-pen" size={15} class="tint-foreground" />
        </View>
      </If>
      <If when={up()} else={
        <View role="button" focusable={false} testID="show-keyboard" accessibilityLabel={t('showKeyboard')} onPress={() => props.session.focus()} class={side}>
          <Glyph name="keyboard" size={15} class="tint-foreground" />
        </View>
      }>
        <View role="button" focusable={false} testID="hide-keyboard" accessibilityLabel={t('hideKeyboard')} onPress={() => Keyboard.dismiss()} class={side}>
          <Glyph name="keyboard-down" size={15} class="tint-foreground" />
        </View>
      </If>
    </View>
  )
}

function Key(props: { label: string; testID: string; onPress: () => void; grow?: boolean }) {
  return (
    <View role="button" focusable={false} testID={props.testID} accessibilityLabel={props.label} onPress={props.onPress}
      class={`${props.grow === false ? 'px-2.5' : 'flex-1'} h-9 items-center justify-center rounded-[8px] bg-secondary active:bg-accent`}>
      <Text class="text-[13px] font-mono text-foreground">{props.label}</Text>
    </View>
  )
}

function BarButton(props: { glyph: string; label: string; testID: string; onPress: () => void; primary?: boolean }) {
  return (
    <View role="button" focusable={false} testID={props.testID} accessibilityLabel={props.label} onPress={props.onPress}
      class={`w-10 h-9 items-center justify-center rounded-[8px] ${props.primary ? 'bg-primary active:opacity-80' : 'active:bg-accent'}`}>
      <Glyph name={props.glyph} size={18} class={props.primary ? 'tint-primary-foreground' : 'tint-muted-foreground'} />
    </View>
  )
}
