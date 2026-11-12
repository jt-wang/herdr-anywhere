// The chat view: one tap from a pane's header, over its terminal, and one tap back. The computer decides which panes
// have one (GET /api/chat/{terminal}); the phone shows its turns, folds a finished turn's steps, fetches a step's
// details on a tap, and shows nothing it isn't sure of.
import { expect, open, test } from 'nsh/test'

const turns = [
  { id: 'u1', from: 'you', said: 'fix the total', end: 'done', secs: 130, reply: 'Fixed: total_items counts each item once.',
    steps: [{ id: 'toolu_1', kind: 'read', title: 'orders.py' }, { id: 'toolu_2', kind: 'edit', title: 'orders.py' }] },
  { id: 'u2', from: 'other', said: '<task-notification>tests passed</task-notification>', end: 'done', reply: 'All 12 tests pass.', steps: [] },
]
const ask = [{ id: 'u3', from: 'you', said: 'pick a color', end: 'open', reply: '', steps: [{ id: 'toolu_3', kind: 'run', title: 'List files' }],
  ask: { text: 'Which color should the button be?', options: ['Red', 'Blue'] } }]

const pane = async (params: Record<string, string>, handlers?: Record<string, unknown>) => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', argv: 'claude', status: 'done', ...params }, ...(handlers ? { handlers } : {}) }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  await app.clock.runFor(100)
  return app
}
// A message to the fake (app.host.send) lands a moment later: the clock goes on in 3 s polls until the text shows.
const pollUntil = async (app: any, text: string) => {
  for (let i = 0; i < 10 && !app.texts().some((x: string) => x.includes(text)); i++) await app.clock.runFor(3100)
  await app.waitFor(`text:${text}`)
}
const asks = (app: any): string[] => JSON.parse(app.storage()['fake-chat-asks'] ?? '[]')

test('a pane the computer has no chat for shows no chat button', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: false }) })
  expect(asks(app)).toContain('/api/chat/t1')
  expect(app.count('id:show-chat')).toBe(0)
})

test('a shell is not even asked about', async () => {
  const app = await pane({ argv: 'zsh', status: '', chat: JSON.stringify({ chat: true, turns }) })
  expect(asks(app)).toEqual([])
  expect(app.count('id:show-chat')).toBe(0)
})

test('the chat opens over the terminal and goes back in one tap; the composer stays', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: true, turns }) })
  await app.press('id:show-chat')
  await app.waitFor('text:fix the total')
  expect(app.texts()).toContain('Fixed: total_items counts each item once.')
  expect(app.texts()).toContain('From another program')
  expect(app.count('id:compose-input')).toBe(1)
  await app.press('id:show-terminal')
  await app.waitUntil(() => !app.texts().includes('fix the total'))
  expect(app.count('id:terminal-t1')).toBe(1)
  expect(app.count('id:show-chat')).toBe(1)
})

test('a finished turn folds its steps behind one line; a tap opens them, a tap on a step fetches its details', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: true, turns }) })
  await app.press('id:show-chat')
  await app.waitFor('text:Worked for 2m · 2 steps')
  expect(app.count('id:step-toolu_1')).toBe(0)
  await app.press('id:fold-u1')
  await app.waitFor('id:step-toolu_1')
  expect(asks(app).some((a) => a.includes('/steps/'))).toBe(false)
  await app.press('id:step-toolu_2')
  await app.waitFor('text:input of toolu_2')
  expect(app.texts()).toContain('output of toolu_2')
  expect(asks(app)).toContain('/api/chat/t1/steps/toolu_2')
})

test('a step whose details are gone says so', async () => {
  const gone = [{ ...turns[0], end: 'open', steps: [{ id: 'gone', kind: 'run', title: 'make' }] }]
  const app = await pane({ status: 'working', chat: JSON.stringify({ chat: true, turns: gone }) })
  await app.press('id:show-chat')
  await app.press('id:step-gone')
  await app.waitFor("text:This step's details are gone.")
})

test('an open question shows the question and its options, to answer in the terminal', async () => {
  const app = await pane({ status: 'blocked', chat: JSON.stringify({ chat: true, turns: ask }) })
  await app.press('id:show-chat')
  await app.waitFor('text:Which color should the button be?')
  expect(app.texts()).toContain('Red')
  expect(app.texts()).toContain('Blue')
  expect(app.texts()).toContain('Answer it in the terminal.')
  expect(app.count('id:step-toolu_3')).toBe(1) // an open turn's steps show
})

test('when the computer is not sure it shows no turn at all, only why and the way back', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: true, unknown: 'format' }) })
  await app.press('id:show-chat')
  await app.waitFor("text:This version of the agent writes its record in a way the app can't read yet.")
  await app.press('id:chat-terminal')
  await app.waitFor('id:show-chat')
})

// OpenCode 1 reports no status (measured with 1.19.0): known by what it runs, it is asked about, and its view asks
// again every 3 s, as nothing says when its record grows. (The composer's "Tell OpenCode…": e2e/android-chat-agents.sh.)
test('an OpenCode that reports no status still has its chat, and the view keeps asking', async () => {
  const app = await pane({ argv: 'opencode', status: '', chat: JSON.stringify({ chat: true, unknown: 'not-reported' }) })
  expect(asks(app)).toContain('/api/chat/t1')
  await app.press('id:show-chat')
  await app.waitFor("text:Can't tell which conversation this is yet. It shows once OpenCode does some work.")
  app.host.send('fake.chat', { chat: true, turns })
  await pollUntil(app, 'Fixed: total_items counts each item once.')
})

test('an OpenCode that has not said which conversation it shows: the view says so, and shows it once OpenCode works', async () => {
  const app = await pane({ argv: 'opencode', status: 'idle', chat: JSON.stringify({ chat: true, unknown: 'not-reported' }) })
  await app.press('id:show-chat')
  await app.waitFor("text:Can't tell which conversation this is yet. It shows once OpenCode does some work.")
  app.host.send('fake.chat', { chat: true, turns: ask })
  app.host.send('fake.status', { address: 't1', state: 'working' })
  await pollUntil(app, 'pick a color')
})

test('while the agent works the view asks again every 3 s, and shows what is new', async () => {
  const app = await pane({ status: 'working', chat: JSON.stringify({ chat: true, turns: ask }) })
  await app.press('id:show-chat')
  await app.waitFor('text:pick a color')
  const before = asks(app).length
  app.host.send('fake.chat', { chat: true, turns: [{ ...ask[0], ask: undefined, end: 'done', reply: 'Blue it is.' }] })
  await pollUntil(app, 'Blue it is.')
  expect(asks(app).length).toBeGreaterThan(before)
})

test('the record falling behind shows why, then the turns once it catches up', async () => {
  const app = await pane({ status: 'working', chat: JSON.stringify({ chat: true, unknown: 'behind' }) })
  await app.press('id:show-chat')
  await app.waitFor("text:The record hasn't caught up with the screen yet.")
  app.host.send('fake.chat', { chat: true, turns: ask })
  await pollUntil(app, 'pick a color')
})

test('a computer that stops answering leaves the last turns up, marked', async () => {
  const app = await pane({ status: 'working', chat: JSON.stringify({ chat: true, turns: ask }) })
  await app.press('id:show-chat')
  await app.waitFor('text:pick a color')
  app.host.send('fake.chat', { offline: true })
  await pollUntil(app, "Can't reach the computer. This is what it said last.")
  expect(app.texts()).toContain('pick a color')
})

test('the view picked last is the one a pane opens in, here and in the next pane', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: true, turns }) })
  await app.press('id:show-chat')
  await app.waitFor('text:fix the total')
  await app.press('id:back')
  await app.press('id:space-sp1')
  await app.waitFor('text:fix the total') // no tap this time
  await app.press('id:show-terminal')
  await app.press('id:back')
  await app.press('id:space-sp1')
  await app.waitFor('id:show-chat')
  expect(app.texts()).not.toContain('fix the total')
})

test('the choice is kept with the phone\'s settings, so the app opens agents in the chat next time', async () => {
  const app = await pane({ prefs: JSON.stringify({ chatFirst: true }), chat: JSON.stringify({ chat: true, turns }) })
  await app.waitFor('text:fix the total')
})

test('a pane with no chat opens in its terminal whatever was picked', async () => {
  const app = await pane({ prefs: JSON.stringify({ chatFirst: true }), chat: JSON.stringify({ chat: false }) })
  expect(app.count('id:terminal-t1')).toBe(1)
  expect(app.count('id:chat-view')).toBe(0)
})

test('a reply shows as Markdown: the heading, list and code, without their marks', async () => {
  const md = [{ ...turns[0], reply: '## Notes\n\n- **notes.txt** has one line\n\n```sh\ncat notes.txt\n```' }]
  const app = await pane({ chat: JSON.stringify({ chat: true, turns: md }) })
  await app.press('id:show-chat')
  await app.waitFor('text:Notes')
  const all = app.texts().join('\n')
  expect(all).toContain('cat notes.txt')
  expect(all).not.toContain('## Notes')
  expect(all).not.toContain('**')
})

test('images sent with a prompt, and in a step\'s result, show as pictures', async () => {
  const pics = [{ ...turns[0], images: [{ id: 'u1.1' }], steps: [{ id: 'toolu_img', kind: 'read', title: 'shot.png' }] }]
  const app = await pane({ chat: JSON.stringify({ chat: true, turns: pics }) })
  await app.press('id:show-chat')
  await app.waitFor('id:image-u1.1')
  expect(asks(app)).toContain('/api/chat/t1/images/u1.1')
  await app.press('id:fold-u1')
  await app.press('id:step-toolu_img')
  await app.waitFor('id:image-toolu_img.0')
})

test('an image shows under the path it came from, which can be selected and copied', async () => {
  const pics = [{ ...turns[0], images: [{ id: 'u1.1', source: '/tmp/claude/x/images/1.png' }, { id: 'u1.2' }], steps: [{ id: 'toolu_img', kind: 'read', title: 'shot.png' }] }]
  const app = await pane({ chat: JSON.stringify({ chat: true, turns: pics }) })
  await app.press('id:show-chat')
  await app.waitFor('id:image-u1.1')
  expect(app.count('id:image-source-u1.1')).toBe(1)
  expect(app.texts()).toContain('/tmp/claude/x/images/1.png')
  await app.waitFor('id:image-u1.2')
  expect(app.count('id:image-source-u1.2')).toBe(0)
  await app.press('id:fold-u1')
  await app.press('id:step-toolu_img')
  await app.waitFor('id:image-toolu_img.0')
  expect(app.texts()).toContain('/work/acme/toolu_img.png')
})

test('thinking is a step of its own, its whole text a tap away', async () => {
  const thought = [{ ...turns[0], steps: [{ id: 'a1', kind: 'think', title: 'The total counts twice.' }] }]
  const app = await pane({ chat: JSON.stringify({ chat: true, turns: thought }) })
  await app.press('id:show-chat')
  await app.press('id:fold-u1')
  await app.waitFor('text:The total counts twice.')
  await app.press('id:step-a1')
  await app.waitFor('text:input of a1')
})

test('what was opened stays open when the view asks again and a turn is added', async () => {
  const app = await pane({ status: 'working', chat: JSON.stringify({ chat: true, turns: [...turns, ask[0]] }) })
  await app.press('id:show-chat')
  await app.press('id:fold-u1')
  await app.press('id:step-toolu_2')
  await app.waitFor('text:input of toolu_2')
  app.host.send('fake.chat', { chat: true, turns: [...turns, { ...ask[0], ask: undefined, end: 'done', reply: 'Blue it is.' }] })
  await pollUntil(app, 'Blue it is.')
  expect(app.count('id:step-toolu_1')).toBe(1)
  expect(app.texts()).toContain('input of toolu_2')
})

test('a record falling behind for a moment keeps the turns up with "Updating…" at the end, and what was opened stays open', async () => {
  const app = await pane({ status: 'working', chat: JSON.stringify({ chat: true, turns: [...turns, ask[0]] }) })
  await app.press('id:show-chat')
  await app.press('id:fold-u1')
  await app.waitFor('id:step-toolu_1')
  app.host.send('fake.chat', { chat: true, unknown: 'behind' })
  await pollUntil(app, 'Updating…')
  expect(app.count('id:step-toolu_1')).toBe(1)
  expect(app.texts()).toContain('fix the total')
  expect(app.texts()).not.toContain("The record hasn't caught up with the screen yet.")
  app.host.send('fake.chat', { chat: true, turns: [...turns, { ...ask[0], ask: undefined, end: 'done', reply: 'Blue it is.' }] })
  await pollUntil(app, 'Blue it is.')
  expect(app.count('id:step-toolu_1')).toBe(1)
  expect(app.texts()).not.toContain('Updating…')
})

test('a record behind for over 10 s says so above the turns', async () => {
  const app = await pane({ status: 'working', chat: JSON.stringify({ chat: true, turns: [...turns, ask[0]] }) })
  await app.press('id:show-chat')
  await app.waitFor('text:fix the total')
  app.host.send('fake.chat', { chat: true, unknown: 'behind' })
  await pollUntil(app, 'Updating…')
  expect(app.texts()).not.toContain("The record hasn't caught up with the screen yet.")
  await pollUntil(app, "The record hasn't caught up with the screen yet.")
  expect(app.texts()).toContain('fix the total')
})

test('an agent running with its record off says so', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: true, unknown: 'no-record' }) })
  await app.press('id:show-chat')
  await app.waitFor('text:This agent runs with its record turned off, so there is no conversation to show.')
})

// What the computer read of a step, shown as itself: a command with what it printed last, a change as its lines,
// a read as the lines it read. The raw input (JSON) is not shown for these.
const printed = Array.from({ length: 15 }, (_, i) => `line ${i + 1}`).join('\n')
const shaped = (details: Record<string, unknown>) => JSON.stringify({ chat: true, details, turns: [{ id: 'u1', from: 'you', said: 'fix the total', end: 'done', reply: 'Done.',
  steps: [{ id: 'toolu_r', kind: 'run', title: 'npm test' }, { id: 'toolu_e', kind: 'edit', title: 'orders.py', added: 2, deleted: 1 }, { id: 'toolu_d', kind: 'read', title: 'orders.py' }] }] })

test('a command shows itself, that it failed and its exit code, and the end of what it printed; the rest a tap away', async () => {
  const app = await pane({ chat: shaped({ toolu_r: { input: '{"command": "npm test"}', output: printed, run: { command: 'npm test', failed: true, code: 1, output: printed, lines: 15 } } }) })
  await app.press('id:show-chat')
  await app.press('id:fold-u1')
  await app.press('id:step-toolu_r')
  await app.waitFor('id:run-toolu_r')
  expect(app.texts()).toContain('npm test')
  expect(app.texts()).toContain('Failed · exit code 1')
  expect(app.texts().some((x: string) => x.includes('line 15'))).toBe(true)
  expect(app.texts().some((x: string) => x.includes('line 1\n'))).toBe(false)
  expect(app.texts().some((x: string) => x.includes('"command"'))).toBe(false)
  await app.press('id:run-all-toolu_r')
  await app.waitUntil(() => app.texts().some((x: string) => x.startsWith('line 1\n')))
})

test('a change shows its lines, marked and numbered as in the file, once with how many were added and removed', async () => {
  const change = { file: '/work/acme/orders.py', added: 2, deleted: 1, hunks: [{ oldStart: 41, newStart: 41, lines: ['  if not items:', '-    return 0', '+    return sum(', '+      i.count for i in items)'] }] }
  const app = await pane({ chat: shaped({ toolu_e: { input: '{}', output: 'updated', change } }) })
  await app.press('id:show-chat')
  await app.press('id:fold-u1')
  await app.press('id:step-toolu_e')
  await app.waitFor('id:change-toolu_e')
  expect(app.texts().filter((x: string) => x === '+2 −1').length).toBe(1)
  for (const x of ['    return 0', '    return sum(', '      i.count for i in items)']) expect(app.texts()).toContain(x)
  // The removed line has its old number, the added ones their new numbers: 41 is kept, 42 removed, 42 and 43 added.
  expect(app.texts().filter((x: string) => x === '42').length).toBe(2)
  expect(app.texts()).toContain('43')
  expect(app.texts()).not.toContain('updated')
})

test('a read says which lines of the file it read', async () => {
  const app = await pane({ chat: shaped({ toolu_d: { input: '{}', output: '1\timport os', read: { file: '/work/acme/orders.py', from: 1, count: 60, total: 200 } } }) })
  await app.press('id:show-chat')
  await app.press('id:fold-u1')
  await app.press('id:step-toolu_d')
  await app.waitFor('text:Lines 1–60 of 200')
  expect(app.texts().some((x: string) => x.includes('import os'))).toBe(false)
})

test('a step says on its line that it failed, and an edit how many lines it added and removed', async () => {
  const app = await pane({ chat: JSON.stringify({ chat: true, turns: [{ id: 'u1', from: 'you', said: 'fix the total', end: 'done', reply: 'Done.',
    steps: [{ id: 'toolu_r', kind: 'run', title: 'npm test', failed: true }, { id: 'toolu_e', kind: 'edit', title: 'orders.py', added: 2, deleted: 1 }, { id: 'toolu_k', kind: 'run', title: 'ls' }] }] }) })
  await app.press('id:show-chat')
  await app.press('id:fold-u1')
  await app.waitFor('id:step-toolu_r')
  expect(app.texts().filter((x: string) => x === 'Failed').length).toBe(1)
  expect(app.texts()).toContain('+2 −1')
})

// The files a reply names, under it as cards, when the computer says they are there: a video, a PDF, notes. Notes
// open in the app; a path that isn't there gets no card.
const withFiles = () => JSON.stringify({ chat: true,
  files: { 'renders/demo.mp4': { kind: 'video', size: 18_400_000 }, 'docs/guide.pdf': { kind: 'pdf', size: 2_400_000 }, 'NOTES.md': { kind: 'text', size: 3_100, text: '# Release notes\n\n- the video is 42 s' } },
  turns: [{ id: 'u1', from: 'you', said: 'render it', end: 'done', steps: [],
    reply: 'Rendered `renders/demo.mp4` (42 s). The guide is in docs/guide.pdf, notes in NOTES.md; old.mp4 is gone. See https://example.com/a.pdf.' }] })

test('the files a reply names show under it as cards, by what they are, when the computer has them', async () => {
  const app = await pane({ chat: withFiles() })
  await app.press('id:show-chat')
  await app.waitFor('id:file-renders/demo.mp4')
  expect(app.count('id:file-docs/guide.pdf')).toBe(1)
  expect(app.count('id:file-NOTES.md')).toBe(1)
  expect(app.count('id:file-old.mp4')).toBe(0)
  expect(app.count('id:file-https://example.com/a.pdf')).toBe(0)
  for (const x of ['demo.mp4', 'Video · 18.4 MB', 'guide.pdf', 'PDF · 2.4 MB', 'NOTES.md', 'Text · 3.1 KB']) expect(app.texts()).toContain(x)
})

test('notes a reply names open in the app, as Markdown', async () => {
  const app = await pane({ chat: withFiles() })
  await app.press('id:show-chat')
  await app.press('id:file-NOTES.md')
  await app.waitFor('text:Release notes')
  expect(app.texts()).toContain('the video is 42 s')
  expect(JSON.parse(app.storage()['fake-file-asks'] ?? '[]')).toContain('/work/acme/NOTES.md')
  await app.press('id:file-close')
  await app.waitUntil(() => !app.texts().includes('Release notes'))
})

test('a video a reply names plays in the app, from the computer, by a link only for it', async () => {
  const app = await pane({ chat: withFiles() })
  await app.press('id:show-chat')
  await app.press('id:file-renders/demo.mp4')
  await app.waitFor('id:video-player')
  expect(JSON.parse(app.storage()['fake-file-asks'] ?? '[]')).toContain('link /work/acme/renders/demo.mp4')
  await app.press('id:video-close')
  await app.waitUntil(() => app.count('id:video-player') === 0)
})

// The phone makes a frame of a video from the link (Android MediaMetadataRetriever, iOS AVAssetImageGenerator).
const FRAME = { image: 'data:image/jpeg;base64,/9j/4AAQSkZJRgABAQ', duration: 42_000 }

test('a video a reply names shows a frame of it and how long it is, made on the phone from its link', async () => {
  const app = await pane({ chat: withFiles() }, { 'page.videoFrame': FRAME })
  await app.press('id:show-chat')
  await app.waitFor('id:video-frame-renders/demo.mp4')
  expect(app.texts()).toContain('0:42')
  const call = app.host.calls().find((c: any) => c.name === 'page.videoFrame') as any
  expect(String(call?.data?.url)).toMatch(/\/api\/file\?path=%2Fwork%2Facme%2Frenders%2Fdemo\.mp4&terminal=t1&key=k1$/)
  // A tap still plays it.
  await app.press('id:file-renders/demo.mp4')
  await app.waitFor('id:video-player')
})

test('a video the phone makes no frame of keeps its plain card', async () => {
  const app = await pane({ chat: withFiles() }, { 'page.videoFrame': null })
  await app.press('id:show-chat')
  await app.waitFor('id:file-renders/demo.mp4')
  await app.clock.runFor(500)
  expect(app.count('id:video-frame-renders/demo.mp4')).toBe(0)
  expect(app.texts()).toContain('Video · 18.4 MB')
})

test('a PDF a reply names opens in another app on the phone, from the computer by a link only for it', async () => {
  const app = await pane({ chat: withFiles() }, { 'page.openFile': 'opened' })
  await app.press('id:show-chat')
  await app.press('id:file-docs/guide.pdf')
  await app.waitUntil(() => app.host.calls().some((c: any) => c.name === 'page.openFile'))
  const call = app.host.calls().find((c: any) => c.name === 'page.openFile') as any
  expect([call.data.name, call.data.kind]).toEqual(['guide.pdf', 'pdf'])
  expect(String(call.data.url)).toMatch(/\/api\/file\?path=%2Fwork%2Facme%2Fdocs%2Fguide\.pdf&terminal=t1&key=k1$/)
  expect(JSON.parse(app.storage()['fake-file-asks'] ?? '[]')).toContain('link /work/acme/docs/guide.pdf')
})

test('a file no app on the phone opens, or one that could not be fetched, says so', async () => {
  const none = await pane({ chat: withFiles() }, { 'page.openFile': 'no_app' })
  await none.press('id:show-chat')
  await none.press('id:file-docs/guide.pdf')
  await none.waitFor('text:No app on this phone opens this file.')
  const failed = await pane({ chat: withFiles() }, { 'page.openFile': 'failed' })
  await failed.press('id:show-chat')
  await failed.press('id:file-docs/guide.pdf')
  await failed.waitFor("text:Couldn't get the file from the computer.")
})

test('a message the agent took while it worked shows where it was taken, as yours, marked so; it is not a step to open', async () => {
  const working = [{ id: 'u5', from: 'you', said: 'tidy the menu', end: 'open', reply: '', steps: [
    { id: 'toolu_5', kind: 'run', title: 'npm test' },
    { id: 'y1', kind: 'you', title: 'Also keep the old colors' },
    { id: 'toolu_6', kind: 'edit', title: 'menu.tsx', added: 2, deleted: 1 }] }]
  const app = await pane({ status: 'working', chat: JSON.stringify({ chat: true, turns: working }) })
  await app.press('id:show-chat')
  await app.waitFor('id:you-y1')
  expect(app.texts()).toContain('Also keep the old colors')
  expect(app.texts()).toContain('Added while it was working')
  expect(app.count('id:step-y1')).toBe(0)
  expect(app.count('id:step-toolu_5')).toBe(1)
})

test('once the turn is done and folded, what you said while it worked still shows above the fold line, not twice when opened', async () => {
  const finished = [{ id: 'u6', from: 'you', said: 'tidy the menu', end: 'done', secs: 40, reply: 'The menu is tidy.', steps: [
    { id: 'toolu_7', kind: 'run', title: 'npm test' },
    { id: 'y2', kind: 'you', title: 'Also keep the old colors' },
    { id: 'toolu_8', kind: 'edit', title: 'menu.tsx', added: 2, deleted: 1 }] }]
  const app = await pane({ chat: JSON.stringify({ chat: true, turns: finished }) })
  await app.press('id:show-chat')
  await app.waitFor('text:Worked for <1m · 2 steps')
  expect(app.count('id:you-y2')).toBe(1)
  expect(app.count('id:step-toolu_7')).toBe(0)
  await app.press('id:fold-u6')
  await app.waitFor('id:step-toolu_7')
  expect(app.count('id:you-y2')).toBe(1)
})
