import { expect, open, test } from 'nsh/test'

const CURSOR = { agent: 'cursor', name: 'Cursor', file: '~/.cursor/hooks.json', version: 1, state: 'absent' }
const turns = [{ id: 'u1', from: 'you', said: 'count paper flowers', end: 'done', reply: 'Four blue flowers.', steps: [{ id: 'shell1', kind: 'run', title: 'printf flowers', failed: true }] }]
const asks = (app: any): string[] => JSON.parse(app.storage()['fake-chat-asks'] ?? '[]')

const pane = async (params: Record<string, string> = {}, locale = 'en') => {
  const app = await open('./fixtures/fake.tsx', { locale, host: { params: { scene: 'single', argv: 'cursor-agent', chat: JSON.stringify({ chat: true, turns }), ...params } }, clock: {} })
  await app.clock.runFor(100)
  await app.host.send('fake.foreground', { address: 't1', foreground: null })
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  await app.clock.runFor(100)
  return app
}

test('Cursor launch argv without a status report requests chat, shows tools and sends the composer to the terminal', async () => {
  const app = await pane()
  await app.waitFor('id:show-chat')
  expect(asks(app).some((a) => a.includes('t1'))).toBe(true)
  await app.press('id:show-chat')
  await app.waitFor('text:Four blue flowers.')
  expect(app.texts()).toContain('count paper flowers')
	await app.press('id:fold-u1')
  await app.waitFor('id:step-shell1')
  expect(app.texts()).toContain('printf flowers')
  await app.type('id:compose-input', 'paint one yellow')
  await app.press('id:compose-send')
  await app.waitUntil(() => JSON.parse(app.storage()['fake-sent'] ?? '[]').some((a: any) => a[1].startsWith('paint one yellow')))
  await app.press('id:show-terminal')
  await app.waitFor('id:show-chat')
})

test('an unrelated agent command is not globally treated as Cursor', async () => {
  const app = await pane({ argv: 'agent' })
  await app.clock.runFor(300)
  expect(asks(app)).toEqual([])
  expect(app.count('id:show-chat')).toBe(0)
})

test('Cursor setup installs hooks and tells an already running Cursor to restart, never sending a slash reload', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: true, unknown: 'setup-needed', setup: 'cursor' }), setups: JSON.stringify([CURSOR]) })
  await app.press('id:show-chat')
  await app.waitFor('id:chat-setup-ask')
  expect(app.texts()).toContain('Use Cursor’s chat on this phone?')
  await app.press('id:chat-setup-yes')
  await app.waitFor('text:Installed. This Cursor was open before, so it starts using the file after a restart.')
  expect(app.count('id:chat-setup-reload')).toBe(0)
  expect(JSON.parse(app.storage()['fake-setup-asks'] ?? '[]')).toContain('PUT /api/chat-setups/cursor')
})

for (const [locale, text] of [
  ['en', 'Chat appears after your first message.'],
  ['es', 'El chat aparecerá después de tu primer mensaje.'],
  ['ja', '最初のメッセージを送るとチャットが表示されます。'],
  ['ko', '첫 메시지를 보내면 채팅이 표시돼요.'],
  ['ru', 'Чат появится после вашего первого сообщения.'],
  ['zh-CN', '发送第一条消息后即可看到聊天。'],
  ['zh-TW', '傳送第一則訊息後即可看到聊天。'],
]) {
  test(`${locale}: Cursor started after hook installation waits for a first message, not a restart`, async () => {
    const app = await pane({ chat: JSON.stringify({ chat: true, program: 'cursor', unknown: 'awaiting-prompt' }), setups: JSON.stringify([{ ...CURSOR, state: 'installed' }]) }, locale)
    await app.press('id:show-chat')
    await app.waitFor(`text:${text}`)
    expect(app.count('id:chat-setup-loaded')).toBe(0)
    expect(app.count('id:chat-setup-reload')).toBe(0)
    expect(app.count('id:chat-setup-install')).toBe(0)
    if (locale === 'en') {
      await app.host.send('fake.chat', { chat: true, program: 'cursor', turns })
      await app.clock.runFor(3100)
      await app.waitFor('text:Four blue flowers.')
      expect(app.count('id:chat-unknown')).toBe(0)
    }
  })
}

test('Settings offers Cursor hook installation and removal independently of Pi', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', open: 'settings', setups: JSON.stringify([CURSOR]) } }, clock: {} })
  await app.clock.runFor(300)
  await app.waitFor('id:setup-cursor')
  await app.press('id:setup-cursor-install')
  await app.waitFor('text:On')
  const confirm = new Promise<string>((done) => app.onDialog((d) => { done(d.title ?? ''); return 'Remove' }))
  await app.press('id:setup-cursor-remove')
  expect(await confirm).toBe('Remove Cursor’s file?')
  await app.waitUntil(() => JSON.parse(app.storage()['fake-setup-asks'] ?? '[]').includes('DELETE /api/chat-setups/cursor'))
})

test('a running Cursor gains chat after Settings installs hooks, with restart guidance and no status event', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: false }), setups: JSON.stringify([CURSOR]) })
  expect(app.count('id:show-chat')).toBe(0)
  await app.press('id:space-more')
  await app.press('id:menu-settings')
  await app.waitFor('id:setup-cursor-install')
  await app.host.send('fake.chat', { chat: true, program: 'cursor', unknown: 'setup-not-loaded', setup: 'cursor' })
  await app.press('id:setup-cursor-install')
  await app.waitFor('text:On')
  await app.press('id:back')
  await app.waitFor('id:show-chat')
  await app.press('id:show-chat')
  await app.waitFor('text:Installed. This Cursor was open before, so it starts using the file after a restart.')
})

test('Cursor availability retries without status or foreground, including after an unrelated label event', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: false }) })
  expect(app.count('id:show-chat')).toBe(0)
  await app.host.send('fake.chat', { chat: true, program: 'cursor', turns })
  await app.clock.runFor(1500)
  await app.host.send('fake.label', { address: 't1', labels: { unrelated: 'value' } })
  await app.clock.runFor(1700)
  await app.waitFor('id:show-chat')
  await app.press('id:show-chat')
  await app.waitFor('text:Four blue flowers.')
})

const labels = { 'herdr-anywhere.chat.pid': '4242', 'herdr-anywhere.chat.session': 'invented-session', 'herdr-anywhere.chat.cursor.cwd': '/synthetic/flowers' }

test('the chat page terminal shortcut remains selected after Settings returns to the same pane', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: true, unknown: 'setup-needed', setup: 'cursor' }), setups: JSON.stringify([CURSOR]) })
  await app.press('id:show-chat')
  await app.waitFor('id:chat-terminal')
  await app.press('id:chat-terminal')
  await app.waitFor('id:show-chat')
  await app.press('id:space-more')
  await app.press('id:menu-settings')
  await app.waitFor('id:setup-cursor-install')
  await app.press('id:back')
  await app.clock.runFor(100)
  expect(app.count('id:show-chat')).toBe(1)
  expect(app.count('id:chat-terminal')).toBe(0)
})

test('an unrelated foreground gained behind Settings clears the old chat availability on return', async () => {
  const app = await pane({ argv: 'bash', setups: JSON.stringify([CURSOR]) })
  await app.waitFor('id:show-chat')
  await app.press('id:space-more')
  await app.press('id:menu-settings')
  await app.waitFor('id:setup-cursor-install')
  await app.host.send('fake.foreground', { address: 't1', foreground: { pid: 3131, name: 'bash', argv: ['bash'] } })
  await app.press('id:back')
  await app.clock.runFor(100)
  expect(app.count('id:show-chat')).toBe(0)
})

for (const hiddenFor of [100, 10_000]) test(`Cursor probes pause behind Settings and resume once, even across a late answer: hidden=${hiddenFor}`, async () => {
  const app = await pane({ chat: JSON.stringify({ chat: false }), setups: JSON.stringify([CURSOR]) })
  await app.host.send('fake.chat', { chat: false, slow: 1000 })
  await app.clock.runFor(3000)
  await app.press('id:space-more')
  await app.press('id:menu-settings')
  await app.waitFor('id:setup-cursor-install')
  const covered = asks(app).length
  await app.clock.runFor(hiddenFor)
  expect(asks(app).length).toBe(covered)
  await app.host.send('fake.chat', { chat: false })
  await app.press('id:back')
  await app.clock.runFor(100)
  expect(asks(app).length).toBe(covered + 1)
  await app.clock.runFor(6100)
  expect(asks(app).length).toBe(covered + 3)
})

test('Cursor started in an open unreported shell offers setup without a metadata event', async () => {
  const app = await pane({ argv: 'bash', chat: JSON.stringify({ chat: false }), setups: JSON.stringify([CURSOR]) })
  expect(app.count('id:show-chat')).toBe(0)
  await app.host.send('fake.chat', { chat: true, program: 'cursor', unknown: 'setup-needed', setup: 'cursor' })
  await app.clock.runFor(10100)
  await app.waitFor('id:show-chat')
  await app.press('id:show-chat')
  await app.waitFor('id:chat-setup-ask')
})

test('removing Cursor setup clears the previously readable conversation', async () => {
  const app = await pane({ setups: JSON.stringify([{ ...CURSOR, state: 'installed' }]) })
  await app.press('id:show-chat')
  await app.waitFor('text:Four blue flowers.')
  await app.press('id:space-more')
  await app.press('id:menu-settings')
  await app.waitFor('id:setup-cursor-remove')
  app.onDialog(() => 'Remove')
  await app.host.send('fake.chat', { chat: true, program: 'cursor', unknown: 'setup-needed', setup: 'cursor' })
  await app.press('id:setup-cursor-remove')
  await app.waitFor('text:Off: Cursor’s chat can’t show on this phone')
  await app.press('id:back')
  await app.clock.runFor(3100)
  if (app.count('id:show-chat')) await app.press('id:show-chat')
  await app.waitFor('id:chat-setup-ask')
  expect(app.texts()).not.toContain('Four blue flowers.')
})

test('shell-launched Cursor labels trigger validated discovery without navigation; stale labels do not retain Cursor', async () => {
  const app = await pane({ argv: 'bash', chat: JSON.stringify({ chat: false }) })
  expect(app.count('id:show-chat')).toBe(0)
  await app.host.send('fake.label', { address: 't1', labels })
  await app.clock.runFor(100)
  expect(app.count('id:show-chat')).toBe(0)
  expect(app.texts()).not.toContain('Cursor')
  await app.host.send('fake.chat', { chat: true, program: 'cursor', turns })
  await app.clock.runFor(3100)
  await app.waitFor('id:show-chat')
  expect(app.texts()).toContain('Cursor')
  await app.host.send('fake.chat', { chat: false })
  await app.clock.runFor(3100)
  expect(app.count('id:show-chat')).toBe(0)
  expect(app.texts()).not.toContain('Cursor')
})

test('a delayed Cursor reply cannot classify a different labeled process', async () => {
  const app = await pane({ argv: 'bash', chat: JSON.stringify({ chat: false }) })
  await app.host.send('fake.chat', { chat: true, program: 'cursor', turns, slow: 1000 })
  await app.host.send('fake.label', { address: 't1', labels })
  await app.clock.runFor(100)
  await app.host.send('fake.chat', { chat: false })
  await app.host.send('fake.label', { address: 't1', labels: { ...labels, 'herdr-anywhere.chat.pid': '4343' } })
  await app.clock.runFor(1200)
  expect(app.count('id:show-chat')).toBe(0)
  expect(app.texts()).not.toContain('Cursor')
})

test('an unrelated foreground prevents stale labels from selecting Cursor and is never polled as Cursor', async () => {
  const app = await pane({ argv: 'bash', chat: JSON.stringify({ chat: false }) })
  await app.host.send('fake.label', { address: 't1', labels })
  await app.clock.runFor(100)
  await app.host.send('fake.foreground', { address: 't1', foreground: { pid: 3131, name: 'bash', argv: ['bash'] } })
  const before = asks(app).length
  await app.host.send('fake.chat', { chat: true, program: 'cursor', turns })
  await app.clock.runFor(6500)
  expect(asks(app).length).toBe(before)
  expect(app.count('id:show-chat')).toBe(0)
  expect(app.texts()).not.toContain('Cursor')
})
