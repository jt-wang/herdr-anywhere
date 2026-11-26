// The existing e2e hook is independent of opt-in, local native diagnostics. Never forward its raw fields to native.
import { host } from 'nsh'

export type Diag = (what: string, fields: Record<string, string>) => void
let hook: Diag = () => {}
export const setDiag = (f: Diag) => { hook = f }
export const diag: Diag = (what, fields) => { diagnostics.event(what, fields); hook(what, fields) }

type Bridge = { available(): boolean; call(method: string, args?: any, options?: { timeout: number }): Promise<any> }
export const DIAGNOSTIC_CATEGORIES = ['interaction', 'performance', 'network', 'resources'] as const
export type DiagnosticCategory = typeof DIAGNOSTIC_CATEGORIES[number]
export type DiagnosticCategories = Record<DiagnosticCategory, boolean>
export type DiagnosticStatus = { supported: boolean; enabled: boolean; categories: DiagnosticCategories }
const NO_CATEGORIES: DiagnosticCategories = { interaction: false, performance: false, network: false, resources: false }
const OFF: DiagnosticStatus = { supported: false, enabled: false, categories: NO_CATEGORIES }
const oneOf = (...values: string[]) => (value: string) => values.includes(value)
const number = (value: string) => /^(0|[1-9]\d{0,14})$/.test(value)
const bool = oneOf('true', 'false')
const step = oneOf('start', 'hello-before', 'hello-after', 'subscribe-before', 'subscribe-after', 'ready',
  'entry', 'stop-loops-before', 'stop-loops-after', 'clear-client-before', 'clear-client-after',
  'conn-before', 'conn-after', 'lost-why-before', 'lost-why-after', 'error-before', 'error-after')
const errorName = oneOf('Error', 'TypeError', 'RangeError', 'ReferenceError', 'SyntaxError', 'URIError', 'EvalError', 'other')
const route = oneOf('home', 'space', 'settings', 'diagnostics', 'progress', 'task', 'pair')
const request = {
  request: (value: string) => /^d[1-9]\d{0,14}$/.test(value),
  elapsed: number, status: number, timeout: number, requestBytes: number, responseBytes: number,
  result: oneOf('ok', 'error', 'timeout', 'disconnected'),
}
const endpoints = ['/api/pair', '/api/info', '/api/folders', '/api/chat/:terminal', '/api/chat/:terminal/steps/:id',
  '/api/chat/:terminal/images/:id', '/api/chat-setups', '/api/chat-setups/:agent', '/api/cli', '/api/file', '/api/file/check',
  '/api/file/link', '/api/files', '/api/image', '/api/image/check', '/api/update', '/api/update/check', '/api/update/checks', '/api/update/auto', 'other']
const rpcMethods = ['hello', 'terminal.run', 'terminal.list', 'terminal.kill', 'terminal.label', 'terminal.attach',
  'terminal.keep', 'terminal.get', 'terminal.send', 'terminal.keys', 'terminal.read', 'doc.get', 'doc.set',
  'events.subscribe', 'machine.list', 'machine.exec', 'server.status', 'other']
const http = { ...request, endpoint: oneOf(...endpoints), method: oneOf('GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'other') }
const rpc = { ...request, method: oneOf(...rpcMethods), rpcId: number }
type Rules = Record<string, (value: string) => boolean>
// Deny by default: new e2e events/fields require an explicit privacy review here before appearing in exports.
const RULES: Record<string, Rules> = {
  'app-start': {},
  'ui-handler': { action: oneOf('open-space', 'open-terminal') },
  'ui-open-space': { step: oneOf('enter', 'selected', 'popped', 'pushed', 'already-open') },
  'ui-nav': { route, depth: number },
  'ui-page': { route },
  'http-start': http, 'http-end': http,
  'rpc-start': rpc, 'rpc-end': rpc,
  'socket-start': { ...request, enc: oneOf('bin', 'b64') },
  'socket-open': request,
  'socket-end': { ...request, reason: oneOf('unpaired', 'no-core', 'binary-unsupported', 'network', 'closed') },
  'tunnel-start': request, 'tunnel-end': request,
  'connection-connect': { attempt: number, step },
  'connection-connect-catch': { attempt: number, name: errorName },
  'connection-lost': { wanted: bool, retrying: bool, attempt: number },
  'connection-step': { attempt: number, step },
  'connection-return': { attempt: number, reason: oneOf('same-attempt', 'not-wanted', 'unpaired', 'retry-pending') },
  'connection-retry-fired': { attempt: number, wanted: bool, hasClient: bool },
  'connection-retry-scheduled': { attempt: number, delay: number },
  'connection-error': { attempt: number, step, name: errorName },
}

/** No addresses, payloads, exception messages or dynamic names, even from existing diagnostic callers. */
export function diagnosticFields(event: string, fields: Record<string, string>): Record<string, string> | null {
  if (!Object.prototype.hasOwnProperty.call(RULES, event)) return null
  const safe: Record<string, string> = {}
  for (const [key, accepts] of Object.entries(RULES[event])) {
    const value = fields[key]
    if (typeof value === 'string' && accepts(value)) safe[key] = value
  }
  return safe
}

export class NativeDiagnostics {
  private state: DiagnosticStatus = OFF
  private revision = 0
  constructor(private bridge: Bridge) {}
  get status() { return this.state }
  async refresh(): Promise<DiagnosticStatus> { return this.read('page.diagnostics.status', {}) }
  async set(enabled: boolean): Promise<DiagnosticStatus> {
    if (!this.state.supported) return this.state
    // Stop immediately on opt-out; don't resume until the host confirms a later opt-in.
    this.state = { ...this.state, enabled: false }
    return this.read('page.diagnostics.set', { enabled })
  }
  async setCategories(updates: Partial<DiagnosticCategories>): Promise<DiagnosticStatus> {
    if (!this.state.supported) return this.state
    const categories: Partial<DiagnosticCategories> = {}
    const local = { ...this.state.categories }
    for (const category of DIAGNOSTIC_CATEGORIES) if (typeof updates[category] === 'boolean') {
      categories[category] = updates[category]
      if (!updates[category]) local[category] = false
    }
    // Keep master consent unchanged. Newly selected categories start only after native acknowledgement.
    this.state = { ...this.state, categories: local }
    return this.read('page.diagnostics.set', { categories })
  }
  private async read(method: string, args: object): Promise<DiagnosticStatus> {
    const revision = ++this.revision
    try {
      const value = this.bridge.available() ? await this.bridge.call(method, args) : null
      if (revision === this.revision) this.state = value?.supported === true && typeof value.enabled === 'boolean'
        && DIAGNOSTIC_CATEGORIES.every((c) => typeof value.categories?.[c] === 'boolean')
        ? { supported: true, enabled: value.enabled, categories: {
          interaction: value.categories.interaction, performance: value.categories.performance,
          network: value.categories.network, resources: value.categories.resources,
        } } : OFF
    } catch { if (revision === this.revision) this.state = OFF }
    return this.state
  }
  event(event: string, fields: Record<string, string>) {
    if (!this.state.supported || !this.state.enabled) return
    const category = event === 'app-start' || event.startsWith('ui-') ? 'interaction' : 'network'
    if (!this.state.categories[category]) return
    const safe = diagnosticFields(event, fields)
    if (!safe) return
    // Best effort, no per-event status roundtrip or queue containing pre-consent events.
    try { void this.bridge.call('page.diagnostics.event', { event, at: Date.now(), fields: safe }).catch(() => {}) } catch {}
  }
  async clear(): Promise<boolean> {
    if (!this.state.supported) return false
    try { await this.bridge.call('page.diagnostics.clear', {}); return true } catch { return false }
  }
  async export(): Promise<boolean> {
    if (!this.state.supported) return false
    // The host asks for the optional note and reproduction times, then opens the share sheet. Never upload here.
    const result = await this.bridge.call('page.diagnostics.export', {}, { timeout: 0 })
    return result?.shared === true
  }
}

export const diagnostics = new NativeDiagnostics(host)
let nextRequest = 0
/** Process-local IDs contain no user data. Native adds its persisted session and latest touch ID on receipt. */
export function diagnosticRequest() { return `d${++nextRequest}` }
export function diagnosticMethod(method: string) { return rpcMethods.includes(method) ? method : 'other' }
export function diagnosticEndpoint(url: string): string {
  const path = url.replace(/^https?:\/\/[^/]+/, '').split(/[?#]/)[0]
  if (endpoints.includes(path) && !path.includes(':')) return path
  if (/^\/api\/chat\/[^/]+\/steps\/[^/]+$/.test(path)) return '/api/chat/:terminal/steps/:id'
  if (/^\/api\/chat\/[^/]+\/images\/[^/]+$/.test(path)) return '/api/chat/:terminal/images/:id'
  if (/^\/api\/chat\/[^/]+$/.test(path)) return '/api/chat/:terminal'
  if (/^\/api\/chat-setups\/[^/]+$/.test(path)) return '/api/chat-setups/:agent'
  return 'other'
}
