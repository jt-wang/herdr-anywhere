// A Codex too old to report its status never alerts, and looks the same as one that is fine: its pane says so.
// Read from the binaries: the 0.163.0 binary holds `]7501;state=` and 0.159.2 holds no `7501;`. A signed-out Codex stops at
// its sign-in screen, where it reports nothing, so 0.163 reporting into herdr-core is not confirmed; the line stays true there.
import { expect, open, test } from 'nsh/test'

const HINT = 'Codex has not reported its status, so it will not alert you. Codex 0.163 or newer reports it.'
const pane = async (params: Record<string, string>) => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', ...params } }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  return app
}

test('a Codex that stays silent is told apart, after it has had time to start', async () => {
  const app = await pane({ argv: 'codex' })
  await app.clock.runFor(4000)
  expect(app.texts()).not.toContain(HINT)
  await app.clock.runFor(1500)
  await app.waitFor(`text:${HINT}`)
})

test('the hint goes as soon as Codex reports', async () => {
  const app = await pane({ argv: 'codex' })
  await app.clock.runFor(5500)
  await app.waitFor(`text:${HINT}`)
  await app.host.send('fake.status', { address: 't1', state: 'idle' })
  await app.waitUntil(() => !app.texts().includes(HINT))
})

test('a Codex that reports has no hint', async () => {
  const app = await pane({ argv: 'codex', status: 'idle' })
  await app.clock.runFor(5500)
  expect(app.texts()).not.toContain(HINT)
})

test('no other program gets it', async () => {
  for (const argv of ['zsh', 'claude', 'opencode']) {
    const app = await pane({ argv })
    await app.clock.runFor(5500)
    expect(app.texts()).not.toContain(HINT)
  }
})
