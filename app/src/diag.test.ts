import { expect, test } from 'nsh/test'
import { diag, diagnosticEndpoint, diagnosticFields, NativeDiagnostics, setDiag } from './diag.ts'
import { diagnosticFetch } from './request.ts'
import { HcClient } from './hc/client.ts'
import { FakeCore } from './testing/fakecore.ts'

const categories = { interaction: true, performance: true, network: true, resources: true }
const off = { supported: false, enabled: false, categories: { interaction: false, performance: false, network: false, resources: false } }
function bridge(enabled = false) {
  const calls: { method: string; args: any; options: any }[] = []
  let status: any = { supported: true, enabled, categories, session: 'native-session', interaction: 'native-touch' }
  const native = new NativeDiagnostics({ available: () => true, call: async (method, args, options) => {
    calls.push({ method, args, options })
    if (method === 'page.diagnostics.set') status = { ...status, ...('enabled' in args ? { enabled: args.enabled } : {}), categories: { ...status.categories, ...args.categories } }
    return method === 'page.diagnostics.export' ? { shared: true } : status
  } })
  return { native, calls, status: (value: any) => { status = value }, events: () => calls.filter((c) => c.method === 'page.diagnostics.event') }
}

test('native diagnostics never send events before confirmed consent, while off, or after opt-out', async () => {
  const b = bridge()
  b.native.event('ui-handler', { action: 'open-space' })
  expect(b.calls).toEqual([])
  await b.native.refresh()
  b.native.event('ui-handler', { action: 'open-space' })
  expect(b.events()).toEqual([])
  await b.native.set(true)
  const before = Date.now()
  b.native.event('ui-handler', { action: 'open-space' })
  expect(b.events().length).toBe(1)
  expect(b.events()[0].args.event).toBe('ui-handler')
  expect(b.events()[0].args.fields).toEqual({ action: 'open-space' })
  expect(b.events()[0].args.at >= before && b.events()[0].args.at <= Date.now()).toBe(true)
  const disabling = b.native.set(false)
  b.native.event('ui-handler', { action: 'open-space' })
  await disabling
  expect(b.events().length).toBe(1)
  // No per-event status roundtrip, and no pre-consent backlog.
  expect(b.calls.map((c) => c.method)).toEqual(['page.diagnostics.status', 'page.diagnostics.set', 'page.diagnostics.event', 'page.diagnostics.set'])
})

test('persisted enabled status resumes logging, but unsupported or malformed hosts fail closed', async () => {
  const b = bridge(true)
  await b.native.refresh()
  b.native.event('app-start', {})
  expect(b.events().length).toBe(1)
  for (const bad of [null, {}, { enabled: true }, { supported: true, enabled: 'true' }, { supported: false, enabled: true }]) {
    b.status(bad)
    await b.native.refresh()
    b.native.event('app-start', {})
    expect(b.native.status.enabled).toBe(false)
    expect(b.events().length).toBe(1)
  }
  const missing = new NativeDiagnostics({ available: () => false, call: async () => { throw new Error('must not call') } })
  expect(await missing.refresh()).toEqual(off)
  expect(await missing.set(true)).toEqual(off)
  expect(await missing.clear()).toBe(false)
  expect(await missing.export()).toBe(false)
})

test('native exports contain only allowlisted event names and fields, never arbitrary diagnostic data', async () => {
  const b = bridge(true)
  await b.native.refresh()
  const privateFields = { text: 'invented input', title: 'invented title', name: 'invented name', path: '/made/up/private',
    url: 'https://example.invalid/secret?token=made-up', token: 'made-up-token', Authorization: 'Bearer made-up-token', payload: 'invented payload' }
  b.native.event('invented-private-event', privateFields)
  b.native.event('__proto__', privateFields)
  expect(b.events()).toEqual([])
  b.native.event('connection-error', { ...privateFields, attempt: '2', step: 'conn-before', name: 'TypeError', message: 'invented exception' })
  expect(b.events()[0].args.fields).toEqual({ attempt: '2', step: 'conn-before', name: 'TypeError' })
  b.native.event('http-end', { ...privateFields, request: 'd12', elapsed: '42', status: '200', method: 'GET', endpoint: '/api/chat/:terminal', result: 'ok' })
  expect(b.events()[1].args.fields).toEqual({ request: 'd12', elapsed: '42', status: '200', method: 'GET', endpoint: '/api/chat/:terminal', result: 'ok' })
  expect(diagnosticFields('rpc-start', { method: 'private.method', rpcId: '/private', request: 'private-id', timeout: 'NaN' })).toEqual({})
  expect(diagnosticFields('connection-error', { name: 'private error name', step: 'private step', attempt: '-1' })).toEqual({})
})

test('native sink failure is best effort and clearing/sharing work even with recording off', async () => {
  const b = bridge()
  await b.native.refresh()
  expect(await b.native.clear()).toBe(true)
  expect(await b.native.export()).toBe(true)
  expect(b.calls.slice(1)).toEqual([
    { method: 'page.diagnostics.clear', args: {}, options: undefined },
    { method: 'page.diagnostics.export', args: {}, options: { timeout: 0 } },
  ])
  const rejecting = new NativeDiagnostics({ available: () => true, call: async (method) => {
    if (method === 'page.diagnostics.status') return { supported: true, enabled: true, categories }
    throw new Error('invented native failure')
  } })
  await rejecting.refresh()
  rejecting.event('ui-handler', { action: 'open-space' })
  await Promise.resolve()
  expect(await rejecting.clear()).toBe(false)
  expect(await rejecting.set(false)).toEqual(off)
})

test('a late status reply cannot override a newer opt-out', async () => {
  let answer: (value: any) => void = () => {}
  let reads = 0
  const native = new NativeDiagnostics({ available: () => true, call: async (method) => {
    if (method === 'page.diagnostics.set') return { supported: true, enabled: false, categories }
    if (++reads === 1) return { supported: true, enabled: true, categories }
    return new Promise((resolve) => { answer = resolve })
  } })
  await native.refresh()
  const old = native.refresh()
  await native.set(false)
  answer({ supported: true, enabled: true, categories })
  await old
  expect(native.status).toEqual({ supported: true, enabled: false, categories })
})

test('category selection preserves master consent and filters disabled categories before sending', async () => {
  const b = bridge(false)
  await b.native.refresh()
  await b.native.setCategories({ network: false, resources: false })
  expect(b.native.status.enabled).toBe(false)
  expect(b.calls.at(-1)?.args).toEqual({ categories: { network: false, resources: false } })
  await b.native.set(true)
  b.native.event('http-start', { request: 'd1', endpoint: '/api/info', method: 'GET' })
  expect(b.events().length).toBe(0)
  b.native.event('ui-handler', { action: 'open-space' })
  expect(b.events().length).toBe(1)
  const disabling = b.native.setCategories({ interaction: false })
  b.native.event('ui-handler', { action: 'open-space' })
  await disabling
  expect(b.events().length).toBe(1)
  expect(b.native.status.enabled).toBe(true)
  await b.native.setCategories({ network: true })
  b.native.event('http-start', { request: 'd2', endpoint: '/api/info', method: 'GET' })
  expect(b.events().length).toBe(2)
  expect(b.native.status.categories.resources).toBe(false)
})

test('the existing e2e hook still receives its unchanged events and raw fields independently of native consent', () => {
  const seen: any[] = []
  const fields = { address: 'invented-terminal', program: 'invented-program' }
  setDiag((event, data) => seen.push([event, data]))
  try { diag('existing-e2e-only', fields); expect(seen).toEqual([['existing-e2e-only', fields]]) }
  finally { setDiag(() => {}) }
})

test('HTTP observation preserves arguments, responses and errors and uses only endpoint templates', async () => {
  const events: any[] = []
  setDiag((event, fields) => events.push({ event, fields }))
  try {
    const url = 'https://example.invalid/api/chat/invented-terminal/steps/private-id?before=private&token=secret'
    const init = { method: 'POST', body: 'invented body', headers: { Authorization: 'Bearer invented' } }
    const response = { status: 503, ok: false, headers: { get: (key: string) => { expect(key).toBe('content-length'); return '42' } } } as Response
    const fetcher = (async (asked, options) => { expect(asked).toBe(url); expect(options).toBe(init); return response }) as typeof fetch
    expect(await diagnosticFetch(fetcher, url, init)).toBe(response)
    expect(events.map((x) => x.event)).toEqual(['http-start', 'http-end'])
    expect(events[0].fields.endpoint).toBe('/api/chat/:terminal/steps/:id')
    expect(events[1].fields.request).toBe(events[0].fields.request)
    expect(events[1].fields.result).toBe('error')
    expect(events[1].fields.status).toBe('503')
    expect(events[1].fields.responseBytes).toBe('42')
    expect(Number(events[1].fields.elapsed) >= 0).toBe(true)
    const failure = new Error('invented request failure')
    let caught: unknown
    try { await diagnosticFetch((async () => { throw failure }) as typeof fetch, url) } catch (e) { caught = e }
    expect(caught).toBe(failure)
    expect(events.at(-1).fields.result).toBe('error')
    expect(JSON.stringify(events).includes('invented')).toBe(false)
    expect(JSON.stringify(events).includes('secret')).toBe(false)
    for (const [path, template] of [['/api/folders?path=private', '/api/folders'], ['/api/chat/private?before=secret', '/api/chat/:terminal'],
      ['/api/chat/private/images/private', '/api/chat/:terminal/images/:id'], ['/api/chat-setups/private', '/api/chat-setups/:agent'],
      ['/unknown/private', 'other'], ['/api/files?name=private', '/api/files'], ['/api/cli', '/api/cli']]) expect(diagnosticEndpoint(path)).toBe(template)
  } finally { setDiag(() => {}) }
})

test('RPC diagnostics correlate replies, existing timeouts and disconnects without request or error text', async () => {
  const events: any[] = []
  setDiag((event, fields) => { if (event.startsWith('rpc-')) events.push({ event, fields }) })
  const core = new FakeCore()
  const client = new HcClient(core, { timeoutMs: 10 })
  try {
    await client.hello()
    await client.call('terminal.list', { private: 'invented payload' })
    core.holding.add('terminal.send')
    let timed: any
    try { await client.call('terminal.send', { text: 'invented secret' }) } catch (e) { timed = e }
    expect(timed.kind).toBe('timeout')
    const pending = client.call('terminal.send', {}, { timeoutMs: 0 }).catch((e) => e)
    core.drop('invented private reason')
    expect((await pending).kind).toBe('disconnected')
    const starts = events.filter((x) => x.event === 'rpc-start')
    const ends = events.filter((x) => x.event === 'rpc-end')
    expect(starts.length).toBe(4)
    expect(ends.map((x) => x.fields.result)).toEqual(['ok', 'ok', 'timeout', 'disconnected'])
    expect(ends.map((x) => x.fields.request)).toEqual(starts.map((x) => x.fields.request))
    expect(new Set(starts.map((x) => x.fields.request)).size).toBe(4)
    expect(starts.at(-1).fields.timeout).toBe('0')
    expect(JSON.stringify(events).includes('invented')).toBe(false)
  } finally { client.close(); setDiag(() => {}) }
})
