import { expect, open, test } from 'nsh/test'

test('the first pending connection shows Connecting immediately without blocking Settings', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', hold: 'doc.get' } }, clock: {} })
  await app.clock.runFor(100)
  expect(app.count('id:conn-banner')).toBe(1)
  expect(app.texts()).toContain('Connecting…')
  await app.press('id:settings')
  await app.waitFor('id:look-dark')
  await app.press('id:look-dark')
  expect(JSON.parse(app.storage()['fake-prefs'] ?? '{}').appearance).toBe('dark')
  await app.press('id:back')
  expect(app.count('id:conn-banner')).toBe(1)
  await app.host.send('fake.release', {})
  await app.clock.runFor(100)
  expect(app.count('id:conn-banner')).toBe(0)
  await app.press('id:space-sp1')
  await app.waitFor('id:pane-header')
})

test('local navigation and chat folds work while the disconnected computer cannot answer', async () => {
  const reply = Array.from({ length: 40 }, (_, n) => `Inventory section ${n + 1} is ready.`).join('\n\n')
  const turns = [{ id: 'offline', from: 'you', said: 'Inspect the orchard inventory.', end: 'done', reply,
    steps: [{ id: 'inventory', kind: 'run', title: 'count orchard entries' }] }]
  const app = await open('./fixtures/fake.tsx', { host: { params: {
    scene: 'single', argv: 'claude', status: 'done', chat: JSON.stringify({ chat: true, turns }),
  } }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.press('id:show-chat')
  await app.waitFor('id:reply-offline')
  await app.host.send('fake.chat', { offline: true })
  await app.host.send('fake.drop', {})
  await app.clock.runFor(3200)
  await app.scroll('id:chat-scroll', -10000)
  await app.press('id:fold-offline')
  expect(app.count('id:step-inventory')).toBe(1)
  await app.scroll('id:chat-scroll', 200)
  await app.press('id:back')
  await app.waitFor('id:settings')
  await app.press('id:settings')
  await app.waitFor('id:look-dark')
  await app.press('id:look-dark')
  expect(JSON.parse(app.storage()['fake-prefs'] ?? '{}').appearance).toBe('dark')
  await app.press('id:back')
  await app.press('id:space-sp1')
  await app.waitFor('id:pane-header')
  expect(app.count('id:conn-banner')).toBe(1)
})

test('dropping the line with Chat open finishes connection notification and schedules retry', async () => {
  const turns = [{ id: 'u1', from: 'you', said: 'check the total', end: 'done', reply: 'The total is correct.', steps: [] }]
  const app = await open('./fixtures/fake.tsx', { host: { params: {
    scene: 'single', argv: 'claude', status: 'done', traceConn: '1', chat: JSON.stringify({ chat: true, turns }),
  } }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  await app.clock.runFor(100)
  await app.press('id:show-chat')
  await app.waitFor('id:chat-view')
  await app.waitFor('text:The total is correct.')
  expect(app.count('id:conn-banner')).toBe(0)
  await app.host.send('fake.drop', {})
  await app.clock.runFor(100)
  const raw = app.storage()['fake-conn-trace']
  expect(Boolean(raw)).toBe(true)
  const trace = JSON.parse(raw ?? '{}')
  console.log('connection subscriber trace', JSON.stringify(trace))
  expect(trace.limited).toBe(false)
  expect(trace.finished).toBe(true)
  expect(trace.calls > 0).toBe(true)
  const diags: string[] = JSON.parse(app.storage()['fake-diag'] ?? '[]')
  expect(diags.some((s) => s.startsWith('connection-step ') && s.includes('step=conn-after'))).toBe(true)
  expect(diags.some((s) => s.startsWith('connection-retry-scheduled '))).toBe(true)
  expect(app.count('id:chat-view')).toBe(1)
  expect(app.count('id:conn-banner')).toBe(1)
  expect(app.texts()).toContain('The total is correct.')
})
