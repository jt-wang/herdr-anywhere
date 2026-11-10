// The first launch: pair this phone with the computer that runs herdr-core.
import { Scroll, signal, Text, View } from 'nsh'
import { Input } from 'nsh/base'
import { pairThrough, parseInvite, parsePairLink, pairWith, tunnelCode, type Link, type Tunnel } from '../link.ts'
import { Button, openPrivacy } from './kit.tsx'
import { t } from '../strings/index.ts'
import { keyboardGone } from './keyboard.ts'

/** `origin`: on the web, the computer that serves the page, where a `#pair=` link opens and the code alone is enough.
 *  `tunnel`: in the phone apps, TailPocket, which pairs by the invite `serve --tailcat` shows, scanned or pasted. */
export function PairScreen(props: { onPaired: (l: Link) => void; device: string; fetcher?: typeof fetch; initial?: string; message?: string; origin?: string; tunnel?: Tunnel; onTry?: () => void; onBack?: () => void }) {
  const text = signal(props.initial ?? '')
  const busy = signal(false)
  const error = signal(props.message ?? '')
  const { origin, tunnel } = props
  const pair = async () => {
    const typed = text().trim()
    const invite = tunnel ? parseInvite(typed) : null
    const p = invite ? null : parsePairLink(typed) ?? (origin && /^[A-Za-z0-9_-]{4,}$/.test(typed) ? { url: origin, code: typed } : null)
    if (!invite && !p) {
      return error.set(tunnel ? t('pasteTunnel')
        : origin ? t('pasteOrigin')
        : t('pasteLan', { example: 'http://192.168.1.20:7681/#pair=K7QD-M2XP' }))
    }
    busy.set(true)
    error.set('')
    try {
      const l = invite ? await pairThrough(tunnel!, invite, props.device) : await pairWith(p!, props.device, props.fetcher)
      await keyboardGone()
      props.onPaired(l)
    } catch (e: any) {
      error.set(e?.message ?? String(e))
      busy.set(false)
    }
  }
  const scan = async () => {
    error.set('')
    try {
      text.set(await tunnel!.scan())
    } catch (e) {
      const code = tunnelCode(e)
      if (code !== 'cancelled') error.set(code === 'unsupported' ? t('cantScan') : t('scanFailed', { code }))
      return
    }
    void pair()
  }
  if (props.initial && (parsePairLink(props.initial) || (tunnel && parseInvite(props.initial)))) queueMicrotask(() => void pair())
  return (
    <Scroll class="flex-1 bg-background" keyboardShouldPersistTaps="handled" contentClass="px-6 pt-[env(safe-area-inset-top)] mt-12 pb-10 gap-3">
      <If when={props.onBack}>
        <View role="button" testID="pair-back" onPress={() => props.onBack!()} class="self-start -ml-2 -mt-8 px-2 py-2 active:opacity-60">
          <Text class="text-[15px] text-foreground">‹ {t('back')}</Text>
        </View>
      </If>
      <Text class="text-[24px] font-semibold text-foreground tracking-[-0.4px]">{t('pairTitle')}</Text>
      <Text class="text-[15px] leading-[22px] text-muted-foreground">
        {tunnel
          ? t('pairIntroTunnel')
          : t('pairIntroLan')}
      </Text>
      <View class="mt-3 gap-2 rounded-(--radius) bg-muted p-4">
        <Text class="text-[13px] text-foreground font-medium">{t('onTheComputer')}</Text>
        <Text class="text-[13px] text-muted-foreground">{t('startCompanion')}</Text>
        <Text selectable class="text-[12.5px] font-mono text-foreground">{tunnel ? 'herdr-anywhere start' : 'herdr-anywhere start --lan'}</Text>
        <Text class="text-[13px] text-muted-foreground">{tunnel ? t('showQr') : t('makeLink')}</Text>
        <Text selectable class="text-[12.5px] font-mono text-foreground">herdr-anywhere pair</Text>
      </View>
      <If when={tunnel}>
        <Button label={t('scanQr')} testID="pair-scan" disabled={busy()} onPress={() => void scan()} />
      </If>
      <Input testID="pair-link" placeholder={tunnel ? 'herdr1:… / http://…/#pair=…' : origin ? t('linkOrCode') : 'http://…/#pair=…'} value={text()} onChange={(t) => text.set(t)}
        autoCapitalize="none" autoCorrect={false} keyboardType="url" submitBehavior="blurAndSubmit" onSubmit={() => void pair()}
        class="mt-2 rounded-(--radius) border border-input bg-card px-3 py-3 text-[14px] font-mono text-foreground placeholder:text-sidebar-foreground" />
      <Button label={busy() ? t('pairing') : t('pair')} testID="pair" disabled={busy()} onPress={() => void pair()} />
      <If when={error()}><Text testID="pair-error" class="text-[14px] leading-5 text-destructive">{error()}</Text></If>
      <If when={props.onTry}>
        <View class="mt-3 gap-2 border-t border-border pt-4">
          <Text class="text-[13px] leading-[18px] text-muted-foreground">{t('tryItHint')}</Text>
          <Button label={t('tryIt')} testID="try-it" kind="outline" disabled={busy()} onPress={() => props.onTry!()} />
        </View>
      </If>
      <View role="button" testID="pair-privacy" onPress={openPrivacy} class="self-center px-3 py-2 active:opacity-60">
        <Text class="text-[13px] text-muted-foreground">{t('privacy')}</Text>
      </View>
    </Scroll>
  )
}
