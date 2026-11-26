// Pairing refused, the line coming back after the companion restarts, and pairing again after a revoke, with the
// app's own Linker and Store against a real companion and herdr-core. Run by e2e/run.sh:
// HM_LIVE_TEST=e2e/live-reconnect.test.ts e2e/run.sh
import { expect, open, test } from 'nsh/test'

test('a wrong or expired code is refused, a restarted companion takes the same pairing back, and a revoked phone pairs again', async () => {
  const app = await open('./live-reconnect-app.tsx', { network: 'live' })
  await app.waitUntil(() => app.texts().some((t) => t === 'done' || t.startsWith('FAIL')), { timeout: 110000 })
  expect(app.texts()).toEqual([
    'wrong code: That code is not right. Check it, or run herdr-anywhere pair again.',
    'connected: ready',
    'expired code: That code expired or was already used. Run herdr-anywhere pair again.',
    'while away: refused, said true',
    'back: ready, same pairing true',
    'after restart: after-42',
    'revoked: unpaired, This computer no longer knows this phone. Pair it again.',
    'paired again: ready, new token true, space kept',
    'old token: unpaired',
    'done',
  ])
}, 120000)
