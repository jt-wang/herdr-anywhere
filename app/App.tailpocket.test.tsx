// On a phone the app pairs through TailPocket, which the app's host project attaches (ios/Sources/App.swift,
// android MainActivity): the invite `herdr-anywhere pair` shows under `serve --tailcat`, scanned or pasted, then the
// tunnel to the computer. Host calls are faked here; e2e/ios-tailcat.sh runs the real thing.
import { expect, open, test } from 'nsh/test'

const INVITE = 'herdr1:eyJ2IjoxLCJhZGRyZXNzIjoiZXhhbXBsZSJ9'
const DECODED = { v: 1, address: 'nodekey:00', port: 1, key: 'k', proof: 'p', expiresAt: 0, name: 'studio' }
const handlers = (more: Record<string, unknown> = {}) => ({
  'tailPocket.decodeInvite': DECODED,
  'tailPocket.pair': { address: 'nodekey:00', port: 1, name: 'studio', key: 'synthetic-key', payload: JSON.stringify({ token: 'synthetic-token', device: 'd1', name: 'studio' }) },
  'tailPocket.connect': { handle: 1, port: 40001, baseUrl: 'http://127.0.0.1:40001' },
  ...more,
})
const calls = (app: any, name: string) => app.host.calls().filter((c: any) => c.name === name).map((c: any) => c.data)
// The v2 invite `herdr-anywhere pair` shows once the computer has a relay or the encrypted local network (TailPocket v0.3).
const INVITE2 = 'herdr2:eJyrVipTsjLSUcrMS0nNVbJSMjQyNjA0MDE1BABqHQcv'
const DECODED2 = { v: 2, name: 'studio', exp: 0, pk: 'host-pk', psk: 'psk', via: [{ t: 'relay', u: 'wss://relay.example.com', r: 'room' }] }
// The saved link as TailPocket gives it back once the tunnel is open: from before v0.3 it has no `pk`.
const OLD_LINK = { address: 'nodekey:00', port: 1, name: 'studio', key: 'synthetic-key' }
const NEW_LINK = { ...OLD_LINK, pk: 'host-pk', via: [{ t: 'tailcat' }] }
/** Pairs from the paste field, then waits for the tunnel. */
const pairAndConnect = async (app: any, invite = INVITE) => {
  await app.waitFor('id:pair-link')
  await app.type('id:pair-link', invite)
  await app.press('id:pair')
  await app.waitUntil(() => calls(app, 'tailPocket.connect').length > 0)
}

test('an invite pasted on a phone pairs through TailPocket, and the app connects through its tunnel', async () => {
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers() }, network: { '*': { error: 'offline' } } })
  await app.waitFor('id:pair-link')
  // What keeps Companion running, also after the computer restarts; serve --tailcat stops with its terminal.
  expect(app.shows('text:herdr-anywhere start')).toBe(true)
  expect(app.shows('text:herdr-anywhere serve --tailcat')).toBe(false)
  await app.type('id:pair-link', INVITE)
  await app.press('id:pair')
  await app.waitUntil(() => calls(app, 'tailPocket.connect').length > 0)
  expect(calls(app, 'tailPocket.decodeInvite')).toEqual([{ text: INVITE, prefix: 'herdr1:' }])
  const [pair] = calls(app, 'tailPocket.pair')
  expect([pair.invite, pair.device, pair.save, pair.slot]).toEqual([DECODED, 'iPhone', true, 'herdr'])
  expect(calls(app, 'tailPocket.connect')[0]).toEqual({ slot: 'herdr' })
  await app.waitUntil(() => !app.shows('id:pair'))
})

test('Scan QR code reads the invite from the camera and pairs with it', async () => {
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.scan': INVITE }) }, network: { '*': { error: 'offline' } } })
  await app.waitFor('id:pair-scan')
  await app.press('id:pair-scan')
  await app.waitUntil(() => calls(app, 'tailPocket.pair').length > 0)
  // v1 and v2 invites: the computer shows herdr2: once it has a relay or the encrypted local network.
  expect(calls(app, 'tailPocket.scan')).toEqual([{ prefix: 'herdr1:', prefix2: 'herdr2:' }])
})

test('an expired invite says so, and the pairing screen stays', async () => {
  const expired = () => { throw new Error('expired') }
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.pair': expired }) }, network: { '*': { error: 'offline' } } })
  await app.waitFor('id:pair-link')
  await app.type('id:pair-link', INVITE)
  await app.press('id:pair')
  await app.waitFor('text:That invite expired. Run herdr-anywhere pair again.')
  expect(app.shows('id:pair')).toBe(true)
})

test('a herdr2: invite is read as v2 with its own prefix, and pairs with every route it holds', async () => {
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.decodeInvite2': DECODED2 }) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(app, INVITE2)
  expect(calls(app, 'tailPocket.decodeInvite2')).toEqual([{ text: INVITE2, prefix: 'herdr2:' }])
  expect(calls(app, 'tailPocket.decodeInvite')).toEqual([])
  const [pair] = calls(app, 'tailPocket.pair')
  expect([pair.invite, pair.device, pair.save, pair.slot]).toEqual([DECODED2, 'iPhone', true, 'herdr'])
  await app.waitUntil(() => !app.shows('id:pair'))
})

test('a scanned herdr2: invite pairs as v2 too', async () => {
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.scan': INVITE2, 'tailPocket.decodeInvite2': DECODED2 }) }, network: { '*': { error: 'offline' } } })
  await app.waitFor('id:pair-scan')
  await app.press('id:pair-scan')
  await app.waitUntil(() => calls(app, 'tailPocket.pair').length > 0)
  expect(calls(app, 'tailPocket.decodeInvite2')).toEqual([{ text: INVITE2, prefix: 'herdr2:' }])
  expect(calls(app, 'tailPocket.pair')[0].invite).toEqual(DECODED2)
})

// A phone paired over Tailcat before v0.3 isn't paired again: once its tunnel is open it registers its end-to-end key
// with the computer (TailPocket's key sync), and from then on uses every route the computer has.
test('a link from before v0.3 (no pk) syncs its key once the tunnel is open', async () => {
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.link': OLD_LINK, 'tailPocket.sync': NEW_LINK }) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(app)
  await app.waitUntil(() => calls(app, 'tailPocket.sync').length > 0)
  expect(calls(app, 'tailPocket.link')[0]).toEqual({ handle: 1 })
  expect(calls(app, 'tailPocket.sync')[0]).toEqual({ handle: 1 })
})

test('a link that has its key already (pk) does not sync', async () => {
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.link': NEW_LINK, 'tailPocket.sync': NEW_LINK }) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(app)
  await app.waitUntil(() => calls(app, 'tailPocket.link').length > 0)
  await app.wait(300)
  expect(calls(app, 'tailPocket.sync')).toEqual([])
})

// A computer not on v0.3 yet has no key sync: the phone goes on over Tailcat. Unanswered (the network is off here), the
// tunnel's address was tried, so the banner says the computer doesn't answer, not that the tunnel failed.
for (const [what, sync] of [
  ['fails', () => { throw new Error('unreachable') }],
  ['never answers', () => new Promise(() => {})],
] as const) {
  test(`a key sync that ${what} leaves the connection to go on over Tailcat`, async () => {
    const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.link': OLD_LINK, 'tailPocket.sync': sync, 'tailPocket.disconnect': null }) }, network: { '*': { error: 'offline' } } })
    await pairAndConnect(app)
    await app.waitFor("text:Your computer doesn't answer. Run this on it:")
    expect(calls(app, 'tailPocket.sync').length).toBeGreaterThan(0)
    expect(app.shows("text:Can't reach the computer through TailPocket. Reconnecting…")).toBe(false)
  })
}

// New pairings never use the companion's unencrypted port: the screen asks for an invite, not a `#pair=` link (one
// from an older computer still pairs, while those last).
test('the pairing screen asks for a herdr1: or herdr2: invite, not a #pair= link', async () => {
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers() }, network: { '*': { error: 'offline' } } })
  await app.waitFor('id:pair-link')
  expect(app.shows('text:herdr1:… / herdr2:…')).toBe(true)
  await app.press('id:pair')
  await app.waitFor('id:pair-error')
  expect(app.texts()).toContain('Scan the QR code that herdr-anywhere pair shows, or paste its herdr1: or herdr2: invite.')
  expect(app.texts().some((t) => t.includes('#pair='))).toBe(false)
})

// TailPocket v0.3's emoji check: the phone shows the four emoji the computer asks its user to compare, and pairs once
// the computer's user confirms there.
const PAIRED = { address: 'nodekey:00', port: 1, name: 'studio', key: 'synthetic-key', pk: 'host-pk', payload: JSON.stringify({ token: 'synthetic-token', device: 'd1', name: 'studio' }) }
// (The host fake cannot hold TailPocket's pair call open, so this pairs through the fixture's tunnel.)
for (const approve of [true, false]) {
  test(`pairing shows the emoji the computer asks about, and ${approve ? 'pairs once it confirms' : 'says to start again on a no'}`, async () => {
    const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'unpaired', tunnel: 'on', emoji: '🐶 🌵 🔑 🚲' } } })
    await app.waitFor('id:pair-link')
    expect(app.shows('id:pair-emoji')).toBe(false)
    await app.type('id:pair-link', INVITE2)
    await app.press('id:pair')
    await app.waitFor('id:pair-emoji')
    expect(app.shows('text:🐶 🌵 🔑 🚲')).toBe(true)
    expect(app.shows('text:Check that your computer shows these same four emoji, then confirm there.')).toBe(true)
    await app.host.send('fake.confirm', { approve })
    if (approve) {
      await app.waitUntil(() => !app.shows('id:pair-link'))
    } else {
      await app.waitFor('text:The computer did not confirm this phone, or the code was already used. Run herdr-anywhere pair again.')
      expect(app.shows('id:pair-emoji')).toBe(false)
    }
  })
}

test('a pairing the computer did not confirm says to start again', async () => {
  const refused = () => { throw new Error('unauthorized') }
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.decodeInvite2': DECODED2, 'tailPocket.pair': refused }) }, network: { '*': { error: 'offline' } } })
  await app.waitFor('id:pair-link')
  await app.type('id:pair-link', INVITE2)
  await app.press('id:pair')
  await app.waitFor('text:The computer did not confirm this phone, or the code was already used. Run herdr-anywhere pair again.')
  expect(app.shows('id:pair-emoji')).toBe(false)
  expect(app.shows('id:pair')).toBe(true)
})

// Settings › Computer › Tailcat only: TailPocket is told, and a phone that has it on is told before it pairs or connects.
test('Tailcat only in Settings is told to TailPocket, and kept for the next launch', async () => {
  const more = { 'tailPocket.link': NEW_LINK, 'tailPocket.setTailcatOnly': null, 'tailPocket.path': { route: 'direct' } }
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers(more) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(app)
  expect(calls(app, 'tailPocket.setTailcatOnly')).toEqual([])
  await app.waitFor('id:settings')
  await app.press('id:settings')
  await app.waitFor('id:tailcat-only')
  await app.press('id:tailcat-only')
  await app.waitUntil(() => calls(app, 'tailPocket.setTailcatOnly').length > 0)
  expect(calls(app, 'tailPocket.setTailcatOnly')).toEqual([{ on: true }])
  expect(JSON.parse(app.storage()['prefs']).tailcatOnly).toBe(true)

  const again = await open('./App.tsx', { platform: 'ios', storage: { prefs: JSON.stringify({ tailcatOnly: true }) }, host: { handlers: handlers(more) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(again)
  const order = again.host.calls().map((c: any) => c.name).filter((n: string) => n === 'tailPocket.setTailcatOnly' || n === 'tailPocket.pair')
  expect(order).toEqual(['tailPocket.setTailcatOnly', 'tailPocket.pair'])
})
