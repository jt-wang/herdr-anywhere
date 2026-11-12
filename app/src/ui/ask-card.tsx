// The decision card at the end of the chat view (model/ask.ts): the agent's question with the terminal's numbers,
// each option a button that sends the agent's own key. After a tap the card waits for the terminal to change
// (store.press): the tapped row says so and the others can't be tapped; the card then shows what the terminal shows.
import { effect, signal, Text, untracked, View } from 'nsh'
import { Input } from 'nsh/base'
import type { Store } from '../store.ts'
import { ownSteps, type Card, type CardRow, type Step } from '../model/ask.ts'
import { t } from '../strings/index.ts'

export function AskCard(props: { store: Store; address: string; agent: string; card: () => Card; back: () => void }) {
  const s = props.store
  const c = () => props.card()
  // What was tapped ('own', 'next', 'skip' or a row's number), until the card shows another screen.
  const sent = signal<string | undefined>(undefined)
  const stuck = signal(false)
  const ownOpen = signal(false)
  const ownText = signal('')
  let shown = ''
  effect(() => {
    const key = JSON.stringify(c())
    untracked(() => { if (key !== shown) { shown = key; sent.set(undefined); stuck.set(false); ownOpen.set(false) } })
  })
  const press = async (what: string, steps: Step[]) => {
    if (sent()) return
    sent.set(what)
    stuck.set(false)
    let r: 'moved' | 'same' = 'same'
    try { r = await s.press(props.address, steps) } catch {}
    if (r === 'same') { sent.set(undefined); stuck.set(true) }
  }
  const top = () => {
    const x = c(), who = t('askAsking', { agent: props.agent })
    if (x.kind === 'review') return `${who} · ${t('askReview')}`
    if (x.kind === 'permission') return t(x.action === 'run' ? 'askRun' : x.action === 'edit' ? 'askEdit' : 'askPermission', { agent: props.agent })
    return [who, x.index && x.count ? t('askQuestionOf', { n: x.index, m: x.count }) : '', x.multi ? t('askPickAny') : ''].filter(Boolean).join(' · ')
  }
  return (
    <View testID="ask-card" class="gap-2 px-3 pt-2.5 pb-3 rounded-[12px] border border-chart-2/40 bg-chart-2/10">
      <Text class="text-[12px] font-medium text-muted-foreground">{top()}</Text>
      <If when={c().kind === 'permission' && c().what}>
        <View class="px-2.5 py-2 rounded-[8px] bg-background border border-border">
          <Text selectable class="text-[12.5px] text-foreground font-mono">{c().what}</Text>
        </View>
      </If>
      <If when={c().review}>{(rv) => (
        <View class="gap-1.5">
          <Each of={rv()} key={(x) => x.q}>{(x) => (
            <View class="gap-[1px]">
              <Text class="text-[12.5px] text-muted-foreground">{x().q}</Text>
              <Text class="text-[13.5px] font-medium text-foreground">{x().a}</Text>
            </View>
          )}</Each>
        </View>
      )}</If>
      <If when={c().question}><Text class="text-[15px] font-semibold text-foreground">{c().question}</Text></If>
      <If when={c().unnumbered} else={
        <View class="gap-1.5">
          <Each of={c().rows} key={(r) => String(r.n)}>{(r) => (
            <Row row={r()} multi={c().multi} sent={sent()} agent={props.agent} onPress={() => void press(String(r().n), r().steps)} />
          )}</Each>
        </View>
      }>
        {/* No numbers on screen (OpenCode): buttons in the terminal's order, the first its default. */}
        <View class="flex-row gap-1.5">
          <Each of={c().rows} key={(r) => String(r.n)}>{(r) => (
            <View role="button" testID={`ask-row-${r().n}`} accessibilityLabel={r().label} disabled={!!sent()} onPress={() => void press(String(r().n), r().steps)}
              class={`flex-1 min-h-10 px-2 py-1.5 items-center justify-center rounded-[10px] ${r().n === 1 ? 'bg-primary active:opacity-80' : 'bg-background border border-border active:bg-accent'} ${sent() && sent() !== String(r().n) ? 'opacity-40' : ''}`}>
              <Text class={`text-[13.5px] font-medium text-center ${r().n === 1 ? 'text-primary-foreground' : 'text-foreground'}`}>{r().label}</Text>
            </View>
          )}</Each>
        </View>
        <If when={sent()}><Text class="text-[12px] text-primary">{t('askSent', { agent: props.agent })}</Text></If>
      </If>
      <If when={c().own?.text ? c().own : undefined}>{(o) => (
        // The own answer once it is typed: a row like the others, its words and tick; its digit ticks it.
        <Row row={{ n: o().n, label: o().text!, ...(o().checked !== undefined ? { checked: o().checked } : {}), steps: [{ keys: [String(o().n)] }] }}
          multi={c().multi} sent={sent()} agent={props.agent} onPress={() => void press(String(o().n), [{ keys: [String(o().n)] }])} />
      )}</If>
      <If when={c().own && !c().own!.text ? c().own : undefined}>{(o) => (
        <If when={ownOpen()} else={
          <View role="button" testID="ask-own" disabled={!!sent()} onPress={() => ownOpen.set(true)}
            class={`flex-row gap-2.5 px-3 py-2.5 rounded-[10px] border border-dashed border-muted-foreground/40 active:bg-accent ${sent() ? 'opacity-40' : ''}`}>
            <Text class="text-[12.5px] font-semibold text-muted-foreground w-[22px] text-center">{String(o().n)}</Text>
            <Text class="text-[14px] text-muted-foreground">{t('askOwn')}</Text>
          </View>
        }>
          <View class="gap-2 px-3 py-2.5 rounded-[10px] border border-primary bg-background">
            <Text class="text-[12px] text-muted-foreground">{`${o().n}. ${t('askOwn')}`}</Text>
            <Input testID="ask-own-input" value={ownText()} onChange={(v) => ownText.set(v)} multiline autoFocus
              class="min-h-[64px] px-2.5 py-2 rounded-[8px] bg-secondary text-[14px] text-foreground" />
            <View class="flex-row justify-end gap-2">
              <View role="button" testID="ask-own-cancel" onPress={() => ownOpen.set(false)} class="h-9 px-3.5 justify-center rounded-[8px] active:bg-accent">
                <Text class="text-[13.5px] text-muted-foreground">{t('cancel')}</Text>
              </View>
              <View role="button" testID="ask-own-send" disabled={!ownText().trim() || !!sent()} onPress={() => void press('own', ownSteps(c(), ownText().trim()))}
                class={`h-9 px-3.5 justify-center rounded-[8px] bg-primary active:opacity-80 ${!ownText().trim() ? 'opacity-40' : ''}`}>
                <Text class="text-[13.5px] font-medium text-primary-foreground">{t('send')}</Text>
              </View>
            </View>
            <If when={sent() === 'own'}><Text class="text-[12px] text-primary">{t('askSent', { agent: props.agent })}</Text></If>
          </View>
        </If>
      )}</If>
      <If when={c().next}>{(n) => (
        <View role="button" testID="ask-next" disabled={!!sent()} onPress={() => void press('next', n())}
          class={`h-10 items-center justify-center rounded-[10px] bg-primary active:opacity-80 ${sent() && sent() !== 'next' ? 'opacity-40' : ''}`}>
          <Text class="text-[14px] font-semibold text-primary-foreground">{sent() === 'next' ? t('askSent', { agent: props.agent }) : t('askNext')}</Text>
        </View>
      )}</If>
      <If when={c().rows.some((r) => r.afterNo)}><Text class="text-[12px] text-muted-foreground">{t('askAfterNo')}</Text></If>
      <If when={stuck()}><Text testID="ask-stuck" class="text-[12px] text-destructive">{t('askStuck')}</Text></If>
      <View class="flex-row items-start gap-3 pt-0.5">
        <If when={c().skip} else={<View class="flex-1" />}>{(k) => (
          <View role="button" testID="ask-skip" disabled={!!sent()} onPress={() => void press('skip', k().steps)} class="flex-1 active:opacity-70">
            <Text class="text-[12.5px] text-muted-foreground">{`${k().n}. ${t('askSkip')}`}</Text>
          </View>
        )}</If>
        <View role="button" testID="ask-show-terminal" onPress={props.back} class="ml-auto active:opacity-70">
          <Text class="text-[12.5px] text-muted-foreground">{t('showTerminal')}</Text>
        </View>
      </View>
    </View>
  )
}

function Row(props: { row: CardRow; multi: boolean; sent: string | undefined; agent: string; onPress: () => void }) {
  const r = () => props.row
  const mine = () => props.sent === String(r().n)
  const dim = () => !!props.sent && !mine()
  return (
    <View role="button" testID={`ask-row-${r().n}`} accessibilityLabel={`${r().n}. ${r().label}`} disabled={!!props.sent} onPress={props.onPress}
      class={`flex-row gap-2.5 px-3 py-2.5 rounded-[10px] border ${mine() ? 'border-primary bg-primary/10' : 'border-border bg-background'} ${dim() ? 'opacity-40' : ''} active:bg-accent`}>
      <If when={props.multi} else={
        <View testID={`ask-num-${r().n}`} class={`w-[22px] h-[22px] mt-[1px] rounded-[6px] items-center justify-center ${mine() ? 'bg-primary' : 'bg-secondary border border-border'}`}>
          <Text class={`text-[12px] font-semibold ${mine() ? 'text-primary-foreground' : 'text-muted-foreground'}`}>{String(r().n)}</Text>
        </View>
      }>
        <View testID={`ask-box-${r().n}-${r().checked ? 'on' : 'off'}`} class={`w-[20px] h-[20px] mt-[2px] rounded-[5px] items-center justify-center ${r().checked ? 'bg-primary' : 'border-[1.5px] border-muted-foreground/60'}`}>
          <If when={r().checked}><Text class="text-[12px] font-bold text-primary-foreground">✓</Text></If>
        </View>
      </If>
      <View class="flex-1 min-w-0 gap-[3px]">
        <View class="flex-row items-center gap-1.5 flex-wrap">
          <If when={props.multi}><Text class="text-[12.5px] font-semibold text-muted-foreground">{`${r().n}.`}</Text></If>
          <Text class="text-[14.5px] font-medium text-foreground shrink">{r().label}</Text>
          <If when={r().recommended}>
            <View class="px-1.5 py-[1px] rounded-[5px] bg-primary/15"><Text class="text-[11px] font-semibold text-primary">{t('askRecommended')}</Text></View>
          </If>
        </View>
        <If when={r().detail}><Text class="text-[12.5px] text-muted-foreground">{r().detail}</Text></If>
        <If when={mine()}><Text class="text-[12px] text-primary">{t('askSent', { agent: props.agent })}</Text></If>
      </View>
    </View>
  )
}
