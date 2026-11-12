// A picture whose path was tapped in a terminal (ui/terminal-page.ts says it was a path ending in an image type):
// the path on top, to select or copy; the picture under it, fitted to the width, held to copy it. The computer says
// which file it is and whether it may be shown (link.ts imageApi). Only the open sheet holds the picture. Pinched, it
// zooms in its box, and drags within it; a double tap zooms in on that point, another back out (picture-zoom.ts).
import { Clipboard, effect, Image, Scroll, signal, Text, untracked, View, windowSize, type YNode } from 'nsh'
import { Sheet } from 'nsh/base'
import type { Store } from '../store.ts'
import type { Picture } from '../link.ts'
import { copyPicture } from '../page.ts'
import { IconButton, useSignal } from './kit.tsx'
import { maxZoom, pictureZoom } from './picture-zoom.ts'
import { t } from '../strings/index.ts'

export function PictureSheet(props: { store: Store }) {
  const s = props.store
  const asked = useSignal(s.picture)
  const shown = signal<Picture | null>(null)
  const why = signal('')
  const natural = signal({ width: 0, height: 0 })
  const width = signal(0)
  let zoomer: YNode | null = null
  const zoom = pictureZoom((to) => zoomer?.zoomTo(to))
  effect(() => {
    const a = asked()
    untracked(() => {
      shown.set(null)
      why.set('')
      natural.set({ width: 0, height: 0 })
      zoom.reset()
      if (!a) return
      if (!s.images) return why.set(t('pictureCantRead'))
      s.images.read(a.path, a.terminal).then(
        (p) => { if (s.picture.get() === a) shown.set(p) },
        (e) => { if (s.picture.get() === a) why.set(e instanceof Error ? e.message : t('pictureCantRead')) },
      )
    })
  })
  const close = () => s.picture.set(null)
  const path = () => shown()?.path ?? asked()?.path ?? ''
  const copyPath = () => void Clipboard.writeText(path()).then(() => s.toast({ title: t('copied'), level: 'info' }), () => {})
  const copyImage = () => {
    const p = shown()
    if (!p) return
    void copyPicture(p.url).then((ok) => s.toast(ok ? { title: t('pictureCopied'), level: 'info' } : { title: t('pictureCantCopy'), level: 'error' }))
  }
  // Fitted to the sheet's width and to about half the screen's height, never drawn larger than it is: the terminal
  // stays in sight above it.
  const box = () => {
    const n = natural(), w = width()
    if (!n.width || !w) return { width: w || 1, height: 160 }
    const k = Math.min(1, (w - 2) / n.width, (windowSize().height * 0.5) / n.height) // inside its 1 px border
    return { width: n.width * k, height: n.height * k }
  }
  const tap = (e?: { position: { x: number; y: number } }) =>
    zoom.tap({ at: Date.now(), x: e?.position.x ?? 0, y: e?.position.y ?? 0 }, { natural: natural(), box: box() })
  return (
    <Sheet open={asked() !== null} side="bottom" onOpenChange={(o) => { if (!o) close() }}
      popupClass="bg-popover rounded-t-[14px] border-t border-border max-h-[88%] pb-[env(safe-area-inset-bottom)]">
      <If when={asked()}>
        <View testID="picture-sheet" class="shrink">
          <View class="self-center w-9 h-1 mt-2 rounded-full bg-border" />
          <View class="flex-row items-start gap-1 pl-4 pr-1.5 pt-2">
            <Text selectable testID="picture-path" class="flex-1 pt-2.5 text-[12.5px] font-mono text-foreground">{path()}</Text>
            <View role="button" testID="picture-copy-path" onPress={copyPath}
              class="h-10 px-3 justify-center rounded-(--radius) active:bg-accent">
              <Text class="text-[14px] font-medium text-primary">{t('copyPath')}</Text>
            </View>
            <IconButton name="close" label={t('close')} testID="picture-close" onPress={close} />
          </View>
          <Scroll class="shrink" contentClass="px-4 pt-2 pb-4 gap-2">
            <View onLayout={(b) => width.set(b.size.width)}>
              <If when={shown()} else={
                <Text testID="picture-state" class="py-10 text-[13.5px] text-muted-foreground text-center">{why() || t('pictureLoading')}</Text>
              }>{(p) => (
                <View class="gap-2 items-center">
                  <View class="rounded-[8px] overflow-hidden border border-border bg-background">
                    <Scroll ref={(r: YNode | null) => { zoomer = r }} testID="picture-zoom" style={box()} minimumZoomScale={1}
                      maximumZoomScale={maxZoom(natural(), box())} bounces={false} centerContent scrollbar={false}
                      onScroll={(e) => zoom.scrolled(e.zoomScale)}>
                      <View role="button" testID="picture" accessibilityLabel={t('pictureHold')}
                        onLongPress={copyImage} onPress={tap}>
                        <Image testID="picture-image" src={p().url} resizeMode="contain" onLoad={(e) => natural.set({ width: e.width, height: e.height })} style={box()} />
                      </View>
                    </Scroll>
                  </View>
                  <Text class="text-[12px] text-muted-foreground">{t('pictureHold')}</Text>
                </View>
              )}</If>
            </View>
          </Scroll>
        </View>
      </If>
    </Sheet>
  )
}
