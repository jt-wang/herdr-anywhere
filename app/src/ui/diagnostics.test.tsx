import { expect, open, test } from 'nsh/test'

const calls = (app: any) => JSON.parse(app.storage()['diagnostic-calls'] ?? '[]')
const categories = ['interaction', 'performance', 'network', 'resources']
const saved = (app: any) => JSON.parse(app.storage()['diagnostic-saved'] ?? '{}')

test('settings has only a diagnostic entry and current state; its native stack supports button and system back', async () => {
  const app = await open('../../fixtures/fake.tsx', { host: { params: { scene: 'busy' } } })
  await app.waitFor('id:settings')
  await app.press('id:settings')
  await app.waitFor('id:settings-diagnostics')
  expect(app.count('id:settings-diagnostics')).toBe(1)
  expect(app.count('id:diagnostic-toggle')).toBe(0)
  expect(app.count('id:diagnostic-export')).toBe(0)
  expect(app.count('id:diagnostic-clear')).toBe(0)
  expect(app.count('id:settings-diagnostics >> text:Not supported')).toBe(1)
  await app.press('id:settings-diagnostics')
  await app.waitFor('id:diagnostic-unsupported')
  await app.press('id:diagnostic-back')
  await app.waitFor('id:settings-diagnostics')
  await app.press('id:settings-diagnostics')
  await app.back()
  await app.waitFor('id:settings-diagnostics')
})

test('the settings entry shows persisted master consent and updates after returning from diagnostics', async () => {
  let enabled = true
  const status = () => ({ supported: true, enabled, categories: { interaction: false, performance: false, network: true, resources: false } })
  const app = await open('../../fixtures/fake.tsx', { host: {
    params: { scene: 'busy' },
    handlers: {
      'page.diagnostics.status': () => status(),
      'page.diagnostics.set': (args: any) => { enabled = args.enabled; return status() },
      'page.diagnostics.event': {},
    },
  } })
  await app.waitFor('id:settings')
  await app.press('id:settings')
  await app.waitFor('id:settings-diagnostics >> text:On')
  expect(app.host.calls().filter((c) => c.name === 'page.diagnostics.set')).toEqual([])
  await app.press('id:settings-diagnostics')
  await app.waitFor('text:● Recording selected categories')
  await app.press('id:diagnostic-toggle')
  await app.waitFor('text:Recording off — no diagnostic collection. Off by default.')
  await app.press('id:diagnostic-back')
  await app.waitFor('id:settings-diagnostics >> text:Off')
  await app.press('id:settings-diagnostics')
  await app.press('id:diagnostic-toggle')
  await app.waitFor('text:● Recording selected categories')
  await app.back()
  await app.waitFor('id:settings-diagnostics >> text:On')
})

test('fresh master off disables all category switches without writing consent', async () => {
  const app = await open('../testing/diagnostics.tsx')
  await app.waitFor('id:diagnostic-toggle')
  for (const category of categories) {
    expect(app.enabled(`id:diagnostic-${category}`)).toBe(false)
    // Headless press may refuse a disabled view; either way it must never write.
    try { await app.press(`id:diagnostic-${category}`) } catch {}
  }
  expect(app.texts().filter((s) => s === 'Off').length).toBe(4)
  expect(calls(app).filter((c: any) => c.method === 'page.diagnostics.set')).toEqual([])
})

test('diagnostic settings require explicit consent; share and clear are explicit local host actions', async () => {
  const app = await open('../testing/diagnostics.tsx')
  await app.waitFor('id:diagnostic-toggle')
  expect(app.texts()).toContain('Enable → reproduce the problem once → export the report.')
  expect(app.texts()).toContain('Recording off — no diagnostic collection. Off by default.')
  expect(calls(app).map((c: any) => c.method)).toEqual(['page.diagnostics.status'])
  for (const category of categories) expect(app.count(`id:diagnostic-${category}`)).toBe(1)
  await app.press('id:diagnostic-toggle')
  await app.waitUntil(() => calls(app).some((c: any) => c.method === 'page.diagnostics.set' && c.args.enabled === true))
  await app.waitFor('text:No categories selected — not collecting.')
  expect(Object.values(saved(app).categories)).toEqual([false, false, false, false])
  for (const category of categories) {
    expect(app.enabled(`id:diagnostic-${category}`)).toBe(true)
    await app.press(`id:diagnostic-${category}`)
    await app.waitUntil(() => saved(app).categories[category] === true)
    expect(calls(app).at(-1).args).toEqual({ categories: { [category]: true } })
  }
  await app.waitFor('text:● Recording selected categories')
  await app.press('id:diagnostic-export')
  await app.waitUntil(() => calls(app).some((c: any) => c.method === 'page.diagnostics.export'))
  expect(calls(app).find((c: any) => c.method === 'page.diagnostics.export').args).toEqual({})
  expect(app.count('id:diagnostic-note')).toBe(0) // cancelling share makes no success claim
  await app.press('id:diagnostic-clear')
  await app.waitFor('text:Saved diagnostics cleared.')
  await app.press('id:diagnostic-toggle')
  await app.waitUntil(() => calls(app).some((c: any) => c.method === 'page.diagnostics.set' && c.args.enabled === false))
})

test('upgrading preserves active consent and selected categories; master off only masks saved choices', async () => {
  const prior = { interaction: true, performance: false, network: true, resources: false }
  const app = await open('../testing/diagnostics.tsx', { host: { params: { enabled: 'true', categories: JSON.stringify(prior) } } })
  await app.waitFor('text:● Recording selected categories')
  expect(app.texts().filter((s) => s === 'On').length).toBe(2)
  expect(calls(app).filter((c: any) => c.method === 'page.diagnostics.set')).toEqual([])
  expect(saved(app)).toEqual({ enabled: true, categories: prior })
  await app.press('id:diagnostic-toggle')
  await app.waitFor('text:Recording off — no diagnostic collection. Off by default.')
  expect(app.texts().filter((s) => s === 'Off').length).toBe(4)
  for (const category of categories) {
    expect(app.enabled(`id:diagnostic-${category}`)).toBe(false)
    try { await app.press(`id:diagnostic-${category}`) } catch {}
  }
  expect(saved(app)).toEqual({ enabled: false, categories: prior })
  expect(calls(app).filter((c: any) => c.method === 'page.diagnostics.set').map((c: any) => c.args)).toEqual([{ enabled: false }])
  await app.press('id:diagnostic-toggle')
  await app.waitFor('text:● Recording selected categories')
  expect(saved(app)).toEqual({ enabled: true, categories: prior })
  expect(app.texts().filter((s) => s === 'On').length).toBe(2)
})

test('an older disabled master masks true saved preferences and restores them on opt-in', async () => {
  const prior = { interaction: false, performance: true, network: false, resources: true }
  const app = await open('../testing/diagnostics.tsx', { host: { params: { categories: JSON.stringify(prior) } } })
  await app.waitFor('id:diagnostic-toggle')
  expect(app.texts().filter((s) => s === 'Off').length).toBe(4)
  expect(saved(app)).toEqual({ enabled: false, categories: prior })
  await app.press('id:diagnostic-toggle')
  await app.waitFor('text:● Recording selected categories')
  expect(saved(app)).toEqual({ enabled: true, categories: prior })
  expect(calls(app).filter((c: any) => c.method === 'page.diagnostics.set').map((c: any) => c.args)).toEqual([{ enabled: true }])
})

test('unsupported hosts show no usable diagnostic toggle or share/clear actions', async () => {
  const app = await open('../testing/diagnostics.tsx', { host: { params: { unsupported: 'true' } } })
  await app.waitFor('id:diagnostic-unsupported')
  expect(app.count('id:diagnostic-toggle')).toBe(0)
  expect(app.count('id:diagnostic-export')).toBe(0)
  expect(app.count('id:diagnostic-clear')).toBe(0)
  for (const category of ['interaction', 'performance', 'network', 'resources']) expect(app.count(`id:diagnostic-${category}`)).toBe(0)
})
