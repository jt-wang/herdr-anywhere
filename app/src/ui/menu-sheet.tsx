// A menu as the app's own sheets draw it (sheets.tsx): a long press's or a "…" button's choices, from the bottom,
// in place of the system's action sheet, which iOS 27 draws as a box of large pills in the middle of the screen.
import { signal, Text, View } from 'nsh'
import { AlertDialog, Sheet } from 'nsh/base'
import { Button, Glyph, PANEL, PANEL_BACKDROP } from './kit.tsx'
import { t } from '../strings/index.ts'

export type MenuItem = { id: string; label: string; icon?: Parameters<typeof Glyph>[0]['name']; role?: 'destructive' }
export type MenuAsk = { title: string; message?: string; items: MenuItem[] }

const asked = signal<(MenuAsk & { answer: (id: string) => void }) | null>(null)

/** Shows the menu and resolves with the picked item's id, or 'cancel' when it was closed without one. */
export function pickFrom(ask: MenuAsk): Promise<string> {
  asked()?.answer('cancel')
  return new Promise((resolve) => asked.set({ ...ask, answer: resolve }))
}

/** A dangerous choice asks again in the system's alert: closing `name` and what ends with it. */
export async function confirmClose(name: string, detail: 'closeDetail' | 'closePaneDetail'): Promise<boolean> {
  return await AlertDialog.show({ title: t('closeTitle', { name }), description: t(detail), actions: [
    { id: 'cancel', label: t('cancel'), role: 'cancel' }, { id: 'close', label: t('close'), role: 'destructive' }] }) === 'close'
}

export function MenuSheet() {
  const close = (id: string) => { const a = asked(); asked.set(null); a?.answer(id) }
  return (
    <Sheet open={asked() !== null} side="bottom" onOpenChange={(o) => { if (!o) close('cancel') }}
      popupClass={`${PANEL} px-4 pt-3 pb-[env(safe-area-inset-bottom)]`} backdropClass={PANEL_BACKDROP}>
      <View testID="menu" class="gap-1">
        <View class="pb-1">
          <View testID="menu-title"><Text class="text-[15px] font-semibold text-foreground" numberOfLines={2}>{asked()?.title ?? ''}</Text></View>
          <If when={asked()?.message}><View testID="menu-message"><Text class="text-[13px] text-muted-foreground" numberOfLines={1}>{asked()?.message ?? ''}</Text></View></If>
        </View>
        <View>
          <Each of={asked()?.items ?? []} key={(i) => i.id}>{(i) => (
            <View role="button" testID={`menu-${i().id}`} onPress={() => close(i().id)} class="flex-row items-center gap-3 h-11 px-2 rounded-(--radius) active:bg-sidebar-accent">
              <If when={i().icon}><Glyph name={i().icon!} size={18} class={i().role === 'destructive' ? 'tint-destructive' : 'tint-muted-foreground'} /></If>
              <Text class={`text-[14px] ${i().role === 'destructive' ? 'text-destructive' : 'text-foreground'}`}>{i().label}</Text>
            </View>
          )}</Each>
        </View>
        <Button label={t('cancel')} testID="menu-cancel" kind="plain" onPress={() => close('cancel')} />
      </View>
    </Sheet>
  )
}
