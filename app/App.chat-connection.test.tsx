import { expect, open, test } from 'nsh/test'

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
