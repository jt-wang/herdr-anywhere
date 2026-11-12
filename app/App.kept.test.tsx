// nsh 0.3.0 frees a live computed's value when its object is collected in a cycle with the value (upstream
// longbridge/nsh 0d0338ac, #1014): the computed's next run frees it again and the engine crashes (seen on iOS 27 in the
// chat view, app/e2e/ios-chat-four.sh). The app keeps each computed's object reachable while its component lives
// (src/ui/kept.ts), so the collector never sees it only in a cycle; and lets it go with the component.
import { expect, open, test } from 'nsh/test'

const kept = async (app: any) => { await app.host.send('fake.kept', {}); await app.clock.runFor(50); return Number(app.storage()['fake-kept'] ?? '-1') }

test('the app keeps its computed values while their screens are up, and lets them go with them', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', argv: 'claude', status: 'done', chat: JSON.stringify({ chat: true, turns: [] }) } }, clock: {} })
  await app.clock.runFor(100)
  await app.waitFor('id:space-sp1')
  const home = await kept(app)
  expect(home).toBeGreaterThan(0)
  for (let i = 0; i < 3; i++) {
    await app.press('id:space-sp1')
    await app.clock.runFor(300)
    // After the first time the pane opens in its chat (the view picked last).
    if (app.count('id:chat-view') === 0) await app.press('id:show-chat')
    await app.waitFor('id:chat-view')
    await app.press('id:back')
    await app.waitFor('id:space-sp1')
    await app.clock.runFor(300)
  }
  expect(await kept(app)).toBe(home)
})
