// The progress page (experimental) over a fake herdr-core whose machine.exec answers `herdr-anywhere track …` and
// `progress …` (fixtures/fake.tsx scene "progress", fixtures/progress-tree.ts: the sample scene's tree).
// Version one is view only: the page shows, the computer decides; the one thing the user picks is the filing agent.
import { expect, open, test } from 'nsh/test'

// These run in Chinese with the Chinese scene; the page in every language is below.
const progress = (locale = 'zh-Hans') => open('./fixtures/fake.tsx', { host: { params: { scene: 'progress' } }, locale, clock: {} })
const ran = (app: any): string[] => JSON.parse(app.storage()['fake-exec'] ?? '[]')

test('the first screen: what needs the user, then each goal over its tasks, then what finished outside a goal', async () => {
  const app = await progress()
  await app.waitFor('text:要你处理')
  // What needs the user: the task, then which goal it is under (the page shows two) and what it asks; its pane opens.
  expect(app.shows('id:needs-task-T6')).toBe(true)
  expect(app.shows('text:上线 Acme 商店的新结账流程 · 价格取整，还是保留到分？')).toBe(true)
  expect(app.shows('id:needs-task-T6-open')).toBe(true)
  expect(app.shows('id:task-T6')).toBe(false) // shown once, in 要你处理
  // Each goal a heading with how many of its tasks are done; under it one row per task, the card work a subheading
  // over its own; what finished today stays where it sits, ticked.
  expect(app.shows('id:top-P1')).toBe(true)
  expect(app.shows('id:top-P3')).toBe(true)
  expect(app.shows('text:1/6')).toBe(true)
  for (const id of ['id:task-T1', 'id:task-T2', 'id:task-T3', 'id:task-T4', 'id:task-T5', 'id:task-T9', 'id:task-T10', 'id:task-T11']) expect(app.shows(id)).toBe(true)
  expect(app.shows('text:排队中')).toBe(true)
  // A task under no goal that finished today comes after the goals; no "just done" list of its own.
  expect(app.shows('id:task-P2')).toBe(true)
  const texts = app.texts()
  expect(texts.indexOf('比较几家支付服务商')).toBeGreaterThan(texts.indexOf('给 Acme 官网换新版'))
  expect(app.shows('text:刚做完')).toBe(false)
  // Only what needs the user has a line under its title; the agent's words are on the task's page.
  expect(app.shows('text:等银行卡的活做完再跑 · 你说过：跑完告诉我')).toBe(false)
  // Nothing to tick, accept, answer or type: the computer keeps the tasks.
  for (const id of ['id:needs-task-T6-accept', 'id:scan-offer', 'id:dock-input', 'text:待确认', 'text:验收']) expect(app.shows(id)).toBe(false)
}, 60000)

// With one goal on the page, a task that needs the user does not name it.
test('with one goal, what needs the user does not name it', async () => {
  const app = await progress()
  await app.waitFor('text:上线 Acme 商店的新结账流程 · 价格取整，还是保留到分？')
  await app.host.send('fake.progress', { only: 'P1' })
  await app.wait(30_000)
  await app.waitFor('text:价格取整，还是保留到分？')
}, 60000)

test('what needs the user opens the pane that asks', async () => {
  const app = await progress()
  await app.press('id:needs-task-T6-open')
  await app.waitFor('id:terminal-t4')
}, 60000)

test('a task opens with the titles above it, its own tasks and its pane, and nothing to change', async () => {
  const app = await progress()
  await app.press('id:task-T1')
  await app.waitFor('id:sub-T3')
  expect(app.shows('text:上线 Acme 商店的新结账流程')).toBe(true)
  expect(app.count('id:task-move') + app.count('id:task-accept') + app.count('id:dock-input')).toBe(0)
  // Its pane, by name and no number, opens that pane, though it went on to other work since.
  expect(app.shows('text:checkout')).toBe(true)
  expect(app.shows('text:checkout · t2')).toBe(false)
  expect(app.texts().some((x) => x.includes('现在在做'))).toBe(false)
  await app.press('id:task-pane-t2')
  await app.waitFor('id:terminal-t2')
  await app.back()
  // A finished task lists no pane.
  await app.waitFor('id:task-P2')
  await app.press('id:task-P2')
  await app.waitFor('id:said-P2')
  expect(app.count('id:task-pane-t3')).toBe(0)
}, 60000)

test('while the computer is still sorting, the page says how many are left', async () => {
  const app = await progress()
  await app.waitFor('text:要你处理')
  expect(app.count('id:queue-line')).toBe(0)
  await app.host.send('fake.progress', { queue: 2 })
  await app.wait(30_000)
  await app.waitFor('text:还有 2 条在整理')
}, 60000)

// The agent list says which task a pane is on, and that is all: it does not open the progress page.
test('in the agent list, a pane on a task says 在做, and nothing more', async () => {
  const app = await progress()
  await app.waitFor('text:要你处理')
  await app.press('id:view-agents')
  await app.waitFor('id:doing-t2')
  expect(app.shows('text:在做：付款后发收据邮件')).toBe(true)
  expect(app.count('id:doing-t3')).toBe(0) // its task is done
  await app.press('id:doing-t2').catch(() => {})
  await app.wait(500)
  expect(app.count('id:progress-back')).toBe(0)
  expect(app.count('id:task-pane-t2')).toBe(0)
}, 60000)

// Goals and the tasks with tasks of their own fold and unfold; the page remembers which are folded.
test('a heading folds what is under it and unfolds it again, and stays folded next time', async () => {
  const app = await progress()
  await app.waitFor('id:task-T3')
  await app.press('id:fold-T1')
  await app.waitUntil(() => app.count('id:task-T3') === 0)
  expect(app.shows('id:task-T1')).toBe(true)
  expect(app.shows('id:task-T9')).toBe(true)
  await app.press('id:fold-P1')
  await app.waitUntil(() => app.count('id:task-T9') === 0)
  expect(app.shows('id:top-P1')).toBe(true)
  expect(JSON.parse(app.storage()['fake-prefs']).progressFolded.sort()).toEqual(['P1', 'T1'])
  await app.press('id:fold-P1')
  await app.waitFor('id:task-T9')
  expect(app.count('id:task-T3')).toBe(0) // T1 is still folded
  const again = await open('./fixtures/fake.tsx', { host: { params: { scene: 'progress', prefs: '{"progressFirst":true,"progressFolded":["T1"]}' } }, locale: 'zh-Hans' })
  await again.waitFor('id:task-T1')
  expect(again.count('id:task-T3')).toBe(0)
}, 60000)

test('turning the page on picks the agent that sorts what you say; off turns filing off', async () => {
  const app = await progress()
  await app.waitFor('text:要你处理')
  await app.press('id:view-agents')
  await app.press('id:settings')
  await app.press('id:progress-first')
  await app.waitUntil(() => ran(app).includes(`progress 'off'`))
  await app.press('id:progress-first')
  await app.waitFor('id:agent-pi')
  expect(app.shows('text:用哪个 agent 整理你说的话')).toBe(true)
  for (const id of ['id:agent-claude-code', 'id:agent-kimi-code', 'id:agent-codex']) expect(app.shows(id)).toBe(true)
  expect(app.shows('text:要 0.163 以上才看得到它在不在忙，这台是 0.150.0')).toBe(true)
  await expect(() => app.press('id:agent-codex')).rejects.toThrow('disabled') // can't be picked
  await app.press('id:agent-pi')
  await app.waitUntil(() => ran(app).includes(`progress 'organizer' '--agent' 'pi' '--lang' 'zh-Hans' && "$b" progress 'on'`))
  await app.waitUntil(() => app.count('id:agent-pi') === 0)
  expect(app.shows('text:首页按任务列出进度 · Pi 在整理')).toBe(true)
}, 60000)

test('a filing pane that has not started shows a card that opens it', async () => {
  const app = await progress()
  await app.waitFor('text:要你处理')
  expect(app.count('id:organizer-waits')).toBe(0)
  await app.host.send('fake.progress', { organizerWaits: 't2' })
  await app.wait(30_000)
  await app.waitFor('id:organizer-waits')
  await app.press('id:organizer-waits-open')
  await app.waitFor('id:terminal-t2')
}, 60000)

// Off (the default), the phone asks the computer nothing of the page: no tree, no 在做 line, nothing turned on.
test('while the page is off the phone runs nothing of it on the computer', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'progress', prefs: '{"progressFirst":false}' } }, locale: 'zh-Hans' })
  await app.waitFor('id:status-line')
  await app.wait(5000) // longer than the page's 4 s reading
  expect(app.count('id:doing-t2')).toBe(0)
  await app.press('id:settings')
  await app.waitFor('text:首页按任务列出进度')
  expect(ran(app)).toEqual([])
}, 60000)

test('with the toggle off (the default) the first screen stays the agent list, with no switch', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'lab' } } })
  await app.waitFor('id:status-line')
  expect(app.shows('text:要你处理')).toBe(false)
  expect(app.count('id:view-switch')).toBe(0)
}, 60000)

test('with the page on, one tap at the top switches between the agent list and the progress page', async () => {
  const app = await progress('en')
  await app.waitFor('text:Needs you')
  expect(app.shows('id:view-switch')).toBe(true)
  for (const label of ['Agents', 'Progress']) expect(app.texts()).toContain(label)
  await app.press('id:view-agents')
  await app.waitFor('id:status-line')
  expect(app.shows('id:view-switch')).toBe(true)
  expect(app.count('id:progress-body')).toBe(0)
  // A switch, not a page on top: back has nothing to go back to.
  expect(app.count('id:progress-back')).toBe(0)
  await app.press('id:view-progress')
  await app.waitFor('id:progress-body')
  expect(app.count('id:status-line')).toBe(0)
  await app.press('id:view-agents')
  await app.waitUntil(() => JSON.parse(app.storage()['fake-prefs'] ?? '{}').homeView === 'agents')
}, 60000)

test('turned off from the progress page, back goes to the agent list, with no switch', async () => {
  const app = await progress('en')
  await app.waitFor('text:Needs you')
  await app.press('id:settings')
  await app.press('id:progress-first')
  await app.waitUntil(() => ran(app).includes(`progress 'off'`))
  await app.back()
  await app.waitFor('id:status-line')
  expect(app.count('id:view-switch')).toBe(0)
  expect(app.count('id:progress-body')).toBe(0)
}, 60000)

test('the app opens on the view the user last switched to', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'progress', prefs: '{"homeView":"agents"}' } }, locale: 'en' })
  await app.waitFor('id:status-line')
  expect(app.shows('id:view-switch')).toBe(true)
  expect(app.count('id:progress-body')).toBe(0)
}, 60000)

test('a task that newly needs the user rings once, saying where it sits and what it waits for', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'progress' }, handlers: { 'page.front': true, 'page.notify': true, 'page.notifications': 'granted' } }, locale: 'zh-Hans', clock: {} })
  await app.waitFor('text:要你处理')
  await app.host.send('page.front', { front: false })
  await app.host.send('fake.progress', { wait: 'T4', waiting: '删卡前要不要再问一次？' })
  const notified = () => app.host.calls().filter((c: any) => c.name === 'page.notify').map((c: any) => c.data)
  await app.wait(60_000)
  await app.waitUntil(() => notified().length > 0, { timeout: 10000 })
  await app.wait(60_000) // one more background reading: no second notification for the same change
  expect(notified().map((n: any) => [n.title, n.body])).toEqual([['上线 Acme 商店的新结账流程 › 让顾客能保存银行卡', '等你：删卡前要不要再问一次？']])
}, 60000)

// It ships as an experimental feature, off by default: the switch says so in every language.
const SWITCH: Record<string, [string, string]> = {
  en: ['Experimental', 'The first screen lists your tasks and how far each has got.'],
  'zh-Hans': ['实验功能', '首页按任务列出进度'], 'zh-Hant': ['實驗功能', '首頁按任務列出進度'],
  es: ['Experimental', 'La primera pantalla muestra tus tareas y cómo va cada una.'],
  ja: ['実験的な機能', '最初の画面にタスクごとの進み具合を表示します。'],
  ko: ['실험 기능', '첫 화면에 작업별 진행 상황을 보여 줘요.'],
  ru: ['Экспериментальное', 'На первом экране — ваши задачи и как продвигается каждая.'],
}
for (const [locale, [section, detail]] of Object.entries(SWITCH)) {
  test(`${locale}: Settings says the progress page is experimental, and it is off`, async () => {
    const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'lab' } }, locale })
    await app.press('id:settings')
    await app.waitFor(`text:${detail}`)
    expect(app.texts()).toContain(section)
    expect(app.storage()['fake-prefs'] ?? '').not.toContain('"progressFirst":true')
  }, 60000)
}

test('in English, picking the agent is in English, the too-old note too', async () => {
  const app = await progress('en')
  await app.waitFor('text:Needs you')
  await app.press('id:view-agents')
  await app.press('id:settings')
  await app.press('id:progress-first')
  await app.press('id:progress-first')
  await app.waitFor('id:agent-pi')
  expect(app.texts()).toContain('Which agent sorts what you say')
  expect(app.texts()).toContain('Needs 0.163 or later to report whether it is busy; this computer has 0.150.0')
  await app.press('id:agent-pi')
  await app.waitFor('text:The first screen lists your tasks and how far each has got. · Pi is sorting')
  // The sorting agent writes in the phone's language.
  expect(ran(app)).toContain(`progress 'organizer' '--agent' 'pi' '--lang' 'en' && "$b" progress 'on'`)
}, 60000)

// Every word of the page in the phone's language; titles and questions are the sorting agent's, written in it too
// (the English scene, fixtures/tree-en.json). Stages come as codes and are worded here.
const PAGE: Record<string, string[]> = {
  en: ['Progress', 'Agents', 'Needs you', 'In progress', 'Doing', 'Open mobile'],
  'zh-Hans': ['进度', 'Agent', '要你处理', '进行中', '在做', '打开 mobile'],
  'zh-Hant': ['進度', 'Agent', '要你處理', '進行中', '在做', '打開 mobile'],
  es: ['Progreso', 'Agentes', 'Te necesita', 'En curso', 'En marcha', 'Abrir mobile'],
  ja: ['進捗', 'エージェント', '対応待ち', '進行中', '作業中', 'mobile を開く'],
  ko: ['진행 상황', '에이전트', '내가 할 일', '진행 중', '하는 중', 'mobile 열기'],
  ru: ['Прогресс', 'Агенты', 'Ждёт вас', 'В работе', 'Делается', 'Открыть mobile'],
}
for (const [locale, words] of Object.entries(PAGE)) {
  test(`${locale}: the page is in the phone's language, and the computer is told it`, async () => {
    const app = await progress(locale)
    await app.waitFor(`text:${words[2]}`)
    for (const w of words) expect(`${w}: ${app.texts().includes(w)}`).toBe(`${w}: true`)
    await app.waitUntil(() => ran(app).includes(`progress 'lang' '${locale}'`))
  }, 60000)
}

test('in English, a task and its panes read in English', async () => {
  const app = await progress('en')
  await app.waitFor('text:Needs you')
  expect(app.texts()).toContain('Ship the new checkout for the Acme shop · Round prices to whole numbers, or keep the cents?')
  await app.press('id:task-T1')
  await app.waitFor('text:Tasks under it')
  expect(app.texts()).toContain('Add the card form')
  expect(app.texts()).toContain('Queued')
  expect(app.texts().some((x) => /[一-龥]/.test(x))).toBe(false)
}, 60000)


// Agents answer in Markdown; the task page shows it as paragraphs and list items, without the marks (a real task
// page showed "- **主打的点：**").
test('what an agent said shows as paragraphs and list items, without Markdown marks', async () => {
  const app = await progress()
  await app.waitFor('id:task-T9')
  await app.host.send('fake.progress', { said: { id: 'T9', text: '表单做好了。**还差**两步：\n\n- 校验卡号\n- 删卡\n\n## 然后\n跑一遍 `checkout` 测试' } })
  await app.wait(30_000)
  await app.press('id:task-T9')
  await app.waitFor('id:said-T9')
  for (const x of ['表单做好了。还差两步：', '校验卡号', '删卡', '然后', '跑一遍 checkout 测试']) expect(app.texts()).toContain(x)
  expect(app.texts().filter((x) => x === '•').length).toBe(2)
  expect(app.texts().some((x) => /\*\*|^- |^## |`/.test(x))).toBe(false)
}, 60000)
