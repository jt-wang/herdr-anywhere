// Peek: a terminal without a pane (an agent's sub-agent) watched from a sheet without taking its size;
// keep it as a tab of its own, or close it (herdr-app defaults/mods/agents peek).
import { Text, View, type Signal } from 'nsh'
import { Sheet } from 'nsh/base'
import type { Store } from '../store.ts'
import { Button, PANEL, PANEL_BACKDROP, ProgramIcon, StatusDot, useRev } from './kit.tsx'
import { t } from '../strings/index.ts'
import { TerminalView } from './terminal.tsx'

export function PeekSheet(props: { store: Store; address: Signal<string | null>; fontSize: () => number }) {
  const s = props.store
  const rev = useRev(s)
  const term = () => { rev(); const a = props.address(); return a ? s.term(a) : undefined }
  const close = () => props.address.set(null)
  return (
    <Sheet open={props.address() !== null} side="bottom" onOpenChange={(o) => { if (!o) close() }}
      popupClass={`${PANEL} h-[70%] pb-[env(safe-area-inset-bottom)] mb-3`} backdropClass={PANEL_BACKDROP}>
      <If when={props.address()}>{(addr) => (
        <View class="flex-1">
          <View class="flex-row items-center gap-2 px-4 h-11">
            <ProgramIcon icon={(rev(), s.program(addr()).icon)} />
            <Text class="text-[14px] font-medium text-foreground flex-1" numberOfLines={1}>{(rev(), s.terminalName(term()))}</Text>
            <StatusDot status={(rev(), term()?.status?.state)} />
          </View>
          <Each of={[addr()]}>{(a) => <TerminalView store={s} address={a()} fontSize={props.fontSize()} observe keys={false} />}</Each>
          <View class="flex-row gap-2 px-3 pt-2">
            <View class="flex-1"><Button label={t('keepAsTab')} testID="peek-keep" onPress={() => { void s.keepTerminal(addr()).then(close) }} /></View>
            <View class="flex-1"><Button label={t('close')} kind="danger" testID="peek-close" onPress={() => { s.kill([addr()]); close() }} /></View>
          </View>
        </View>
      )}</If>
    </Sheet>
  )
}
