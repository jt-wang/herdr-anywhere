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
  'tailPocket.setRoutes': null,
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

test('a scan the person cancels says nothing; a phone that cannot scan says to paste', async () => {
  let n = 0
  const scan = () => { throw new Error(n++ === 0 ? 'cancelled' : 'unsupported') }
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.scan': scan }) }, network: { '*': { error: 'offline' } } })
  await app.waitFor('id:pair-scan')
  await app.press('id:pair-scan')
  await app.waitUntil(() => calls(app, 'tailPocket.scan').length === 1)
  await new Promise((r) => setTimeout(r, 200))
  expect(app.shows('text:Scanning failed (cancelled).')).toBe(false)
  await app.press('id:pair-scan')
  await app.waitFor("text:This phone can't scan here. Paste the invite instead.")
  expect(app.shows('id:pair-link')).toBe(true)
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

// Settings › Connection: a pick is told to TailPocket at once, the open tunnel closes and the connection comes back on
// a new one that uses it, and the pick is told before the next launch pairs or connects.
test('Settings › Connection picks the routes TailPocket may use, reconnects at once, and keeps the pick', async () => {
  const more = { 'tailPocket.link': NEW_LINK, 'tailPocket.setRoutes': null, 'tailPocket.disconnect': null, 'tailPocket.path': { route: 'direct', kind: 'lan' } }
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers(more) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(app)
  // Automatic is every route, told as an explicit null at launch.
  expect(calls(app, 'tailPocket.setRoutes')).toEqual([{ routes: null }])
  await app.waitFor('id:settings')
  await app.press('id:settings')
  await app.waitFor('id:connection-state')
  expect(app.texts()).toContain('Automatic')
  await app.press('id:connection')
  await app.waitFor('id:connection-lan')
  expect(app.shows("text:Only when the phone is on the same network as the computer. Away from it, the phone doesn't connect. Nothing goes through Tailcat.")).toBe(true)
  const tunnels = calls(app, 'tailPocket.connect').length
  const closed = calls(app, 'tailPocket.disconnect').length
  await app.press('id:connection-lan')
  await app.waitUntil(() => calls(app, 'tailPocket.setRoutes').length > 1)
  expect(calls(app, 'tailPocket.setRoutes')).toEqual([{ routes: null }, { routes: ['lan'] }])
  // The tunnel opened before the pick closes at once, and a new one opens.
  expect(calls(app, 'tailPocket.disconnect').length).toBe(closed + 1)
  await app.waitUntil(() => calls(app, 'tailPocket.connect').length > tunnels)
  expect(JSON.parse(app.storage()['prefs']).connection).toBe('lan')
  await app.press('id:connection-back')
  await app.waitFor('id:connection-state')
  expect(app.texts()).toContain('Local network only')
  // Back to Automatic: every route again.
  await app.press('id:connection')
  await app.press('id:connection-auto')
  await app.waitUntil(() => calls(app, 'tailPocket.setRoutes').length > 2)
  expect(calls(app, 'tailPocket.setRoutes')[2]).toEqual({ routes: null })

  const again = await open('./App.tsx', { platform: 'ios', storage: { prefs: JSON.stringify({ connection: 'notailcat' }) }, host: { handlers: handlers(more) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(again)
  const order = again.host.calls().filter((c: any) => c.name === 'tailPocket.setRoutes' || c.name === 'tailPocket.pair')
  expect(order.map((c: any) => [c.name, c.data?.routes])).toEqual([['tailPocket.setRoutes', ['lan', 'relay']], ['tailPocket.pair', undefined]])
})

// Before 0.3.6 Settings had a Tailcat only switch: a phone that had it on stays on Tailcat alone.
test('a phone that had Tailcat only on keeps Tailcat alone after the update', async () => {
  const more = { 'tailPocket.link': NEW_LINK, 'tailPocket.setRoutes': null }
  const app = await open('./App.tsx', { platform: 'ios', storage: { prefs: JSON.stringify({ tailcatOnly: true }) }, host: { handlers: handlers(more) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(app)
  expect(calls(app, 'tailPocket.setRoutes')).toEqual([{ routes: ['tailcat'] }])
})

// With routes picked in Settings › Connection, a computer whose link holds none of them is said as such, with a way to
// that setting: TailPocket opens the tunnel either way, and every connection through it ends unanswered. Otherwise
// (Automatic, or a route in common) an unanswered tunnel stays the computer that doesn't answer.
test('no route in common with the computer says so, offers Automatic and opens Settings › Connection', async () => {
  const NO_ROUTE = "text:This phone is set to connect in a way the computer hasn't turned on, so it can't connect."
  const NO_ANSWER = "text:Your computer doesn't answer. Run this on it:"
  // As on Android: each tunnel has its own handle, and a closed one can no longer be asked for its link.
  const tailcatLink = () => {
    let next = 1
    const closed = new Set<number>()
    return {
      'tailPocket.connect': () => ({ handle: next++, port: 40001, baseUrl: 'http://127.0.0.1:40001' }),
      'tailPocket.disconnect': (d: any) => { closed.add(d.handle); return null },
      'tailPocket.link': (d: any) => { if (closed.has(d.handle)) throw new Error('unknown-handle'); return NEW_LINK },
    }
  }
  const lan = await open('./App.tsx', { platform: 'ios', storage: { prefs: JSON.stringify({ connection: 'lan' }) }, host: { handlers: handlers(tailcatLink()) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(lan)
  await lan.waitFor(NO_ROUTE)
  // One status only: no age line while what the screen shows is current.
  expect(lan.texts().some((x) => x.startsWith('Last updated'))).toBe(false)
  // Not reconnecting: nothing gets through until a setting changes, and home and Settings use the same word for it.
  expect(lan.shows('text:Not connected')).toBe(true)
  expect(lan.shows('text:Reconnecting…')).toBe(false)
  await lan.press('id:conn-open-connection')
  await lan.waitFor('id:connection-lan')
  // The Connection page says the same, from the same state.
  expect(lan.count(NO_ROUTE)).toBe(1)
  expect(lan.shows('text:Not connected')).toBe(true)
  expect(lan.shows('text:Reconnecting…')).toBe(false)
  await lan.press('id:connection-back')
  await lan.press('id:conn-use-auto')
  // Every route again: the computer answers or not, but no longer for want of a route.
  await lan.waitFor(NO_ANSWER)
  expect(JSON.parse(lan.storage()['prefs']).connection).toBe('auto')
  expect(calls(lan, 'tailPocket.setRoutes').at(-1)).toEqual({ routes: null })
  expect(lan.count(NO_ROUTE)).toBe(0)

  const auto = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers(tailcatLink()) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(auto)
  await auto.waitFor(NO_ANSWER)
  expect(auto.shows('id:conn-open-connection')).toBe(false)

  const lanLink = { 'tailPocket.link': { ...NEW_LINK, address: undefined, via: [{ t: 'lan', a: ['192.0.2.10:7681'] }] } }
  const common = await open('./App.tsx', { platform: 'ios', storage: { prefs: JSON.stringify({ connection: 'lan' }) }, host: { handlers: handlers(lanLink) }, network: { '*': { error: 'offline' } } })
  await pairAndConnect(common)
  await common.waitFor(NO_ANSWER)
  expect(common.shows('id:conn-open-connection')).toBe(false)
})

// A line down STUCK_MS (store.ts) for no telling reason, as on a plane's or hotel's Wi-Fi before signing in: the banner
// says what may be in the way and offers the sign-in page, a try now, Settings › Connection and pairing again.
test('a tunnel that gets nowhere for a while says the network may be in the way, with what to do', async () => {
  const STUCK = "text:Still can't reach your computer"
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers() }, network: { '*': { error: 'offline' } }, clock: {} })
  await pairAndConnect(app)
  await app.clock.runFor(5_000)
  expect(app.shows(STUCK)).toBe(false)
  await app.clock.runFor(14_000)
  await app.waitFor(STUCK)
  // The banner: one line, the likeliest cause, Try again and Other options; the bar below says the same.
  expect(app.shows('text:The network may be blocking the connection.')).toBe(true)
  for (const id of ['conn-retry', 'conn-options']) expect(`${id} ${app.shows(`id:${id}`)}`).toBe(`${id} true`)
  for (const id of ['conn-sign-in', 'conn-command', 'pair-again']) expect(`${id} ${app.shows(`id:${id}`)}`).toBe(`${id} false`)
  expect(app.shows('text:Not connected · retrying')).toBe(true)
  expect(app.shows('text:Reconnecting…')).toBe(false)
  const tunnels = calls(app, 'tailPocket.connect').length
  await app.press('id:conn-retry')
  expect(app.shows(STUCK)).toBe(false)
  await app.clock.runFor(2_000)
  expect(calls(app, 'tailPocket.connect').length).toBeGreaterThan(tunnels)
  await app.clock.runFor(19_000)
  await app.waitFor(STUCK)
  // Other options: each cause with what to do. A stopped Companion gets no answer through the tunnel too, so its
  // command is there; pairing again says to scan only the computer's own code, and keeps this pairing until then.
  await app.press('id:conn-options')
  await app.waitFor('id:stuck-sheet')
  for (const id of ['conn-sign-in', 'conn-command', 'conn-copy', 'conn-open-connection', 'pair-again']) expect(`${id} ${app.shows(`id:${id}`)}`).toBe(`${id} true`)
  expect(app.shows('text:herdr-anywhere start')).toBe(true)
  expect(app.shows("text:Only scan the QR code shown on your own computer's screen. Your current pairing stays until the new one works.")).toBe(true)
  await app.press('id:pair-again')
  await app.waitFor('id:pair-back')
  await app.press('id:pair-back')
  await app.waitFor(STUCK)
  await app.press('id:conn-options')
  await app.waitFor('id:stuck-sheet')
  await app.press('id:conn-open-connection')
  await app.waitFor('id:connection-lan')
})

// TailPocket's last failed try (v0.3.4 lastFailure) puts its likeliest cause first, in the banner's line and the sheet;
// it isn't authenticated, so the other causes stay, and nothing unpairs.
test('the stuck hint names first the cause TailPocket last saw: refused, Companion; relay-down, the connection setting', async () => {
  const STUCK = "text:Still can't reach your computer"
  const causes = (app: any) => app.texts().map((x: string) => ({
    "This network needs you to sign in": 'network', "Companion isn't running on the computer": 'companion',
    "The connection setting doesn't fit this network": 'route', 'The computer got new keys': 'keys',
  } as Record<string, string>)[x]).filter(Boolean)
  for (const [reason, likely, order] of [
    ['refused', 'Companion may not be running on the computer.', ['companion', 'network', 'route', 'keys']],
    ['handshake-closed', 'Companion may not be running on the computer.', ['companion', 'network', 'route', 'keys']],
    ['relay-down', 'Your relay may not be passing the connection on.', ['route', 'network', 'companion', 'keys']],
    ['no-answer', 'The network may be blocking the connection.', ['network', 'companion', 'route', 'keys']],
  ] as const) {
    const failure = { code: 'unreachable', reason, route: 'relay', routes: [{ t: 'relay', r: reason }], at: 0 }
    const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.lastFailure': failure }) }, network: { '*': { error: 'offline' } }, clock: {} })
    await pairAndConnect(app)
    await app.clock.runFor(19_000)
    await app.waitFor(STUCK)
    expect(`${reason}: ${app.shows(`text:${likely}`)}`).toBe(`${reason}: true`)
    await app.press('id:conn-options')
    await app.waitFor('id:stuck-sheet')
    expect(`${reason}: ${causes(app).join(' ')}`).toBe(`${reason}: ${order.join(' ')}`)
    expect(calls(app, 'tailPocket.forget').length + calls(app, 'tailPocket.deleteLink').length).toBe(0)
    expect(app.shows('id:pair-link')).toBe(false)
  }
})

// That cause is TailPocket's guess, not the computer's word: a phone told Companion isn't answering that pairs again
// with a code that fails keeps the pairing it had, and goes back to it.
test('pairing again from the stuck hint with a code that fails keeps the pairing the phone had', async () => {
  const STUCK = "text:Still can't reach your computer"
  let pairs = 0
  const pair = () => { if (++pairs > 1) throw new Error('unauthorized'); return handlers()['tailPocket.pair'] }
  const failure = { code: 'unreachable', reason: 'handshake-closed', route: 'tailcat', routes: [{ t: 'tailcat', r: 'handshake-closed' }], at: 0 }
  const app = await open('./App.tsx', { platform: 'ios', host: { handlers: handlers({ 'tailPocket.pair': pair, 'tailPocket.lastFailure': failure, 'tailPocket.decodeInvite2': DECODED2 }) }, network: { '*': { error: 'offline' } }, clock: {} })
  await pairAndConnect(app)
  await app.clock.runFor(19_000)
  await app.waitFor(STUCK)
  expect(app.shows('text:Companion may not be running on the computer.')).toBe(true)
  await app.press('id:conn-options')
  await app.waitFor('id:stuck-sheet')
  await app.press('id:pair-again')
  await app.waitFor('id:pair-link')
  await app.type('id:pair-link', INVITE2)
  await app.press('id:pair')
  await app.clock.runFor(500)
  await app.waitFor('text:The computer did not confirm this phone, or the code was already used. Run herdr-anywhere pair again.')
  expect(pairs).toBe(2)
  await app.press('id:pair-back')
  const tunnels = calls(app, 'tailPocket.connect').length
  await app.clock.runFor(20_000)
  await app.waitFor(STUCK)
  expect(calls(app, 'tailPocket.connect').length).toBeGreaterThan(tunnels)
  expect(calls(app, 'tailPocket.connect').every((c: any) => c.slot === 'herdr')).toBe(true)
  expect(calls(app, 'tailPocket.forget').length + calls(app, 'tailPocket.store.deleteLink').length).toBe(0)
})
