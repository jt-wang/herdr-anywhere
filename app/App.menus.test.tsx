// The "…" and long-press menus are the app's own bottom sheet (ui/menu-sheet.tsx), as the other panels, not the
// system's action sheet (iOS 26 and 27 draw that as a box of large pills in the middle, with no Cancel): their
// choices, Cancel and back close them, the dangerous ones still ask again, in every language.
import { expect, open, test } from 'nsh/test'
import { en } from './src/strings/en.ts'

const busy = (o: Record<string, unknown> = {}) => open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' } }, ...o })
const more = async (app: any) => {
  await app.press('id:space-sp1')
  await app.waitFor('id:space-more')
  await app.press('id:space-more')
  await app.waitFor('id:menu')
}

test("the pane's … menu is the app's own sheet: titled with the space, its choices in order, no system dialog", async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await more(app)
  expect(app.dialogs().length).toBe(0)
  expect(app.count('id:menu >> text:api')).toBe(1)
  const ids = ['menu-type', 'menu-split-right', 'menu-split-down', 'menu-close-pane', 'menu-rename-tab', 'menu-rename', 'menu-close', 'menu-settings']
  expect(ids.map((i) => app.count(`id:${i}`))).toEqual(ids.map(() => 1))
  expect(app.count('id:menu-cancel')).toBe(1)
})

test('Cancel and back close a menu without doing anything', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await more(app)
  await app.press('id:menu-cancel')
  await app.waitUntil(() => app.count('id:menu') === 0)
  expect(app.count('id:space-more')).toBe(1)
  await app.press('id:space-more')
  await app.waitFor('id:menu')
  await app.back()
  await app.waitUntil(() => app.count('id:menu') === 0)
  expect(app.count('id:space-more')).toBe(1)
})

test('Close space from the menu still asks again; Cancel there keeps the space', async () => {
  const app = await busy()
  await app.waitFor('text:Needs you')
  await more(app)
  const asked: string[] = []
  app.onDialog((d) => { asked.push(d.title ?? ''); return 'Cancel' })
  await app.press('id:menu-close')
  await app.waitUntil(() => asked.length === 1)
  expect(asked[0]).toBe(en.closeTitle.replace('{name}', 'api'))
  await app.wait(300)
  expect(app.count('id:space-more')).toBe(1)
})

test("an agent's long press: its menu titled with the row's words, Pin to the top pins it", async () => {
  const app = await busy()
  await app.waitFor('id:space-agent-t2')
  await app.longPress('id:space-agent-t2')
  await app.waitFor('id:menu')
  expect(app.dialogs().length).toBe(0)
  expect([app.count('id:menu >> text:Codex'), app.count('id:menu >> text:api')]).toEqual([1, 1])
  await app.press('id:menu-pin')
  await app.waitFor('id:pinned-agent-t2')
  expect(app.count('id:menu')).toBe(0)
})

test("a space's long press: Rename opens the rename sheet", async () => {
  const app = await busy()
  await app.waitFor('id:space-sp1')
  await app.longPress('id:space-sp1')
  await app.waitFor('id:menu')
  expect(app.dialogs().length).toBe(0)
  await app.press('id:menu-rename')
  await app.waitFor('id:rename')
})

for (const locale of ['zh-CN', 'zh-TW', 'es-ES', 'ko-KR', 'ja-JP', 'ru-RU']) {
  test(`${locale}: the menu has no English words`, async () => {
    const app = await busy({ locale })
    await app.waitFor('id:space-sp1')
    await more(app)
    for (const k of ['splitRight', 'splitDown', 'closePane', 'renameTab', 'renameSpace', 'closeSpace', 'settings', 'typeInTerminal', 'cancel'] as const)
      expect([k, app.count(`text:${en[k]}`)]).toEqual([k, 0])
  })
}

test("a wide screen's tab, held: its menu renames it or closes it", async () => {
  const app = await busy({ width: 1000 })
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp1')
  await app.waitFor('id:tab-tb1')
  await app.longPress('id:tab-tb1')
  await app.waitFor('id:menu')
  expect(app.dialogs().length).toBe(0)
  expect([app.count('id:menu-rename'), app.count('id:menu-close')]).toEqual([1, 1])
  await app.press('id:menu-rename')
  await app.waitFor('id:rename')
})

test('the attach menu: Photo, File, and Cancel leaves the input box as it was', async () => {
  const app = await busy()
  await app.press('id:space-sp2')
  await app.waitFor('id:attach')
  await app.press('id:attach')
  await app.waitFor('id:menu')
  expect(app.dialogs().length).toBe(0)
  expect([app.count('id:menu-photo'), app.count('id:menu-file')]).toEqual([1, 1])
  await app.press('id:menu-cancel')
  await app.waitUntil(() => app.count('id:menu') === 0)
  expect(app.count('id:attach')).toBe(1)
})

test('closing a pane from a menu asks again (the … menu and an agent held); Cancel there keeps it', async () => {
  const app = await busy()
  await app.waitFor('id:space-agent-t2')
  const asked: { title?: string | null; message?: string | null }[] = []
  app.onDialog((d) => { asked.push({ title: d.title, message: d.message }); return 'Cancel' })
  await app.longPress('id:space-agent-t2')
  await app.waitFor('id:menu')
  await app.press('id:menu-close')
  await app.waitUntil(() => asked.length === 1)
  expect(asked[0]).toEqual({ title: 'Close Codex?', message: 'Its terminal and the program in it end.' })
  await app.wait(300)
  expect(app.count('id:space-agent-t2')).toBe(1)
  await more(app)
  await app.press('id:menu-close-pane')
  await app.waitUntil(() => asked.length === 2)
  expect(asked[1].message).toBe('Its terminal and the program in it end.')
  // Named as the header above it leads (the tab's name, "fix login"), not by the program in it (Claude Code).
  expect(asked[1].title).toBe('Close fix login?')
  expect(app.count('id:pane-header >> text:fix login')).toBeGreaterThan(0)
  await app.wait(300)
  expect(app.count('id:pane-header')).toBe(1)
})
