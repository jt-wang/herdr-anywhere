// Sheets over any screen: the "+" menu in two steps: the folder on the computer first
// (folder-picker.tsx), then a terminal or an agent installed there, with its first task; and renaming a space or a tab.
import { computed, effect, Keyboard, keyboardFrame, signal, Text, View, type Signal } from 'nsh'
import { Input, Sheet } from 'nsh/base'
import type { Store } from '../store.ts'
import { explain } from '../store.ts'
import { startable, type Program } from '../model/programs.ts'
import { tilde, type FolderApi } from '../model/folders.ts'
import { Button, Glyph, ProgramIcon, useRev } from './kit.tsx'
import { t } from '../strings/index.ts'
import { FolderPickerView } from './folder-picker.tsx'

/** A sheet sits on the keyboard while it is up: NSH's Sheet stays at the screen's bottom, behind it (seen on Android). */
const above = () => ({ marginBottom: keyboardFrame().height + 16 })

/** Where "+" was pressed: the home screen starts a space, a space starts a tab in it. */
export type NewTarget = { kind: 'space' } | { kind: 'tab'; space: string; agent?: boolean }

export function NewSheet(props: { store: Store; target: Signal<NewTarget | null>; folders?: FolderApi; onStarted: (r: { space: string; pane: string }) => void }) {
  const s = props.store
  const rev = useRev(s)
  const picked = signal<Program | null>(null)
  const task = signal('')
  /** The folder picked in the first step; it starts at home for a new space and at the space's folder for a new tab. */
  const folder = signal('')
  const picking = signal(true)
  const busy = signal(false)
  const error = signal('')
  const open = () => props.target() !== null
  const agents = computed(() => {
    rev()
    const have = s.installed.get('')
    return have ? startable().filter((p) => p.agent && have.includes(p.run![0])) : []
  })
  const close = () => { props.target.set(null); picked.set(null); task.set(''); folder.set(''); picking.set(true); error.set(''); busy.set(false) }
  const where = () => {
    rev()
    const t = props.target()
    return folder() || (t?.kind === 'tab' ? s.folderFor(t.space) : s.home)
  }
  const title = () => { const n = props.target(); return n?.kind === 'tab' ? (n.agent ? t('addAgent') : t('newTab')) : t('newSpace') }
  // Adding an agent starts at the program, in the space's folder.
  effect(() => { const n = props.target(); if (n?.kind === 'tab' && n.agent) picking.set(false) })
  // Another machine's folders are not this computer's: the picker lists only the computer the companion runs on, so a
  // tab there starts at the second step, in its space's folder.
  const local = () => { rev(); const t = props.target(); return !!props.folders && !(t?.kind === 'tab' && s.model.space(t.space)?.machine) }
  const start = async (p: Program | null) => {
    const t = props.target()
    if (!t || busy()) return
    busy.set(true)
    error.set('')
    try {
      const argv = p?.run
      const f = folder() || undefined
      const r = t.kind === 'space'
        ? await s.newSpace({ argv, task: task(), folder: f })
        : await s.newTab({ space: t.space, argv, task: task(), folder: f })
      Keyboard.dismiss()
      close()
      props.onStarted(r)
    } catch (e) {
      error.set(explain(e))
      busy.set(false)
    }
  }
  return (
    <Sheet open={open()} side="bottom" onOpenChange={(o) => { if (!o) close() }} popupClass="bg-popover rounded-t-[14px] px-4 pt-3 pb-[env(safe-area-inset-bottom)]" popupStyle={above()}>
      <View class="gap-1">
        <If when={open() && picking() && local()}>
          <FolderPickerView title={title()} api={props.folders!} home={s.home} start={where()} recent={s.recentFolders()}
            onPick={(f) => { Keyboard.dismiss(); folder.set(f); picking.set(false) }} />
        </If>
        <If when={!(picking() && local())}>
        <Text class="text-[15px] font-semibold text-foreground">{title()}</Text>
        {/* The folder chosen: a line that goes back to the first step */}
        <View role="button" testID="folder-chosen" disabled={!local()} accessibilityLabel={t('folderChosen', { path: tilde(where(), s.home) })} onPress={() => picking.set(true)}
          class="flex-row items-center gap-1.5 py-1.5 mb-1 self-start rounded-[6px] active:opacity-60">
          <If when={local()}><Glyph name="chevron-left" size={13} /></If>
          <Glyph name="folder" size={13} />
          <Text class="text-[13px] font-mono text-muted-foreground" numberOfLines={1}>{tilde(where(), s.home)}</Text>
        </View>
        <If when={picked()} else={
          <View>
            <View role="button" testID="new-terminal" onPress={() => void start(null)} class="flex-row items-center gap-3 h-11 px-2 rounded-(--radius) active:bg-sidebar-accent">
              <ProgramIcon icon="terminal" size={18} />
              <Text class="text-[14px] text-foreground">{t('terminal')}</Text>
            </View>
            <Each of={agents()} key={(p) => p.key}>{(p) => (
              <View role="button" testID={`new-${p().key}`} onPress={() => picked.set(p())} class="flex-row items-center gap-3 h-11 px-2 rounded-(--radius) active:bg-sidebar-accent">
                <ProgramIcon icon={p().icon} size={18} />
                <Text class="text-[14px] text-foreground">{p().name}</Text>
              </View>
            )}</Each>
            <If when={!agents().length}><Text class="text-[12px] text-sidebar-foreground px-2 py-1">{t('agentsShowUp')}</Text></If>
          </View>
        }>{(p) => (
          <View class="gap-2">
            <View class="flex-row items-center gap-2">
              <ProgramIcon icon={p().icon} size={18} />
              <Text class="text-[14px] text-foreground">{p().name}</Text>
            </View>
            <Input testID="task" placeholder={t('taskPlaceholder')} value={task()} onChange={(t) => task.set(t)} multiline autoFocus
              class="min-h-[72px] rounded-(--radius) border border-input bg-background px-3 py-2 text-[14px] text-foreground placeholder:text-sidebar-foreground" />
            <Button label={busy() ? t('starting') : t('startProgram', { name: p().name })} testID="start" disabled={busy()} onPress={() => void start(p())} />
            <Button label={t('back')} kind="plain" onPress={() => picked.set(null)} />
          </View>
        )}</If>
        <If when={error()}><Text testID="new-error" class="text-[13px] text-destructive mt-1">{error()}</Text></If>
        </If>
      </View>
    </Sheet>
  )
}

export type RenameTarget = { kind: 'space' | 'tab'; id: string; current: string }

export function RenameSheet(props: { store: Store; target: Signal<RenameTarget | null> }) {
  const text = signal('')
  const save = () => {
    const t = props.target()
    if (!t) return
    if (t.kind === 'space') props.store.renameSpace(t.id, text().trim())
    else props.store.renameTab(t.id, text().trim())
    Keyboard.dismiss()
    props.target.set(null)
  }
  return (
    <Sheet open={props.target() !== null} side="bottom" onOpenChange={(o) => { if (!o) props.target.set(null); else text.set(props.target()?.current ?? '') }}
      popupClass="bg-popover rounded-t-[14px] px-4 pt-3 pb-[env(safe-area-inset-bottom)]" popupStyle={above()}>
      <View class="gap-2">
        <Text class="text-[15px] font-semibold text-foreground">{props.target()?.kind === 'tab' ? t('renameTab') : t('renameSpace')}</Text>
        <Input testID="rename" value={text()} defaultValue={props.target()?.current ?? ''} onChange={(t) => text.set(t)} autoFocus submitBehavior="submit" onSubmit={save}
          placeholder={t('renamePlaceholder')}
          class="rounded-(--radius) border border-input bg-background px-3 py-2.5 text-[15px] text-foreground placeholder:text-sidebar-foreground" />
        <Button label={t('save')} testID="rename-save" onPress={save} />
      </View>
    </Sheet>
  )
}
