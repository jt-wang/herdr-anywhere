// Isolated settings contract fixture: native consent and sharing are answered locally, with no network or files.
import { host, mount, Storage, View } from 'nsh'
import { NativeDiagnostics } from '../diag.ts'
import { DiagnosticsScreen } from '../ui/settings.tsx'

const params = host.params as Record<string, string>
let enabled = params.enabled === 'true'
let categories = { interaction: false, performance: false, network: false, resources: false, ...JSON.parse(params.categories ?? '{}') }
const calls: { method: string; args: any }[] = []
const controller = new NativeDiagnostics({ available: () => true, call: async (method, args) => {
  calls.push({ method, args })
  await Storage.setItem('diagnostic-calls', JSON.stringify(calls))
  if (params.unsupported) throw new Error('unsupported')
  if (method === 'page.diagnostics.set') {
    if (typeof args.enabled === 'boolean') enabled = args.enabled
    categories = { ...categories, ...args.categories }
  }
  await Storage.setItem('diagnostic-saved', JSON.stringify({ enabled, categories }))
  return method === 'page.diagnostics.export' ? { shared: false } : { supported: true, enabled, categories }
} })
mount(() => <View class="flex-1"><DiagnosticsScreen controller={controller} back={() => {}} /></View>)
