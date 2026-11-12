// Notifications on iPhone, which the app posts itself: Settings says they come only while iOS still lets it run.
import { expect, open, test } from 'nsh/test'

const settings = async (platform: 'ios' | 'android') => {
  const app = await open('./fixtures/fake.tsx', {
    platform,
    host: { params: { scene: 'busy' }, handlers: { 'page.front': true, 'page.notify': true, 'page.notifications': 'granted', 'page.notifyChannels': () => null, 'page.openChannel': true } },
  })
  await app.waitFor('id:settings')
  await app.press('id:settings')
  await app.waitFor('id:alert-blocked')
  return app
}

test('on iPhone, Settings says notifications come only while the app still runs after you leave it', async () => {
  const app = await settings('ios')
  await app.waitFor('id:ios-notify-limit')
  expect(app.shows("text:On iPhone they come only while the app still runs after you leave it, until iOS stops it.")).toBe(true)
})

test('on Android, which keeps the connection, there is no such line', async () => {
  const app = await settings('android')
  await app.wait(300)
  expect(app.count('id:ios-notify-limit')).toBe(0)
})
