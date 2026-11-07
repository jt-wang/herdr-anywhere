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
  expect(t).toContain('api › fix login')
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
  await app.press('id:space-sp1')
  await app.waitFor('id:answers')
  expect(app.shows('id:header-needs-you')).toBe(true)
  expect(['id:choice-1', 'id:choice-2', 'id:choice-3'].map((id) => app.count(id))).toEqual([1, 1, 1])
  expect(app.count('label:1. Yes')).toBe(1)
  expect(app.count('id:compose-input')).toBe(0)
  await app.press('id:choice-1')
  await app.waitUntil(() => app.storage()['fake-keys'] === JSON.stringify([['t1', ['1']]]))
  // Claude Code goes on working: the composer comes back.
  await app.waitFor('id:compose-input')
  expect(app.count('id:answers')).toBe(0)
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
