// The key row in each look it can take, at this phone's width and text size, for e2e/key-row.test.tsx: in terminal
// mode (esc, the box switch and the keyboard button), with Shift and Ctrl held, and
// under the input box. Each look says whether every label sat on one line, whole.
import { mount, signal, Text, View } from 'nsh'
import { KeyRow, rowKeys, type KeyRowLook } from '../src/ui/pane-bar.tsx'
import { Signal } from '../src/store.ts'
import type { TerminalSession } from '../src/ui/terminal-session.ts'

export const LOOKS: { name: string; look: KeyRowLook }[] = [
  { name: 'word-label', look: { labels: 'word', size: 'label' } },
  { name: 'symbol-equal', look: { labels: 'symbol', size: 'equal' } },
  { name: 'word-equal', look: { labels: 'word', size: 'equal' } },
  { name: 'symbol-label', look: { labels: 'symbol', size: 'label' } },
]

const session = (held: boolean) => ({ ctrl: new Signal(held), shift: new Signal(held), toggleCtrl() {}, toggleShift() {}, key() {}, focus() {} }) as unknown as TerminalSession
const plain = session(false), held = session(true)
const ROWS = [
  { name: 'terminal', keys: rowKeys('compose', 'terminal'), session: plain, toBox: () => {} },
  { name: 'terminal, ⇧ ⌃ held', keys: rowKeys('compose', 'terminal'), session: held, toBox: () => {} },
  { name: 'input box', keys: rowKeys('compose', 'composer'), session: plain, toBox: undefined },
]

function Look(props: { name: string; look: KeyRowLook }) {
  const bad = signal<string[][]>(ROWS.map(() => []))
  const laid = signal(ROWS.map(() => false))
  const verdict = () => !laid().every(Boolean) ? 'waiting' : bad().every((b) => b.length === 0) ? 'fits' : `wraps ${[...new Set(bad().flat())].join(',')}`
  return (
    <View class="gap-1 pb-3">
      <Text testID={`fit-${props.name}`} class="text-[11px] text-muted-foreground px-2">{`${props.name}: ${verdict()}`}</Text>
      <Each of={ROWS}>{(r, i) => (
        <KeyRow keys={r().keys} session={r().session} keepTerminal={() => false} toBox={r().toBox} look={props.look} keyboardUp={() => true}
          onFit={(_, cramped) => { const b = [...bad()]; b[i()] = cramped; bad.set(b); const l = [...laid()]; l[i()] = true; laid.set(l) }} />
      )}</Each>
    </View>
  )
}

mount(() => (
  <View class="flex-1 bg-background pt-14">
    <Each of={LOOKS}>{(l) => <Look name={l().name} look={l().look} />}</Each>
  </View>
))
