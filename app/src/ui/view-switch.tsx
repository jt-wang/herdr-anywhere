// The two ways the home page shows things while the progress page is on: the agent list, or the progress page.
// One tap switches; the app opens on the last one.
import { Text, View } from 'nsh'
import { t } from '../strings/index.ts'
import { TOUCH_TARGET } from './kit.tsx'

export type HomeView = 'agents' | 'progress'
export type ViewSwitchProps = { current: HomeView; switchTo: (v: HomeView) => void }

export function ViewSwitch(props: ViewSwitchProps) {
  const seg = (v: HomeView, label: string) => (
    <View role="button" testID={`view-${v}`} accessibilityLabel={label} accessibilityState={{ selected: props.current === v }}
      onPress={() => { if (props.current !== v) props.switchTo(v) }}
      style={{ minHeight: TOUCH_TARGET }}
      class={`px-3.5 justify-center rounded-[10px] ${props.current === v ? 'bg-background' : 'active:bg-sidebar-accent'}`}>
      <Text class={`text-[15px] ${props.current === v ? 'font-medium text-foreground' : 'text-muted-foreground'}`} numberOfLines={1}>{label}</Text>
    </View>
  )
  return (
    <View testID="view-switch" role="tablist" class="flex-row items-center self-start rounded-[12px] bg-muted p-0.5 gap-0.5">
      {seg('agents', t('viewAgents'))}
      {seg('progress', t('progressTitle'))}
    </View>
  )
}
