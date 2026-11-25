// A picture whose path was tapped in a terminal (ui/terminal-page.ts says it was a path ending in an image type):
// the path on top, to select or copy; the picture under it, fitted to the width, held to copy it. The computer says
// which file it is and whether it may be shown (link.ts imageApi). Only the open sheet holds the picture. Pinched, it
// zooms in its box, and drags within it; a double tap zooms in on that point, another back out (picture-zoom.ts).
// A picture opened from a chat (an image of the record, or an image file a reply names) takes the whole screen, fitted
// to it, and closes on a swipe down while it is not zoomed in; an image of the record is shown as the chat fetched it,
// with no path when the record gives none.
import { cleanup, Clipboard, effect, Image, Scroll, signal, Text, untracked, View, windowSize, type YNode } from 'nsh'
import type { Store } from '../store.ts'
import type { Picture } from '../link.ts'
import { copyPicture } from '../page.ts'
import { IconButton, PANEL, PANEL_BACKDROP, PANEL_STYLE, Sheet, useSignal } from './kit.tsx'
import { maxZoom, pictureZoom } from './picture-zoom.ts'
import { t } from '../strings/index.ts'
import { pictureEvents } from './picture-events.ts'

export function PictureSheet(props: { store: Store }) {
  const s = props.store
  const asked = useSignal(s.picture)
  const shown = signal<(Picture & { generation: number }) | null>(null)
  let imageGeneration = 0
  const why = signal('')
  const retry = signal(0)
  const decoded = signal(false)
  let request: AbortController | undefined
  let decodeTimer: ReturnType<typeof setTimeout> | undefined
  const stop = () => { request?.abort(); clearTimeout(decodeTimer) }
  cleanup(stop)
  const natural = signal({ width: 0, height: 0 })
  // Where the picture may go: the sheet's width; on the whole screen, the height left for it too.
  const area = signal({ width: 0, height: 0 })
  const zoomedIn = signal(false)
  let zoomer: YNode | null = null
  const zoom = pictureZoom((to) => zoomer?.zoomTo(to))
  const full = () => { const a = asked(); return !!a && ('url' in a || !!a.full) }
  effect(() => {
    const a = asked(); retry()
    untracked(() => {
      stop()
      decoded.set(false)
      shown.set(null)
      why.set('')
      natural.set({ width: 0, height: 0 })
      zoom.reset()
      zoomedIn.set(false)
      if (!a) return
      const controller = new AbortController()
      request = controller
      const current = () => !controller.signal.aborted && s.picture.get() === a && request === controller
      const show = (p: Picture) => {
        if (!current()) return
        shown.set({ ...p, generation: ++imageGeneration })
        decodeTimer = setTimeout(() => {
          if (current() && !decoded()) { why.set(t('pictureTimeout')); shown.set(null) }
        }, 15_000)
      }
      if ('url' in a) return show({ path: a.path ?? '', url: a.url })
      if (!s.images) return why.set(t('pictureCantRead'))
      s.images.read(a.path, a.terminal, controller.signal).then(
        show,
        (e) => { if (current()) why.set(e instanceof Error ? e.message : t('pictureCantRead')) },
      )
    })
  })
  const close = () => s.picture.set(null)
  const path = () => { const a = asked(); return shown()?.path || (a && a.path) || '' }
  const copyPath = () => void Clipboard.writeText(path()).then(() => s.toast({ title: t('copied'), level: 'info' }), () => {})
  const copyImage = () => {
    const p = shown()
    if (!p) return
    void copyPicture(p.url).then((ok) => s.toast(ok ? { title: t('pictureCopied'), level: 'info' } : { title: t('pictureCantCopy'), level: 'error' }))
  }
  // From a terminal: fitted to the sheet's width and to about half the screen's height, never drawn larger than it
  // is, so the terminal stays in sight above it. From a chat: fitted to the space under the path, larger or smaller.
  const box = () => {
    const n = natural(), a = area()
    if (!n.width || !a.width) return { width: a.width || 1, height: 160 }
    const k = full() && a.height
      ? Math.min((a.width - 2) / n.width, (a.height - 2) / n.height)
      : Math.min(1, (a.width - 2) / n.width, (windowSize().height * 0.5) / n.height) // inside its 1 px border
    return { width: n.width * k, height: n.height * k }
  }
  const tap = (e?: { position: { x: number; y: number } }) =>
    zoom.tap({ at: Date.now(), x: e?.position.x ?? 0, y: e?.position.y ?? 0 }, { natural: natural(), box: box() })
  const state = () => <View class="items-center gap-2 py-10">
    <Text testID="picture-state" class="text-[13.5px] text-muted-foreground text-center">{why() || t('pictureLoading')}</Text>
    <If when={why()}><View role="button" testID="picture-retry" accessibilityLabel={t('tryAgain')} class="px-4 py-3 rounded-(--radius) bg-secondary" onPress={() => retry.set(retry() + 1)}>
      <Text class="text-primary">{t('tryAgain')}</Text>
    </View></If>
  </View>
  const picture = (p: () => Picture) => {
    const image = p(), owner = request
    const events = pictureEvents(image, owner, () => ({ image: shown(), owner: request }),
      (e) => { clearTimeout(decodeTimer); natural.set({ width: e.width, height: e.height }); decoded.set(true) },
      () => { clearTimeout(decodeTimer); why.set(t('pictureCantRead')); shown.set(null) })
    return (
    <View class="rounded-[8px] overflow-hidden border border-border bg-background">
      <Scroll ref={(r: YNode | null) => { zoomer = r }} testID="picture-zoom" style={box()} minimumZoomScale={1}
        maximumZoomScale={maxZoom(natural(), box())} bounces={false} centerContent scrollbar={false}
        onScroll={(e) => { zoom.scrolled(e.zoomScale, Date.now()); zoomedIn.set(zoom.zoomed()) }}>
        <View role="button" testID="picture" accessibilityLabel={t('pictureHold')}
          onLongPress={copyImage} onPress={tap}>
          <Image testID="picture-image" src={image.url} resizeMode="contain" onLoad={events.onLoad} onError={events.onError} style={box()} />
        </View>
      </Scroll>
      <If when={!decoded()}>{state()}</If>
    </View>
  )
  }
  const hint = () => <Text class="text-[12px] text-muted-foreground">{t('pictureHold')}</Text>
  return (
    <Sheet open={asked() !== null} side="bottom" onOpenChange={(o) => { if (!o) close() }} closeOnSwipe={full() && !zoomedIn()}
      popupClass={full() ? 'bg-popover h-full pb-[env(safe-area-inset-bottom)]'
        : `${PANEL} max-h-[88%] pb-[env(safe-area-inset-bottom)]`} popupStyle={full() ? {} : PANEL_STYLE} backdropClass={PANEL_BACKDROP}>
      <If when={asked()}>
        <View testID="picture-sheet" class={full() ? 'flex-1 pt-[env(safe-area-inset-top)]' : 'shrink'}>
          <View class="self-center w-9 h-1 mt-2 rounded-full bg-border" />
          <View class="flex-row items-start gap-1 pl-4 pr-1.5 pt-2">
            <View class="flex-1">
              <If when={path()}>
                <Text selectable testID="picture-path" class="pt-2.5 text-[12.5px] font-mono text-foreground">{path()}</Text>
              </If>
            </View>
            <If when={path()}>
              <View role="button" testID="picture-copy-path" onPress={copyPath}
                class="h-10 px-3 justify-center rounded-(--radius) active:bg-accent">
                <Text class="text-[14px] font-medium text-primary">{t('copyPath')}</Text>
              </View>
            </If>
            <IconButton name="close" label={t('close')} testID="picture-close" onPress={close} />
          </View>
          <If when={full()} else={
            <Scroll class="shrink" contentClass="px-4 pt-2 pb-4 gap-2">
              <View onLayout={(b) => area.set({ width: b.size.width, height: 0 })}>
                <If when={shown()} else={state()}>{(p) => (
                  <View class="gap-2 items-center">
                    <Each of={[p()]} key={(image) => image.generation}>{(image) => picture(image)}</Each>
                    {hint()}
                  </View>
                )}</If>
              </View>
            </Scroll>
          }>
            <View class="flex-1 px-4 pt-2 pb-4 gap-2">
              <View class="flex-1 items-center justify-center" onLayout={(b) => area.set({ width: b.size.width, height: b.size.height })}>
                <If when={shown()} else={state()}>{(p) => <Each of={[p()]} key={(image) => image.generation}>{(image) => picture(image)}</Each>}</If>
              </View>
              <If when={shown()}><View class="items-center">{hint()}</View></If>
            </View>
          </If>
        </View>
      </If>
    </Sheet>
  )
}
