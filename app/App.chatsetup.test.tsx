// Some agents' chats need a small file in the agent's own folder (a chat setup: Pi's). The phone asks once, by
// itself, the first time such a chat opens; after "Not now" it never asks by itself again, and the chat page says
// what is missing with an Install button. Installed while the agent runs, the page says what to type in it. Settings
// has a row for each setup, to install or remove at any time. A file someone changed is replaced only when asked.
import { expect, open, test } from 'nsh/test'

const PI = { agent: 'pi', name: 'Pi', file: '~/.pi/agent/extensions/herdr-anywhere.ts', reload: '/reload', version: 1, state: 'absent' }
const needs = { chat: true, unknown: 'setup-needed', setup: 'pi' }
const turns = [{ id: 'u1', from: 'you', said: 'round the login button', end: 'done', reply: 'Done: 8px corners.', steps: [] }]

const pane = async (params: Record<string, string> = {}) => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', argv: 'pi', status: 'idle', chat: JSON.stringify(needs), setups: JSON.stringify([PI]), ...params } }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  await app.clock.runFor(100)
  return app
}
const setupAsks = (app: any): string[] => JSON.parse(app.storage()['fake-setup-asks'] ?? '[]')
const sent = (app: any): string[] => JSON.parse(app.storage()['fake-sent'] ?? '[]').map((s: any) => s[1])

test('the first Pi chat asks once; after Not now the page says what is missing, and it never asks by itself again', async () => {
  const app = await pane()
  await app.press('id:show-chat')
  await app.waitFor('id:chat-setup-ask')
  expect(app.texts()).toContain('Use Pi’s chat on this phone?')
  expect(app.texts().some((x: string) => x.includes('~/.pi/agent/extensions/herdr-anywhere.ts'))).toBe(true)
  await app.press('id:chat-setup-later')
  await app.waitUntil(() => app.count('id:chat-setup-ask') === 0)
  expect(setupAsks(app).filter((a) => !a.startsWith('GET'))).toEqual([])
  // The page itself: what is missing, and Install. The terminal is one tap away.
  expect(app.count('id:chat-setup-install')).toBe(1)
  expect(app.texts()).toContain('Pi’s chat needs one small file on the computer')
  await app.press('id:chat-terminal')
  await app.waitFor('id:show-chat')
  await app.press('id:show-chat')
  await app.waitFor('id:chat-setup-install')
  await app.clock.runFor(500)
  expect(app.count('id:chat-setup-ask')).toBe(0)
})

test('Install puts the file there; a Pi already running is told to type /reload, and the phone can type it', async () => {
  const app = await pane()
  await app.press('id:show-chat')
  await app.waitFor('id:chat-setup-ask')
  await app.press('id:chat-setup-yes')
  await app.waitUntil(() => setupAsks(app).includes('PUT /api/chat-setups/pi'))
  app.host.send('fake.chat', { chat: true, unknown: 'setup-not-loaded', setup: 'pi' })
  await app.clock.runFor(3100)
  await app.waitFor('id:chat-setup-reload')
  expect(app.texts()).toContain('Installed. This Pi was open before, so it starts using the file after /reload or a restart.')
  await app.press('id:chat-setup-reload')
  await app.waitUntil(() => sent(app).some((s) => s.startsWith('/reload')))
  app.host.send('fake.chat', { chat: true, turns })
  await app.clock.runFor(3100)
  await app.waitFor('text:Done: 8px corners.')
})

test('a file someone changed is replaced only when the person says so', async () => {
  const app = await pane({ setups: JSON.stringify([{ ...PI, state: 'changed' }]) })
  await app.press('id:show-chat')
  await app.waitFor('id:chat-setup-ask')
  await app.press('id:chat-setup-yes')
  await app.waitFor('id:chat-setup-changed')
  expect(setupAsks(app)).toContain('PUT /api/chat-setups/pi')
  expect(setupAsks(app)).not.toContain('PUT /api/chat-setups/pi?force=1')
  await app.press('id:chat-setup-replace')
  await app.waitUntil(() => setupAsks(app).includes('PUT /api/chat-setups/pi?force=1'))
})

const settings = async (setups: unknown[]) => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', open: 'settings', setups: JSON.stringify(setups) } }, clock: {} })
  await app.clock.runFor(300)
  return app
}

test('settings has a row for each agent on the computer whose chat needs a file: install or remove at any time', async () => {
  const app = await settings([PI, { ...PI, agent: 'other', name: 'Other', state: 'no-agent' }])
  await app.waitFor('id:setup-pi')
  expect(app.count('id:setup-other')).toBe(0)
  expect(app.texts()).toContain('Pi chat')
  expect(app.texts()).toContain('Off: Pi’s chat can’t show on this phone')
  await app.press('id:setup-pi-install')
  await app.waitFor('text:On')
  expect(setupAsks(app)).toContain('PUT /api/chat-setups/pi')
  const asked = new Promise<string>((said) => app.onDialog((d) => { said(d.title ?? ''); return 'Remove' }))
  await app.press('id:setup-pi-remove')
  expect(await asked).toBe('Remove Pi’s file?')
  await app.waitUntil(() => setupAsks(app).includes('DELETE /api/chat-setups/pi'))
  await app.waitFor('id:setup-pi-install')
})

test('no agent that needs a file: no row', async () => {
  const app = await settings([{ ...PI, state: 'no-agent' }])
  await app.clock.runFor(500)
  expect(app.count('id:setup-pi')).toBe(0)
})
