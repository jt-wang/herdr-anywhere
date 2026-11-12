// The screens over a fake herdr-core: what the desktop's sidebar, tab bar and panes show, and what tapping does.
import { expect, open, test } from 'nsh/test'

const busy = () => open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' } } })

test('a row of choices too long for the phone fades at its end until it is scrolled there, so no button reads as cut off', async () => {
  const app = await busy()
  await app.waitFor('id:needs-t1-choice-2')
  await app.waitFor('id:needs-t1-choices-more')
  await app.scroll('id:needs-t1-choice-1', 2000)
  await app.waitUntil(() => app.count('id:needs-t1-choices-more') === 0)
  const wide = await open('./fixtures/fake.tsx', { width: 1000, host: { params: { scene: 'busy' } } })
  await wide.waitFor('id:needs-t1-choice-2')
  await wide.wait(100)
  expect(wide.count('id:needs-t1-choices-more')).toBe(0)
})

test('home puts Needs you first, oldest wait first: where each agent is, what it asks, its choices and Open; then the spaces and their agents', async () => {
  const app = await busy()
  await app.waitFor('id:needs-t1')
  await app.wait(1000)
  await app.host.send('fake.status', { address: 't2', state: 'blocked', kind: 'permission', msg: 'approve Bash: npm test' })
  await app.waitFor('id:needs-t2')
  const t = app.texts()
  expect(t[t.indexOf('Needs you') + 1]).toBe('2')
  // The longest wait first, across spaces.
  expect(t.indexOf('Write: /Users/sam/src/api/login.ts') < t.indexOf('Bash: npm test')).toBe(true)
  // Each row names its agent as home's rows do: the tab's name first, its space under it.
  expect([app.count('id:needs-t1 >> text:fix login'), app.count('id:needs-t1 >> text:api')]).toEqual([1, 1])
  // Claude Code's own menu, read off its screen, the highlighted option first; Open on every row.
  expect(['id:needs-t1-choice-1', 'id:needs-t1-choice-2', 'id:needs-t1-choice-3', 'id:needs-t1-open', 'id:needs-t2-open'].map((id) => app.count(id))).toEqual([1, 1, 1, 1, 1])
  expect(app.count('label:1. Yes')).toBe(1)
  // Codex's menu can't be read: no guessed answers.
  expect(app.count('id:needs-t2-choice-1')).toBe(0)
  // Then the spaces, each with its agents and their status words; a shell is not an agent.
  expect(t.indexOf('Bash: npm test') < t.indexOf('Spaces')).toBe(true)
  expect(app.count('id:space-agent-t1') + app.count('id:space-agent-t2')).toBe(2)
  expect(app.count('id:space-agent-t3')).toBe(0)
  // A detail that only repeats the program's name is left out.
  expect(app.count('label:Codex, api, Needs you')).toBe(1)
  for (const s of ['api', 'web', 'This computer', '2 agents · 2 need you']) expect(t).toContain(s)
})

test('a choice on a home row answers there; Open, or a choice that wants text, opens the pane', async () => {
  const app = await busy()
  await app.waitFor('id:needs-t1-choice-1')
  await app.press('id:needs-t1-choice-1')
  await app.waitUntil(() => app.storage()['fake-keys'] === JSON.stringify([['t1', ['1']]]))
  // Claude Code goes on working: it leaves the queue.
  await app.waitUntil(() => app.count('id:needs-t1') === 0)

  const typed = await busy()
  await typed.waitFor('id:needs-t1-choice-3')
  await typed.press('id:needs-t1-choice-3')
  await typed.waitFor('id:pane-header')
  expect(typed.storage()['fake-keys']).toBe(JSON.stringify([['t1', ['3']]]))
  // The pane opens on its composer (its focus is checked on a device: a headless run can't see it).
  await typed.waitFor('id:compose-input')

  const opened = await busy()
  await opened.waitFor('id:needs-t1-open')
  await opened.press('id:needs-t1-open')
  await opened.waitFor('id:answers')
})

test('a lost line says so on home and in the pane, with how old what you see is', async () => {
  const app = await busy()
  await app.waitFor('id:needs-t1')
  expect(app.count('id:conn-banner')).toBe(0)
  await app.host.send('fake.drop', {})
  await app.waitFor('id:conn-banner')
  expect(app.texts()).toContain('Lost the computer. Reconnecting…')
  expect(app.texts()).toContain('Last updated just now')
  await app.press('id:space-sp2')
  await app.waitFor('id:pane-header')
  expect(app.count('id:conn-banner')).toBe(1)
})

test('on a phone a space is its pane: a two-line header, the terminal, one composer row; no tab strip, frame or status bar', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:terminal-t3')
  expect(app.shows('id:pane-header')).toBe(true)
  expect(app.shows('text:web › Terminal')).toBe(true)
  for (const id of ['id:composer', 'id:compose-input', 'id:key-esc', 'id:attach']) expect(app.shows(id)).toBe(true)
  for (const id of ['id:tab-tb3', 'id:cwd-p-tb3', 'id:space-status', 'id:answers']) expect(app.count(id)).toBe(0)
})

test("the title opens the switcher: this space's tabs, the other spaces' agents, and new agents", async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:terminal-t3')
  await app.press('id:switcher')
  await app.waitFor('id:switch-tab-tb3')
  for (const id of ['id:switch-agent-t1', 'id:switch-agent-t2', 'id:switch-new-claude', 'id:switch-new-shell']) expect(app.shows(id)).toBe(true)
  await app.press('id:switch-agent-t1')
  await app.waitFor('id:terminal-t1')
  await app.press('id:switcher')
  await app.waitFor('id:switch-tab-tb2')
  await app.press('id:switch-tab-tb2')
  await app.waitFor('id:terminal-t2')
  expect(app.shows('id:terminal-t1')).toBe(false)
})

test('the switcher tells two Claude Codes apart as home does: the tab name or title, the status word, what each is doing, its sub-agents', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'twins' } } })
  await app.waitFor('id:space-agent-t1')
  const home = app.texts()
  await app.press('id:space-sp1')
  await app.waitFor('id:pane-header')
  await app.press('id:switcher')
  await app.waitFor('id:switch-tab-tb2')
  // Each agent's row reads as its row on home: the program and what tells it apart, its status in words, its activity.
  const rows: [string, string[]][] = [
    ['id:switch-tab-tb1', ['Claude Code', 'fix login', 'Working', 'Running the login tests']],
    ['id:switch-tab-tb2', ['Claude Code', 'Billing refactor', 'Idle', 'Moved invoices to the new table']],
    ['id:switch-agent-t4', ['web', 'Claude Code', 'Checkout page', 'Working', 'Editing cart.tsx']],
  ]
  for (const [row, texts] of rows) for (const x of texts) expect([row, x, app.count(`${row} >> text:${x}`)]).toEqual([row, x, 1])
  for (const x of ['fix login', 'Running the login tests', 'Moved invoices to the new table', 'test runner']) expect(home).toContain(x)
  // Its sub-agent under it, and the shell tab as before.
  expect(app.shows('id:switch-sub-t5')).toBe(true)
  expect(app.count('id:switch-tab-tb3 >> text:Terminal')).toBe(1)
  // A row still opens its tab.
  await app.press('id:switch-tab-tb2')
  await app.waitFor('id:terminal-t2')
})

test('a sub-agent kept as a tab opens that tab from its row on home, with no peek and no second Keep', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'twins' } } })
  await app.waitFor('id:sub-t5')
  await app.press('id:sub-t5')
  await app.waitFor('id:peek-keep')
  await app.press('id:peek-keep')
  await app.waitUntil(() => app.count('id:peek-keep') === 0)
  // Still listed under its parent, as on the desktop.
  await app.waitFor('id:sub-t5')
  await app.press('id:sub-t5')
  await app.waitFor('id:terminal-t5')
  await app.wait(300)
  expect(app.count('id:peek-keep')).toBe(0)
  await app.press('id:switcher')
  await app.waitFor('id:switch-tab-tb1')
  expect(app.texts().filter((x) => x === 'test runner').length).toBe(1)
})

// What a row's text looks like, against texts on the same screen in the colours it should take (a headless run draws
// with NSH's own palette, where sidebar-foreground is foreground: the sizes tell the name from the title).
const look = (app: any, sel: string) => app.spanStyle(sel)
const FOREGROUND = 'text:Herdr'
const MUTED = 'id:status-line >> text:This computer'

test("an agent's row leads with its title; the program's name is small before it, what it is doing one step quieter, an idle row dimmer", async () => {
  for (const scheme of ['dark', 'light'] as const) {
    const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'twins' } }, colorScheme: scheme })
    await app.waitFor('id:space-agent-t1')
    const fg = look(app, FOREGROUND).color, muted = look(app, MUTED).color
    const title = look(app, 'id:space-agent-t1 >> text:fix login')
    const name = look(app, 'id:space-agent-t1 >> text:Claude Code')
    expect([scheme, title.color, title.fontSize, title.fontWeight]).toEqual([scheme, fg, 14.5, 500])
    expect([scheme, name.fontSize, name.fontWeight]).toEqual([scheme, 12.5, 400])
    // What it is doing: the app's foreground at about 80 % (its own colours, not the palette), smaller and regular.
    const doing = look(app, 'id:space-agent-t1 >> text:Running the login tests')
    expect([scheme, doing.color, doing.fontSize, doing.fontWeight]).toEqual([scheme, scheme === 'dark' ? '#c4c5c7ff' : '#3f3f42ff', 13, 400])
    // Idle: the title and what it did one step down; its status word readable in light, as before in dark.
    expect([scheme, look(app, 'id:space-agent-t2 >> text:Billing refactor').color]).toEqual([scheme, muted])
    expect([scheme, look(app, 'id:space-agent-t2 >> text:Moved invoices to the new table').color]).toEqual([scheme, muted])
    const idle = look(app, 'id:space-agent-t2 >> text:Idle').color
    expect([scheme, idle === muted]).toEqual([scheme, scheme === 'light'])
    // The switcher's rows are the same row.
    await app.press('id:space-sp1')
    await app.press('id:switcher')
    await app.waitFor('id:switch-tab-tb1')
    expect([scheme, look(app, 'id:switch-tab-tb1 >> text:fix login').fontSize, look(app, 'id:switch-tab-tb1 >> text:Claude Code').fontSize]).toEqual([scheme, 14.5, 12.5])
  }
})

test("with Show agent names off, rows on home, Pinned and the switcher drop the program's name; a row with no title keeps it", async () => {
  const app = await busy()
  await app.waitFor('id:space-agent-t2')
  await app.host.send('fake.label', { address: 't1', labels: { 'herdr.pinned': '1' } })
  await app.waitFor('id:pinned-agent-t1')
  expect(app.count('id:pinned-agent-t1 >> text:Claude Code')).toBe(1)
  await app.press('id:settings')
  await app.waitFor('id:show-agent-names')
  await app.press('id:show-agent-names')
  await app.press('id:back')
  await app.waitFor('id:pinned-agent-t1')
  expect(app.count('id:pinned-agent-t1 >> text:Claude Code')).toBe(0)
  expect(app.count('id:pinned-agent-t1 >> text:fix login')).toBe(1)
  // Codex has no title here: its name is what the row says.
  expect(look(app, 'id:space-agent-t2 >> text:Codex').fontSize).toBe(14.5)
  await app.press('id:space-sp1')
  await app.press('id:switcher')
  await app.waitFor('id:switch-tab-tb1')
  expect(app.count('id:switch-tab-tb1 >> text:Claude Code')).toBe(0)
  expect(app.count('id:switch-tab-tb1 >> text:fix login')).toBe(1)
  expect(app.count('id:switch-tab-tb2 >> text:Codex')).toBe(1)
  // Kept on the phone: the next start reads it.
  const again = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', prefs: JSON.stringify({ showAgentNames: false }) } } })
  await again.waitFor('id:space-agent-t2')
  await again.press('id:space-sp1')
  await again.press('id:switcher')
  await again.waitFor('id:switch-tab-tb1')
  expect(again.count('id:switch-tab-tb1 >> text:Claude Code')).toBe(0)
})

test('text in the composer goes to the pane in one go', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:compose-input')
  await app.type('id:compose-input', 'npm test')
  await app.press('id:compose-send')
  await app.waitUntil(() => app.storage()['fake-sent'] === JSON.stringify([['t3', 'npm test', true]]))
  expect(app.value('id:compose-input')).toBe('')
})

test('tapping an agent opens its space at its pane', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('label:Claude Code, api · fix login, Needs you')
  await app.waitFor('id:terminal-t1')
})

test('the first time it connects, the app asks to post notifications; only once', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' }, handlers: { 'page.notifications': 'default', 'page.allowNotifications': 'granted' } } })
  await app.waitFor('text:Needs you')
  await app.waitUntil(() => app.host.calls().some((c) => c.name === 'page.allowNotifications'))
  await app.press('id:space-sp2')
  await app.waitFor('id:terminal-t3')
  await app.press('id:back')
  await app.waitFor('text:Needs you')
  expect(app.host.calls().filter((c) => c.name === 'page.allowNotifications').length).toBe(1)
})

test('a wide screen shows what each agent is doing in its pane header', async () => {
  const app = await open('./fixtures/fake.tsx', { width: 1000, host: { params: { scene: 'busy' } } })
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:terminal-t3')
  await app.host.send('fake.status', { address: 't3', state: 'blocked', kind: 'permission', msg: 'approve Bash: npm test' })
  await app.waitUntil(() => app.texts().some((t) => t.includes('Bash: npm test')))
})

test('a split on a phone asks first, then shows both panes; one at a time is a menu away', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:terminal-t3')

  // Declining the warning leaves one pane.
  const asked: string[] = []
  app.onDialog((d) => {
    if (d.buttons.includes('Split right')) return 'Split right'
    if (d.buttons.includes('Split anyway')) { asked.push(d.title ?? ''); return 'Cancel' }
    return null
  })
  await app.press('id:space-more')
  await app.waitUntil(() => asked.length === 1)
  expect(app.count('id:pane-header')).toBe(1)
  expect(app.shows('id:terminal-t3')).toBe(true)
  expect(app.shows('id:split-right-p-tb3')).toBe(false)

  // Going ahead puts both on the screen, each with its own header, and the row under them stays.
  app.onDialog((d) => (d.buttons.includes('Split right') ? 'Split right' : d.buttons.includes('Split anyway') ? 'Split anyway' : null))
  await app.press('id:space-more')
  await app.waitFor('id:close-p-tb3')
  expect(app.shows('id:terminal-t3')).toBe(true)
  expect(app.count('id:compose-input')).toBe(1)

  // One at a time: the new pane has focus, and the switcher goes back to the first.
  app.onDialog((d) => (d.buttons.includes('Show one pane') ? 'Show one pane' : null))
  await app.press('id:space-more')
  await app.waitUntil(() => !app.shows('id:terminal-t3'))
  await app.press('id:switcher')
  await app.waitFor('id:switch-pane-p-tb3')
  await app.press('id:switch-pane-p-tb3')
  await app.waitFor('id:terminal-t3')

  // Both again: the same warning, and it can be declined.
  app.onDialog((d) => (d.buttons.includes('Show all panes') ? 'Show all panes' : d.buttons.includes('Split anyway') ? 'Split anyway' : null))
  await app.press('id:space-more')
  await app.waitFor('id:close-p-tb3')

  // Closing the other pane leaves one, drawn the phone's way.
  await app.press('id:close-p-tb3')
  await app.waitUntil(() => !app.shows('id:close-p-tb3'))
})

test('going to another screen puts the keyboard away: Settings is not left under it', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:terminal-t3')
  const before = Number(app.storage()['keyboard-away'] ?? 0)
  app.onDialog((d) => (d.buttons.includes('Settings') ? 'Settings' : null))
  await app.press('id:space-more')
  await app.waitFor('text:Appearance')
  await app.waitUntil(() => Number(app.storage()['keyboard-away'] ?? 0) === before + 1)
})

test('+ on home starts a terminal in a new space and opens it', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'empty' } } })
  await app.waitFor('text:Start a terminal or an agent')
  await app.press('id:new')
  await app.waitFor('id:folder-use')
  await app.press('id:folder-use')
  await app.waitFor('id:new-terminal')
  await app.press('id:new-terminal')
  await app.waitFor('id:terminal-t1')
})

test('+ offers the agents installed on the computer and starts one with its task', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'empty' } } })
  await app.waitFor('text:Start a terminal or an agent')
  await app.press('id:new')
  await app.press('id:folder-use')
  await app.waitFor('id:new-claude')
  expect(app.shows('id:new-codex')).toBe(true)
  await app.press('id:new-claude')
  await app.type('id:task', 'fix the login bug')
  await app.press('id:start')
  await app.waitFor('id:terminal-t1')
})

// New space and New tab open on the folder picker (fixtures/fake.tsx answers /api/folders over a made-up home); the
// program comes second, under the folder chosen.
test('New space opens on the folder browser: into src, into demo-app, Use this folder, and the space starts there', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'empty' } } })
  await app.waitFor('text:Start a terminal or an agent')
  await app.press('id:new')
  await app.waitFor('id:folder-src')
  expect(app.shows('id:folder-search')).toBe(true)
  expect(app.count('id:new-terminal')).toBe(0)
  await app.press('id:folder-src')
  await app.waitFor('id:folder-demo-app')
  expect(app.texts()).toContain('git')
  await app.press('id:folder-demo-app')
  await app.waitFor('id:crumb-2')
  expect(app.shows('id:crumbs >> text:demo-app')).toBe(true)
  await app.press('id:folder-use')
  await app.waitFor('id:new-terminal')
  expect(app.shows('id:folder-chosen >> text:~/src/demo-app')).toBe(true)
  await app.press('id:new-terminal')
  await app.waitFor('id:terminal-t1')
  await app.waitFor('text:demo-app › Terminal')
})

test('a space that is already open adds an agent from its header, in its own folder, as a new tab', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:compose-input')
  await app.press('id:add-agent')
  await app.waitFor('id:new-claude')
  expect(app.count('id:folder-search')).toBe(0)
  expect(app.shows('text:Add an agent')).toBe(true)
  await app.press('id:folder-chosen')
  await app.waitFor('id:folder-search')
  await app.press('id:folder-use')
  await app.waitFor('id:new-claude')
  await app.press('id:new-claude')
  await app.waitFor('id:start')
  await app.press('id:start')
  await app.waitUntil(() => app.count('id:start') === 0)
  await app.press('id:switcher')
  await app.waitFor('id:switch-new-folder')
  expect(app.texts().filter((x) => x === 'Terminal').length).toBe(2)
})

test('home adds an agent to a space from its row, and opens that space', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:add-agent-sp2')
  await app.waitFor('id:new-claude')
  expect(app.count('id:folder-search')).toBe(0)
  expect(app.shows('text:Add an agent')).toBe(true)
  await app.press('id:new-claude')
  await app.press('id:start')
  await app.waitFor('id:compose-input')
  await app.press('id:switcher')
  await app.waitFor('id:switch-new-folder')
  expect(app.texts().filter((x) => x === 'Terminal').length).toBe(2)
})

test('the search filters Recent and the folders, the × clears it, a typed path browses, and up goes to the parent', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:new')
  await app.waitFor('id:recent-0')
  expect(app.shows('id:recent-0 >> text:api')).toBe(true)
  expect(app.shows('id:recent-0 >> text:~/src/api')).toBe(true)
  await app.type('id:folder-search', 'web')
  await app.waitUntil(() => app.count('id:recent-1') === 0)
  expect(app.shows('id:recent-0 >> text:web')).toBe(true)
  expect(app.count('id:folder-src')).toBe(0)
  await app.press('id:folder-clear')
  await app.waitFor('id:folder-src')
  await app.type('id:folder-search', '~/src/de')
  await app.waitFor('id:folder-demo-app')
  expect(app.count('id:folder-web')).toBe(0)
  expect(app.count('id:recent-0')).toBe(0)
  await app.press('id:folder-clear')
  await app.press('id:folder-up')
  await app.waitFor('id:folder-Documents')
  expect(app.count('id:folder-up')).toBe(0)
})

test('Show hidden lists the dot folders too', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'empty' } } })
  await app.waitFor('text:Start a terminal or an agent')
  await app.press('id:new')
  await app.waitFor('id:folder-src')
  expect(app.count('id:folder-.config')).toBe(0)
  await app.press('id:show-hidden')
  await app.waitFor('id:folder-.config')
})

test("a typed folder that doesn't exist gets a card: the close name, Create folder and Cancel; only Create makes it", async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'empty' } } })
  await app.waitFor('text:Start a terminal or an agent')
  await app.press('id:new')
  await app.waitFor('id:folder-search')
  await app.type('id:folder-search', '~/src/apii')
  await app.waitFor("text:~/src/apii doesn't exist")
  await app.waitFor('text:Did you mean ~/src/api?')
  expect(app.texts()).toContain('Create “apii” in ~/src?')
  expect(app.count('id:folder-use')).toBe(0)
  await app.press('id:folder-suggest-0')
  await app.waitUntil(() => !app.shows("text:~/src/apii doesn't exist"))
  await app.waitFor('id:folder-use-typed')
  await app.press('id:folder-clear')
  await app.type('id:folder-search', '~/src/apii')
  await app.waitFor('id:folder-cancel')
  await app.press('id:folder-cancel')
  await app.waitUntil(() => !app.shows("text:~/src/apii doesn't exist"))
  expect(app.storage()['fake-created']).toBe(undefined)
  await app.type('id:folder-search', '~/src/apii')
  await app.waitFor('id:folder-create')
  await app.press('id:folder-create')
  await app.waitFor('id:new-terminal')
  expect(app.storage()['fake-created']).toBe(JSON.stringify(['/Users/sam/src/apii']))
  expect(app.shows('id:folder-chosen >> text:~/src/apii')).toBe(true)
})

test("a new tab starts its picker in the space's folder, and the folder line goes back to it", async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp1')
  await app.waitFor('id:switcher')
  await app.press('id:switcher')
  await app.waitFor('id:switch-new-folder')
  await app.press('id:switch-new-folder')
  await app.waitFor('id:crumb-2')
  expect(app.shows('id:crumbs >> text:api')).toBe(true)
  await app.press('id:folder-use')
  await app.waitFor('id:folder-chosen')
  expect(app.shows('id:folder-chosen >> text:~/src/api')).toBe(true)
  await app.press('id:folder-chosen')
  await app.waitFor('id:folder-search')
})

test('renaming a space from its menu changes it everywhere', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:space-more')
  app.onDialog((d) => (d.buttons.includes('Rename space') ? 'Rename space' : null))
  await app.press('id:space-more')
  await app.waitFor('id:rename')
  await app.clear('id:rename')
  await app.type('id:rename', 'frontend')
  await app.press('id:rename-save')
  await app.waitFor('text:frontend › Terminal')
})

test('a computer that forgot this phone shows pairing again, and a good code pairs', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'unpaired' } } })
  await app.waitFor('text:This computer no longer knows this phone. Pair it again.')
  await app.type('id:pair-link', 'http://192.0.2.10:7681/#pair=BAD-CODE')
  await app.press('id:pair')
  await app.waitFor('text:That code is not right. Check it, or run herdr-anywhere pair again.')
  await app.clear('id:pair-link')
  await app.type('id:pair-link', 'http://192.0.2.10:7681/#pair=GOOD-CODE')
  await app.press('id:pair')
  await app.waitFor('text:Start a terminal or an agent')
})

// After the computer restarted, Companion wasn't running, and the phone said the computer might be asleep or on
// another network. Through TailPocket the phone can't tell Companion stopped from this phone taken off the computer
// (link.test.ts), so it says the likely one with what to run, and offers pairing again.
test('a computer that does not answer through the tunnel says to start Companion there, and offers pairing again', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'companion-off', via: 'tailpocket' } } })
  await app.waitFor("text:Your computer doesn't answer. Run this on it:")
  expect(app.shows('text:herdr-anywhere start')).toBe(true)
  expect(app.shows('text:asleep')).toBe(false)
  // Only what the person acts on: the reason, the command, the button.
  expect(app.texts().some((t) => /reconnects by itself|no longer know/.test(t))).toBe(false)
  await app.press('id:pair-again')
  await app.waitFor('id:pair-link')
  await app.press('id:pair-back')
  await app.waitFor('id:conn-banner')
  await app.press('id:pair-again')
  await app.waitFor('id:pair-link')
  await app.type('id:pair-link', 'herdr1:eyJ2IjoxfQ')
  await app.press('id:pair')
  await app.waitFor('text:Start a terminal or an agent')
  expect(app.shows('id:conn-banner')).toBe(false)
})

// A phone paired over the LAN said Companion wasn't running, with nothing to run and no way to pair again; and
// `start` (Tailcat only) wouldn't bring it back.
test('a LAN-paired phone whose computer refuses says to run start --lan there, and offers pairing again', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'companion-refused', url: 'http://192.168.1.20:7681' } } })
  await app.waitFor("text:Can't reach Companion on your computer. Run this on it:")
  expect(app.shows('text:herdr-anywhere start --lan')).toBe(true)
  expect(app.shows('text:herdr-anywhere start')).toBe(false)
  expect(app.texts().some((t) => /terminal open/.test(t))).toBe(false)
  // Unless it can be told apart, the words don't say the computer answered.
  expect(app.texts().some((t) => /answers/.test(t))).toBe(false)
  await app.press('id:pair-again')
  await app.waitFor('id:pair-back')
  await app.press('id:pair-back')
  await app.waitFor('id:conn-banner')
})

test('a phone that reaches the computer by a name says that name, since how serve listens there is unknown', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'companion-refused', url: 'http://mac.example:7681' } } })
  await app.waitFor("text:Can't reach Companion at mac.example:7681. Start it there the way you set it up.")
  expect(app.shows('id:conn-command')).toBe(false)
  expect(app.shows('id:pair-again')).toBe(true)
})

test('opened with a pairing link, the app pairs by itself, then drops the link from the address', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'link', link: 'http://192.0.2.10:7681/#pair=GOOD-CODE' } } })
  await app.waitFor('text:Start a terminal or an agent')
  await app.waitUntil(() => app.storage()['pair-link-used'] === '1')
})

test('a used pairing link is not tried again when the computer later forgets the phone', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'link-forgotten', link: 'http://192.0.2.10:7681/#pair=GOOD-CODE' } } })
  await app.waitFor('text:This computer no longer knows this phone. Pair it again.')
  await app.wait(300)
  expect(app.value('id:pair-link')).toBe('')
  expect(app.shows('text:This computer no longer knows this phone. Pair it again.')).toBe(true)
})

test('opened again with the link it paired by, a phone the computer forgets is not paired again by itself', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'relaunch-forgotten', link: 'http://192.0.2.10:7681/#pair=GOOD-CODE' } } })
  await app.waitFor('text:This computer no longer knows this phone. Pair it again.')
  await app.wait(300)
  expect(app.value('id:pair-link')).toBe('')
  expect(app.shows('text:This computer no longer knows this phone. Pair it again.')).toBe(true)
})

test('settings change the terminal text size and keep it', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:settings')
  await app.waitFor('text:13 pt')
  await app.press('id:font-larger')
  await app.waitFor('text:14 pt')
  await app.press('id:back')
  await app.waitFor('text:Needs you')
  await app.press('id:settings')
  await app.waitFor('text:14 pt')
})

test("a blocked pane says Needs you, and its composer row is the agent's own choices off its screen", async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  const red = app.spanStyle('text:Needs you >> nth=0').color
  await app.press('id:space-sp1')
  await app.waitFor('id:answers')
  expect(app.shows('id:header-needs-you')).toBe(true)
  // A red pill at the start of line 2, apart from the grey words after it (no "·" between), so line 1 is the title alone.
  expect(app.count('id:header-needs-you >> text:Needs you')).toBe(1)
  expect(app.spanStyle('id:header-needs-you >> text:Needs you').color).toBe(red)
  const head = app.texts().slice(0, app.texts().indexOf('Connecting…') >= 0 ? app.texts().indexOf('Connecting…') : undefined)
  expect(head.slice(head.indexOf('Needs you'), head.indexOf('Needs you') + 2)).toEqual(['Needs you', 'Claude Code'])
  expect(['id:choice-1', 'id:choice-2', 'id:choice-3'].map((id) => app.count(id))).toEqual([1, 1, 1])
  expect(app.count('label:1. Yes')).toBe(1)
  expect(app.count('id:compose-input')).toBe(0)
  await app.press('id:choice-1')
  await app.waitUntil(() => app.storage()['fake-keys'] === JSON.stringify([['t1', ['1']]]))
  // Claude Code goes on working: the composer comes back.
  await app.waitFor('id:compose-input')
  expect(app.count('id:answers')).toBe(0)
})

// herdr-core (0.1.20) holds a send while the terminal has a half-typed line, up to 60 s after its last key.
const holding = () => open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', slowSendMs: '300' } } })

test('a send herdr-core holds stays in the box, can not go twice, says why after a few seconds, and leaves once it is there', async () => {
  const app = await holding()
  await app.waitFor('text:Needs you')
  await app.host.send('fake.hold', { method: 'terminal.send' })
  await app.press('id:space-sp2')
  await app.waitFor('id:compose-input')
  await app.type('id:compose-input', 'npm test')
  // Three taps at once, as a thumb tapping again on a send that seems stuck.
  await Promise.allSettled([app.press('id:compose-send'), app.press('id:compose-send'), app.press('id:compose-send')])
  await app.waitFor('id:compose-sending')
  expect(app.count('id:compose-send')).toBe(0)
  await expect(() => app.press('id:compose-sending')).rejects.toThrow('no pressable view')
  expect(app.value('id:compose-input')).toBe('npm test')
  await app.waitFor('id:send-held')
  expect(app.texts()).toContain('The terminal has a half-typed line, so this waits until that line is sent or cleared. Tap >_ to type in the terminal and send or delete it; left alone, this goes on the end of it in a while.')
  expect(app.storage()['fake-sent']).toBe(JSON.stringify([['t3', 'npm test', true]]))
  await app.host.send('fake.release', {})
  await app.waitFor('id:compose-input')
  await app.waitUntil(() => app.value('id:compose-input') === '')
  expect(app.count('id:send-held')).toBe(0)
  expect(app.count('id:compose-sending')).toBe(0)
  expect(app.storage()['fake-sent']).toBe(JSON.stringify([['t3', 'npm test', true]]))
})

test('a held send the line drops says it did not go, and its text is still in the box', async () => {
  const app = await holding()
  await app.waitFor('text:Needs you')
  await app.host.send('fake.hold', { method: 'terminal.send' })
  await app.press('id:space-sp2')
  await app.waitFor('id:compose-input')
  await app.type('id:compose-input', 'npm test')
  await app.press('id:compose-send')
  await app.waitFor('id:send-held')
  await app.host.send('fake.drop', {})
  await app.waitFor("text:The connection dropped while the computer was waiting, so this wasn't sent. It's still in the input box: send it again.")
  expect(app.value('id:compose-input')).toBe('npm test')
  expect(app.count('id:compose-sending')).toBe(0)
})

test('a choice that wants text picks it, then the composer takes the text to the agent', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp1')
  await app.waitFor('id:choice-3')
  await app.press('id:choice-3')
  await app.waitFor('id:compose-input')
  expect(app.storage()['fake-keys']).toBe(JSON.stringify([['t1', ['3']]]))
  await app.type('id:compose-input', 'use a temp file')
  await app.press('id:compose-send')
  await app.waitUntil(() => app.storage()['fake-sent'] === JSON.stringify([['t1', 'use a temp file', true]]))
})

test("a blocked agent whose menu can't be read gets the keys, and says so; never a guessed answer", async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.host.send('fake.status', { address: 't2', state: 'blocked' })
  await app.press('id:space-sp1')
  await app.press('id:switcher')
  await app.press('id:switch-tab-tb2')
  await app.waitFor('id:keys-fallback')
  expect(app.count('id:answers')).toBe(0)
  for (const id of ['id:key-up', 'id:key-down', 'id:key-enter', 'id:key-esc']) expect(app.shows(id)).toBe(true)
  await app.press('id:key-enter')
  await app.waitUntil(() => app.storage()['fake-keys'] === JSON.stringify([['t2', ['enter']]]))
})

test("an agent that needs you plays Herdr's request sound, and one that finished plays the done sound", async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  expect(app.count('id:sound-blocked') + app.count('id:sound-done')).toBe(0)
  await app.host.send('fake.status', { address: 't2', state: 'blocked' })
  await app.waitFor('id:sound-blocked')
  await app.host.send('fake.status', { address: 't2', state: 'idle' })
  await app.waitFor('id:sound-done')
})

test('an alert turned off in settings plays no sound', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:settings')
  await app.press('id:alert-blocked')
  await app.press('id:back')
  await app.host.send('fake.status', { address: 't2', state: 'blocked' })
  await app.wait(1200)
  expect(app.count('id:sound-blocked')).toBe(0)
})

const NEW_COMPUTER = JSON.stringify({ version: '0.1.5', latest: '9.0.0', canUpdate: true, autoUpdate: false })

test('a new version shows on home, a tap asks the computer, and what it answers shows', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', info: NEW_COMPUTER } } })
  await app.waitFor('id:update-banner')
  await app.waitFor('text:Version 9.0.0 is available')
  await app.press('id:update-now')
  await app.waitFor('text:the test computer says no')
  await app.waitFor("text:Couldn't update the computer")
  expect(app.storage()['update-posts']).toBe('1')
  await app.waitFor('id:update-banner')
})

test('"Not now" keeps that version off home, and Settings still offers it', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', info: NEW_COMPUTER } } })
  await app.waitFor('id:update-banner')
  await app.press('id:update-later')
  await app.waitUntil(() => app.count('id:update-banner') === 0)
  await app.press('id:settings')
  await app.waitFor('id:settings-update')
})

test('nothing is offered when everything is current', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', info: JSON.stringify({ version: '0.0.1', latest: '0.0.1', canUpdate: true }) } } })
  await app.waitFor('text:Needs you')
  await app.press('id:settings')
  await app.waitFor('id:auto-update')
  expect(app.count('id:settings-update')).toBe(0)
  expect(app.count('id:update-banner')).toBe(0)
})

test('automatic updates are off until asked, and the computer is told', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', info: NEW_COMPUTER } } })
  await app.waitFor('text:Needs you')
  await app.press('id:settings')
  await app.press('id:auto-update')
  await app.waitUntil(() => app.storage()['update-auto'] === '{"on":true}')
})

const ASKING = JSON.stringify({ version: '0.1.7', latest: '', canUpdate: true, autoUpdate: false, checks: 'ask' })

test('whether the computer may check for new versions is asked the second time the app opens, and yes tells it', async () => {
  const first = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', info: ASKING } } })
  await first.waitFor('text:Needs you')
  await first.wait(500)
  expect(first.count('id:checks-question')).toBe(0)
  const second = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', info: ASKING, opened: '1' } } })
  await second.waitFor('id:checks-question')
  await second.waitFor('text:Check for new versions?')
  await second.press('id:checks-yes')
  await second.waitUntil(() => second.storage()['update-checks'] === '{"on":true}')
  await second.waitUntil(() => second.count('id:checks-question') === 0)
})

test('no to the question is an answer too: it is not asked again', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', info: ASKING, opened: '3' } } })
  await app.press('id:checks-no')
  await app.waitUntil(() => app.storage()['update-checks'] === '{"on":false}')
  await app.waitUntil(() => app.count('id:checks-question') === 0)
})

test('Settings: daily checks, Check now, and installing by itself only while checks are on', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', info: JSON.stringify({ version: '0.1.7', latest: '', canUpdate: true, autoUpdate: false, checks: 'off' }) } } })
  await app.waitFor('text:Needs you')
  await app.press('id:settings')
  await app.waitFor('id:update-checks')
  expect(app.count('id:auto-update')).toBe(0)
  await app.press('id:update-check')
  await app.waitFor('id:settings-update')
  expect(app.storage()['update-check-posts']).toBe('1')
  await app.press('id:update-checks')
  await app.waitUntil(() => app.storage()['update-checks'] === '{"on":true}')
  await app.waitFor('id:auto-update')
})

test('the switcher goes on working after it started an agent', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:terminal-t3')
  for (const key of ['claude', 'shell', 'claude']) {
    await app.press('id:switcher')
    await app.waitFor(`id:switch-new-${key}`)
    await app.press(`id:switch-new-${key}`)
    await app.waitUntil(() => app.count(`id:switch-new-${key}`) === 0)
    await app.wait(300)
  }
})

test('an agent pinned on the desktop shows under Pinned on top, and leaves its space; unpinned there, it goes back', async () => {
  const app = await busy()
  await app.waitFor('id:space-agent-t2')
  expect(app.count('text:Pinned')).toBe(0)
  await app.host.send('fake.label', { address: 't2', labels: { 'herdr.pinned': '1' } })
  await app.waitFor('id:pinned-agent-t2')
  expect(app.count('id:space-agent-t2')).toBe(0)
  const t = app.texts()
  expect(t.indexOf('Pinned') >= 0 && t.indexOf('Pinned') < t.indexOf('Spaces')).toBe(true)
  // Without a space above it, the row names its space, as the desktop's Pinned rows do.
  expect(app.count('label:Codex, api, Working')).toBe(1)
  await app.host.send('fake.label', { address: 't2', labels: { 'herdr.pinned': '' } })
  await app.waitFor('id:space-agent-t2')
  expect(app.count('id:pinned-agent-t2')).toBe(0)
  expect(app.count('text:Pinned')).toBe(0)
})

test('pinning from an agent\'s menu pins it on the computer; Unpin from the Pinned row puts it back', async () => {
  const app = await busy()
  await app.waitFor('id:space-agent-t2')
  app.onDialog((d) => (d.buttons.includes('Pin to the top') ? 'Pin to the top' : null))
  await app.longPress('id:space-agent-t2')
  await app.waitFor('id:pinned-agent-t2')
  expect(app.count('id:space-agent-t2')).toBe(0)
  app.onDialog((d) => (d.buttons.includes('Unpin') ? 'Unpin' : null))
  await app.longPress('id:pinned-agent-t2')
  await app.waitFor('id:space-agent-t2')
  expect(app.count('text:Pinned')).toBe(0)
})

test("an agent's menu is titled with its row's main text, its space and program under it", async () => {
  const shown: { title?: string | null; message?: string | null }[] = []
  const menuOf = async (app: any, row: string) => {
    app.onDialog((d: any) => { shown.push({ title: d.title, message: d.message }); return 'Cancel' })
    await app.longPress(row)
    await app.waitUntil(() => shown.length > 0)
    return shown.pop()
  }
  const twins = await open('./fixtures/fake.tsx', { host: { params: { scene: 'twins' } } })
  await twins.waitFor('id:space-agent-t1')
  expect(await menuOf(twins, 'id:space-agent-t1')).toEqual({ title: 'fix login', message: 'api · Claude Code' })
  // Pinned: the same words as its row there.
  await twins.host.send('fake.label', { address: 't4', labels: { 'herdr.pinned': '1' } })
  await twins.waitFor('id:pinned-agent-t4')
  expect(await menuOf(twins, 'id:pinned-agent-t4')).toEqual({ title: 'Checkout page', message: 'web · Claude Code' })
  // With Show agent names off the program's name is left out, as on the row.
  const off = await open('./fixtures/fake.tsx', { host: { params: { scene: 'twins', prefs: JSON.stringify({ showAgentNames: false }) } } })
  await off.waitFor('id:space-agent-t1')
  expect(await menuOf(off, 'id:space-agent-t1')).toEqual({ title: 'fix login', message: 'api' })
  // No title: the program's name is the main text, and is not said twice.
  const app = await busy()
  await app.waitFor('id:space-agent-t2')
  expect(await menuOf(app, 'id:space-agent-t2')).toEqual({ title: 'Codex', message: 'api' })
})

test("a pane's header leads with what home's row does: the tab name or title, the program's name small under it unless Settings hides it", async () => {
  const twins = (prefs?: object) => open('./fixtures/fake.tsx', { host: { params: { scene: 'twins', ...(prefs ? { prefs: JSON.stringify(prefs) } : {}) } } })
  const app = await twins()
  await app.waitFor('id:space-agent-t1')
  await app.press('id:space-sp1')
  await app.waitFor('id:pane-header')
  const head = (x: string) => app.count(`id:switcher >> text:${x}`)
  expect([head('fix login'), head('Claude Code')]).toEqual([1, 1])
  // The title is line 1, alone, so a narrow phone has room for it; the name starts line 2, small and grey, then what
  // it is doing.
  expect(app.spanStyle('id:switcher >> text:fix login').fontSize).toBe(14.5)
  // The title sits in a chip, as a tab does, so it reads as this pane's name (and "Needs you" below as its state).
  expect(app.count('id:pane-title >> text:fix login')).toBe(1)
  expect([app.count('id:pane-program'), app.count('id:pane-header >> text:Claude Code')]).toEqual([1, 1])
  expect(app.spanStyle('id:pane-header >> text:Claude Code').fontSize).toBe(11.5)
  expect(app.count('id:pane-header >> text:Running the login tests')).toBe(1)
  expect(app.count('id:header-needs-you')).toBe(0)
  // Show agent names off: the label goes, the title stays.
  const off = await twins({ showAgentNames: false })
  await off.waitFor('id:space-agent-t1')
  await off.press('id:space-sp1')
  await off.waitFor('id:pane-header')
  expect([off.count('id:switcher >> text:fix login'), off.count('id:switcher >> text:Claude Code')]).toEqual([1, 0])

  // No title: the program's name is the main text, said once.
  const busyApp = await busy()
  await busyApp.waitFor('id:space-agent-t2')
  await busyApp.press('id:space-agent-t2')
  await busyApp.waitFor('id:pane-header')
  expect(busyApp.count('id:switcher >> text:Codex')).toBe(1)
  expect(busyApp.spanStyle('id:switcher >> text:Codex').fontSize).toBe(14.5)
})

test("home's Needs you row and a wide pane's header name an agent as its row does", async () => {
  const app = await busy()
  await app.waitFor('id:needs-t1')
  // The tab's name leads, the program's name small before it; under it the space, without saying the name again.
  expect([app.count('id:needs-t1 >> text:fix login'), app.count('id:needs-t1 >> text:Claude Code'), app.count('id:needs-t1 >> text:api')]).toEqual([1, 1, 1])
  expect(app.spanStyle('id:needs-t1 >> text:Claude Code').fontSize).toBe(12.5)
  const off = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', prefs: JSON.stringify({ showAgentNames: false }) } } })
  await off.waitFor('id:needs-t1')
  expect([off.count('id:needs-t1 >> text:fix login'), off.count('id:needs-t1 >> text:Claude Code')]).toEqual([1, 0])

  const wide = await open('./fixtures/fake.tsx', { width: 1000, host: { params: { scene: 'busy' } } })
  await wide.waitFor('id:space-sp1')
  await wide.press('id:space-sp1')
  await wide.waitFor('id:pane-name-p-tb1')
  expect([wide.count('id:pane-name-p-tb1 >> text:fix login'), wide.count('id:pane-name-p-tb1 >> text:Claude Code')]).toEqual([1, 1])
})

test('a change redraws the home screen working out the agents list once, however many rows show it', async () => {
  // Each row worked it out again for itself, over every terminal: with 28 agents one redraw took 150 ms on the Android
  // emulator, and taps waited behind it.
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'many' } } })
  await app.waitFor('id:space-agent-t24')
  await app.wait(1500)
  const before = Number(app.storage()['fake-views'] ?? 0)
  await app.host.send('fake.status', { address: 't7', state: 'blocked', kind: 'permission', msg: 'approve Bash: npm test' })
  await app.waitFor('id:needs-t7')
  await app.wait(300)
  expect(Number(app.storage()['fake-views']) - before).toBeLessThan(4)
})
