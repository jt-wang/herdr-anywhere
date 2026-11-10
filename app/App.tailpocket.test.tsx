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
  expect(calls(app, 'tailPocket.scan')).toEqual([{ prefix: 'herdr1:' }])
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
