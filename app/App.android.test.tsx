// Alerts on Android, over the fake core (fixtures/fake.tsx). Away from the app the system's notification is the alert
// and its sound: the channel and the ringer decide it (MainActivity's channels play Herdr's sounds), so the app plays
// none of its own then. Settings says what Android does with each kind and opens its page in Android's settings.
import { expect, open, test } from 'nsh/test'

const CHANNELS = { blocked: 'alert', done: 'sound', connection: 'silent' }
/** What MainActivity answers for page.notifyChannels; a test changes it as a person would in Android's settings. */
let channels: Record<string, string> = CHANNELS
const android = (handlers: Record<string, unknown> = {}) => {
  channels = CHANNELS
  return open('./fixtures/fake.tsx', {
    platform: 'android',
    host: { params: { scene: 'busy' }, handlers: { 'page.front': true, 'page.notify': true, 'page.notifications': 'granted', 'page.notifyChannels': () => channels, 'page.openChannel': true, ...handlers } },
  })
}
const calls = (app: any, name: string) => app.host.calls().filter((c: any) => c.name === name).map((c: any) => c.data)

test("away from the app, a question posts the notification and plays no sound of the app's own", async () => {
  const app = await android()
  await app.waitFor('text:Needs you')
  await app.host.send('page.front', { front: false })
  await app.host.send('fake.status', { address: 't2', state: 'blocked' })
  await app.waitUntil(() => calls(app, 'page.notify').length === 1)
  await app.wait(300)
  expect(app.count('id:sound-blocked')).toBe(0)
})

test("away from the app, a finished agent posts the notification and plays no sound of the app's own", async () => {
  const app = await android()
  await app.waitFor('text:Needs you')
  await app.host.send('page.front', { front: false })
  await app.host.send('fake.status', { address: 't2', state: 'working' })
  await app.wait(600)
  await app.host.send('fake.status', { address: 't2', state: 'idle' })
  await app.waitUntil(() => calls(app, 'page.notify').length === 1)
  await app.wait(300)
  expect(app.count('id:sound-done')).toBe(0)
})

test("in front, an alert is the toast and Herdr's sound, with no notification", async () => {
  const app = await android()
  await app.waitFor('text:Needs you')
  await app.host.send('fake.status', { address: 't2', state: 'blocked' })
  await app.waitFor('id:sound-blocked')
  expect(calls(app, 'page.notify')).toEqual([])
})

const settings = async (handlers: Record<string, unknown> = {}) => {
  const app = await android(handlers)
  await app.waitFor('id:settings')
  await app.press('id:settings')
  await app.waitFor('id:alert-blocked')
  return app
}

test("Settings says what Android does with each kind, and a tap opens that kind's page in Android's settings", async () => {
  const app = await settings()
  await app.waitFor('label:An agent needs you: Sound and pop-up')
  expect(app.shows('label:An agent finished: Sound')).toBe(true)
  expect(app.shows('label:Stay connected in the background: Silent')).toBe(true)
  await app.press('id:channel-done')
  await app.waitUntil(() => calls(app, 'page.openChannel').length === 1)
  expect(calls(app, 'page.openChannel')[0]).toEqual({ kind: 'done' })
})

test('a kind turned off in Android says so', async () => {
  const app = await settings({ 'page.notifyChannels': { blocked: 'off', done: 'silent', connection: 'off' } })
  await app.waitFor('label:An agent needs you: Off in Android settings')
  expect(app.shows('label:An agent finished: Silent')).toBe(true)
  expect(app.shows('label:Stay connected in the background: Off in Android settings')).toBe(true)
})

test('coming back from Android settings, the lines say what changed there', async () => {
  const app = await settings()
  await app.waitFor('label:An agent finished: Sound')
  channels = { blocked: 'alert', done: 'silent', connection: 'silent' }
  await app.host.send('page.front', { front: false })
  await app.host.send('page.front', { front: true })
  await app.waitFor('label:An agent finished: Silent')
})

test('without channels (an older app, iOS, a browser) there are no lines', async () => {
  const app = await open('./fixtures/fake.tsx', { platform: 'ios', host: { params: { scene: 'busy' } } })
  await app.waitFor('id:settings')
  await app.press('id:settings')
  await app.waitFor('id:alert-blocked')
  await app.wait(300)
  expect(app.count('id:channel-blocked')).toBe(0)
})

test("with the app's notifications off in Android, System notifications opens the app's page there", async () => {
  const app = await settings({ 'page.notifications': 'denied' })
  await app.waitFor('id:notify-settings')
  await app.press('id:notify-settings')
  await app.waitUntil(() => calls(app, 'page.openChannel').length === 1)
  expect(calls(app, 'page.openChannel')[0]).toEqual({ kind: 'app' })
})
