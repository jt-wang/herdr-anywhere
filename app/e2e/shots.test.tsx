// Screenshots of the screens over the fake core, on a real platform: `nsh test --target web e2e/shots.test.tsx`.
import { open, test } from 'nsh/test'

for (const scheme of ['dark', 'light'] as const) {
  test(`home with every space on a server, ${scheme}`, async () => {
    const app = await open('../fixtures/fake.tsx', { host: { params: { scene: 'busy', remote: 'only', prefs: JSON.stringify({ appearance: scheme }) } }, colorScheme: scheme })
    await app.waitFor('text:web')
    await app.wait(1500)
    await app.screenshot(`e2e/shots/remote-home-${scheme}.png`)
  }, 60000)
}

for (const scheme of ['dark', 'light'] as const) {
  test(`home and a space, ${scheme}`, async () => {
    const app = await open('../fixtures/fake.tsx', { host: { params: { scene: 'busy' } }, colorScheme: scheme })
    await app.waitFor('text:Needs you')
    await app.screenshot(`e2e/shots/home-${scheme}.png`)
    await app.press('id:space-sp1')
    await app.waitFor('id:terminal-t1')
    await app.wait(1500)
    await app.screenshot(`e2e/shots/space-${scheme}.png`)
  }, 60000)
}

for (const scheme of ['dark', 'light'] as const) {
  test(`settings and panes side by side on a phone, ${scheme}`, async () => {
    const app = await open('../fixtures/fake.tsx', { host: { params: { scene: 'busy' } }, colorScheme: scheme })
    await app.waitFor('text:Needs you')
    await app.press('id:settings')
    await app.waitFor('id:back')
    await app.wait(500)
    await app.screenshot(`e2e/shots/settings-${scheme}.png`)
    await app.scroll('id:forget', 3000).catch(() => {})
    await app.wait(300)
    await app.screenshot(`e2e/shots/settings-end-${scheme}.png`)
    await app.press('id:back')
    await app.waitFor('text:Needs you')
    await app.press('id:space-sp2')
    await app.waitFor('id:terminal-t3')
    app.onDialog((d) => (d.buttons.includes('Split right') ? 'Split right' : d.buttons.includes('Split anyway') ? 'Split anyway' : null))
    await app.press('id:space-more')
    await app.waitFor('id:close-p-tb3')
    await app.wait(1500)
    await app.screenshot(`e2e/shots/split-${scheme}.png`)
  }, 90000)
}

for (const scheme of ['dark', 'light'] as const) {
  test(`a new version on home and in Settings, ${scheme}`, async () => {
    const info = JSON.stringify({ version: '0.1.6', latest: '0.1.7', canUpdate: true, autoUpdate: false })
    const app = await open('../fixtures/fake.tsx', { host: { params: { scene: 'busy', info } }, colorScheme: scheme })
    await app.waitFor('id:update-banner')
    // The app's own setting: the system's scheme doesn't reach the page under test (measured: identical files).
    await app.press('id:settings')
    await app.waitFor(`id:look-${scheme}`)
    await app.press(`id:look-${scheme}`)
    await app.waitFor('id:auto-update')
    await app.scroll('id:auto-update', 3000).catch(() => {})
    await app.wait(300)
    await app.screenshot(`e2e/shots/update-settings-${scheme}.png`)
    await app.press('id:back')
    await app.waitFor('id:update-banner')
    await app.wait(300)
    await app.screenshot(`e2e/shots/update-home-${scheme}.png`)
  }, 60000)
}

for (const scheme of ['dark', 'light'] as const) {
  test(`the question about checking for new versions, and its rows in Settings, ${scheme}`, async () => {
    const info = JSON.stringify({ version: '0.1.7', latest: '', canUpdate: true, autoUpdate: false, checks: 'ask' })
    const app = await open('../fixtures/fake.tsx', { host: { params: { scene: 'busy', info, opened: '1' } }, colorScheme: scheme })
    await app.waitFor('id:checks-question')
    await app.press('id:settings')
    await app.waitFor(`id:look-${scheme}`)
    await app.press(`id:look-${scheme}`)
    await app.scroll('id:update-check', 3000).catch(() => {})
    await app.wait(300)
    await app.screenshot(`e2e/shots/checks-settings-${scheme}.png`)
    await app.press('id:back')
    await app.waitFor('id:checks-question')
    await app.wait(300)
    await app.screenshot(`e2e/shots/checks-home-${scheme}.png`)
  }, 60000)
}

test('wide: a pane header says what its agent is doing', async () => {
  const app = await open('../fixtures/fake.tsx', { width: 1100, height: 760, host: { params: { scene: 'busy' } } })
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  await app.wait(1500)
  await app.screenshot('e2e/shots/space-wide.png')
}, 60000)

test('pairing', async () => {
  const app = await open('../fixtures/fake.tsx', { host: { params: { scene: 'unpaired' } } })
  await app.waitFor('id:pair-link')
  await app.screenshot('e2e/shots/pair.png')
}, 60000)
