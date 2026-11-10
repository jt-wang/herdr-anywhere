// The notice on home, and the rows in Settings: a new version, and installing it.
import { Clipboard, cleanup, openUrl, signal, Text, View } from 'nsh'
import { AlertDialog } from 'nsh/base'
import { t } from '../strings/index.ts'
import type { Notice, Updates } from '../updates.ts'
import type { PrefStore } from '../prefs.ts'
import { useSignal } from './kit.tsx'

// Rebuild by hand for a Mac the phone can't update: the file INSTALL.md downloads for its processor.
export const INSTALL_COMMAND = 'curl -fsSL -o ~/.local/bin/herdr-anywhere https://github.com/jt-wang/herdr-anywhere/releases/latest/download/herdr-anywhere-macos-$(uname -m | sed \'s/x86_64/amd64/\') && chmod +x ~/.local/bin/herdr-anywhere'
const IPHONE_GUIDE = 'https://github.com/jt-wang/herdr-anywhere/blob/main/app/ios/README.md'

/** Follows what the updates say. */
export function useUpdates(u: Updates) {
  const rev = signal(0)
  const bump = () => rev.update((n) => n + 1)
  const offs = [u.info.sub(bump), u.busy.sub(bump), u.progress.sub(bump)]
  cleanup(() => { for (const off of offs) off() })
  return () => { rev(); return { notice: u.notice(), busy: u.busy.get(), progress: u.progress.get(), info: u.info.get() } }
}

/** Asks what must be asked first, then does it. */
export async function runUpdate(u: Updates, notice: Notice) {
  if (notice.kind === 'computer') return void u.updateComputer()
  if (notice.kind === 'computerByHand') {
    const r = await AlertDialog.show({ title: t('updateByHandTitle'), description: `${t('updateByHandDetail')}\n\n${INSTALL_COMMAND}`,
      actions: [{ id: 'cancel', label: t('cancel'), role: 'cancel' }, { id: 'copy', label: t('copyCommand') }] })
    if (r === 'copy') void Clipboard.writeText(INSTALL_COMMAND)
    return
  }
  if (notice.kind === 'rebuild') {
    const r = await AlertDialog.show({ title: t('rebuildTitle', { version: notice.version }), description: t('rebuildDetail'),
      actions: [{ id: 'cancel', label: t('cancel'), role: 'cancel' }, { id: 'open', label: t('openGuide') }] })
    if (r === 'open') void openUrl(IPHONE_GUIDE)
    return
  }
  // The app is large, and the phone may be on mobile data.
  const size = Math.round((u.info.get()?.app?.bytes ?? 0) / 1_000_000)
  const r = await AlertDialog.show({ title: t('downloadAppTitle', { size }), description: t('downloadAppDetail'),
    actions: [{ id: 'cancel', label: t('cancel'), role: 'cancel' }, { id: 'go', label: t('download') }] })
  if (r === 'go') void u.updateApp()
}

export const busyWord = (busy: 'computer' | 'app' | null, progress: number) =>
  busy === 'app' ? t('downloadingApp', { percent: Math.round(progress * 100) }) : t('updating')

/** Asked once, from the second time the app opens: may the computer ask GitHub about new versions each day? */
export function ChecksQuestion(props: { updates: Updates; prefs: PrefStore }) {
  const state = useUpdates(props.updates)
  const prefs = useSignal(props.prefs.prefs)
  const shown = () => { state(); return props.updates.asksChecks(prefs().opened) }
  return (
    <If when={shown()}>
      <View testID="checks-question" class="mx-3 mt-1 mb-1 rounded-(--radius) bg-muted pl-3 pr-1.5 py-2 gap-1">
        <Text class="text-[13px] leading-[18px] font-medium text-foreground pr-1.5">{t('checksQuestion')}</Text>
        <Text class="text-[12px] leading-[17px] text-muted-foreground pr-1.5">{t('checksQuestionDetail')}</Text>
        <View class="flex-row justify-end gap-2 mt-1">
          <View role="button" testID="checks-no" onPress={() => void props.updates.setChecks(false)} class="px-2 py-2 rounded-[6px] active:bg-accent">
            <Text class="text-[13px] text-muted-foreground">{t('checksNo')}</Text>
          </View>
          <View role="button" testID="checks-yes" onPress={() => void props.updates.setChecks(true)} class="px-3 py-2 rounded-[6px] bg-background active:bg-accent">
            <Text class="text-[13px] font-medium text-foreground">{t('checksYes')}</Text>
          </View>
        </View>
      </View>
    </If>
  )
}

export function UpdateBanner(props: { updates: Updates; prefs: PrefStore }) {
  const state = useUpdates(props.updates)
  const prefs = useSignal(props.prefs.prefs)
  const notice = () => state().notice
  const hidden = () => !notice() || prefs().dismissedUpdate === notice()!.version
  const busy = () => state().busy
  return (
    <If when={busy() || !hidden()}>
      <View testID="update-banner" class="mx-3 mt-1 mb-1 flex-row items-center gap-2 rounded-(--radius) bg-muted pl-3 pr-1.5 py-1.5">
        <Text class="flex-1 text-[13px] leading-[18px] text-foreground" numberOfLines={2}>
          {busy() ? busyWord(busy(), state().progress) : t('updateAvailable', { version: notice()?.version ?? '' })}
        </Text>
        <If when={!busy()}>
          <View role="button" testID="update-later" onPress={() => props.prefs.set({ dismissedUpdate: notice()!.version })} class="px-2 py-2 rounded-[6px] active:bg-accent">
            <Text class="text-[13px] text-muted-foreground">{t('dismissUpdate')}</Text>
          </View>
          <View role="button" testID="update-now" onPress={() => void runUpdate(props.updates, notice()!)} class="px-3 py-2 rounded-[6px] bg-background active:bg-accent">
            <Text class="text-[13px] font-medium text-foreground">{t('updateNow')}</Text>
          </View>
        </If>
      </View>
    </If>
  )
}
