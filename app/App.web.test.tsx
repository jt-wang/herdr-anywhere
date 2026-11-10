// The web app reads its page's address from the page's host (web-host.js): the app itself runs in the runtime and has
// no `location`. Opening a `#pair=` link pairs by itself and takes the code out of the address; on a page of the
// computer, the code alone is enough.
import { expect, open, test } from 'nsh/test'

const PAGE = 'http://192.0.2.10:7681'
const paired = { status: 200, body: { token: 'synthetic-token', device: 'd1', name: 'studio' } }

test('opened with a pairing link, the web app pairs by itself and drops the code from the address', async () => {
  const app = await open('./App.tsx', {
    platform: 'web',
    network: { [`POST ${PAGE}/api/pair`]: paired, '*': { error: 'offline' } },
    host: { handlers: { 'page.address': { href: `${PAGE}/#pair=GOOD-CODE`, origin: PAGE }, 'page.dropPair': null } },
  })
  await app.waitUntil(() => app.requests().some((r) => r.url === `${PAGE}/api/pair`))
  expect(JSON.parse(app.requests().find((r) => r.url === `${PAGE}/api/pair`)!.body!).code).toBe('GOOD-CODE')
  await app.waitUntil(() => app.host.calls().some((c) => c.name === 'page.dropPair'))
  await app.waitUntil(() => !app.shows('id:pair'))
})

test('on a page of the computer, the code alone pairs with that computer', async () => {
  const app = await open('./App.tsx', {
    platform: 'web',
    network: { [`POST ${PAGE}/api/pair`]: paired, '*': { error: 'offline' } },
    host: { handlers: { 'page.address': { href: `${PAGE}/`, origin: PAGE }, 'page.dropPair': null } },
  })
  await app.waitFor('id:pair-link')
  // start --lan keeps the LAN serve running, also after a restart; serve --lan stops with its terminal.
  expect(app.shows('text:herdr-anywhere start --lan')).toBe(true)
  expect(app.shows('text:herdr-anywhere serve --lan')).toBe(false)
  await app.type('id:pair-link', 'GOOD-CODE')
  await app.press('id:pair')
  await app.waitUntil(() => app.requests().some((r) => r.url === `${PAGE}/api/pair`))
  expect(app.host.calls().some((c) => c.name === 'page.dropPair')).toBe(false)
})

// Alerts in a browser, over the fake core (fixtures/fake.tsx): the page says whether it is in front and posts the
// system's notification (web-host.js).
const busyPage = (handlers: Record<string, unknown> = {}) => open('./fixtures/fake.tsx', {
  platform: 'web',
  host: { params: { scene: 'busy' }, handlers: { 'page.front': true, 'page.notify': true, 'page.notifications': 'granted', ...handlers } },
})
const notified = (app: any) => app.host.calls().filter((c: any) => c.name === 'page.notify').map((c: any) => c.data)

test('in another tab, an agent that needs you posts the system notification, and tapping it opens the agent', async () => {
  const app = await busyPage()
  await app.waitFor('text:Needs you')
  await app.host.send('page.front', { front: false })
  await app.host.send('fake.status', { address: 't2', state: 'blocked' })
  await app.waitUntil(() => notified(app).length === 1)
  const n = notified(app)[0]
  expect([n.title, n.terminal, n.kind]).toEqual(['Codex needs you', 't2', 'blocked'])
  await app.waitFor('id:sound-blocked')
  await app.host.send('page.front', { front: true })
  await app.host.send('page.open', { terminal: 't2' })
  await app.waitFor('id:terminal-t2')
})

test('an agent that finished posts the quieter kind of notification', async () => {
  const app = await busyPage()
  await app.waitFor('text:Needs you')
  await app.host.send('page.front', { front: false })
  await app.host.send('fake.status', { address: 't2', state: 'working' })
  await app.wait(600)
  await app.host.send('fake.status', { address: 't2', state: 'idle' })
  await app.waitUntil(() => notified(app).length === 1)
  expect([notified(app)[0].title, notified(app)[0].kind]).toEqual(['Codex finished', 'done'])
})

test('a page in front shows the toast and posts no notification', async () => {
  const app = await busyPage()
  await app.waitFor('text:Needs you')
  await app.host.send('fake.status', { address: 't2', state: 'blocked' })
  await app.waitFor('text:Codex needs you')
  await app.wait(600)
  expect(notified(app)).toEqual([])
})

test('settings ask the browser to allow notifications', async () => {
  const app = await busyPage({ 'page.notifications': 'default', 'page.allowNotifications': 'granted' })
  await app.waitFor('text:Needs you')
  await app.press('id:settings')
  await app.waitFor('id:notify-allow')
  await app.press('id:notify-allow')
  await app.waitFor('text:On while Herdr Anywhere is in the background.')
  expect(app.host.calls().some((c) => c.name === 'page.allowNotifications')).toBe(true)
})
