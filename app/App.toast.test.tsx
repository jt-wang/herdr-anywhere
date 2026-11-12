// The toast at the top of the screen: swiped up it goes without doing what it offers, a short pull or a pull down
// leaves it there, and a tap still does what it says. Where it sits is src/ui/toast-top.test.ts.
import { expect, open, test } from 'nsh/test'

const KEEP = 'text:Tap here to keep it for every pane.'
const prefs = (app: any) => JSON.parse(app.storage()['fake-prefs'] ?? '{}')
// Switching a pane to the terminal shows a toast whose tap keeps the terminal for every pane: a toast with an action.
const withToast = async (safeArea = { top: 62, bottom: 34 }, clock?: {}) => {
  const app = await open('./fixtures/fake.tsx', { platform: 'ios', safeArea, host: { params: { scene: 'busy' } }, clock })
  if (clock) await app.clock.runFor(100)
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:terminal-t3')
  await app.press('id:to-terminal')
  await app.waitFor(KEEP)
  return app
}

test('swiped up, the toast goes, and what it offers is not done', async () => {
  const app = await withToast()
  await app.drag(KEEP, { y: -80 })
  await app.waitUntil(() => app.count(KEEP) === 0, { timeout: 1500 })
  expect(prefs(app).typeIn ?? 'composer').toBe('composer')
})

// An iPhone SE has no Dynamic Island: the toast sits 8 pt from the top, so a finger has about 35 pt above the card's
// middle before it leaves the screen. A slow pull of 30 pt (about 600 pt/s, under a flick's speed) has to be enough.
test('on a phone without a Dynamic Island, a slow pull up within the screen takes the toast away', async () => {
  const app = await withToast({ top: 20, bottom: 0 })
  await app.drag(KEEP, { y: -30 })
  await app.waitUntil(() => app.count(KEEP) === 0, { timeout: 1500 })
  expect(prefs(app).typeIn ?? 'composer').toBe('composer')
})

// On the app's own clock: a toast goes by itself 5 s after it shows (store.toast), and on the system's clock a slow
// machine (public CI) took that long over the two pulls, the toast gone before the last check.
test('a short pull up or a pull down leaves the toast where it is', async () => {
  const app = await withToast(undefined, {})
  await app.drag(KEEP, { y: -20 })
  await app.clock.runFor(600)
  expect(app.count(KEEP)).toBe(1)
  await app.drag(KEEP, { y: 60 })
  await app.clock.runFor(600)
  expect(app.count(KEEP)).toBe(1)
  expect(prefs(app).typeIn ?? 'composer').toBe('composer')
})

test('a tap on the toast still does what it says', async () => {
  const app = await withToast()
  await app.press(KEEP)
  await app.waitFor('text:Every pane now opens typing in the terminal')
  expect(prefs(app).typeIn).toBe('terminal')
})
