// A sub-agent kept as a tab, against a real companion and herdr-core: its row on home then opens that tab, and the
// computer's shared layout holds one tab for it, as a phone that connects afresh shows. Run by e2e/run.sh:
// HM_LIVE_TEST=e2e/live-subkeep.test.ts e2e/run.sh
import { expect, open, test } from 'nsh/test'

test('a sub-agent kept as a tab has one tab, on the phone, in the shared layout and after connecting again', async () => {
  const app = await open('./live-subkeep-app.tsx', { network: 'live' })
  const step = (p: string) => app.texts().find((t) => t.startsWith(p))
  const failed = () => step('FAIL')
  await app.waitUntil(() => !!step('kid: ') || !!failed(), { timeout: 40000 })
  expect(failed()).toBe(undefined)
  const kid = step('kid: ')!.slice(5)
  await app.waitFor(`id:sub-${kid}`, { timeout: 15000 })
  await app.press(`id:sub-${kid}`)
  await app.waitFor('id:peek-keep')
  await app.press('id:peek-keep')
  await app.waitUntil(() => app.count('id:peek-keep') === 0)
  // Back on home, the row again: its tab, or (as before) a peek with Keep, pressed once more.
  if (app.count('id:back')) await app.press('id:back')
  await app.waitFor(`id:sub-${kid}`, { timeout: 15000 })
  await app.press(`id:sub-${kid}`)
  await app.wait(1500)
  // As before: a peek with Keep again, pressed once more.
  const peeked = app.count('id:peek-keep')
  if (peeked) { await app.press('id:peek-keep'); await app.waitUntil(() => app.count('id:peek-keep') === 0) }
  await app.press('id:live-next')
  await app.waitUntil(() => !!step('fresh phone tabs: ') || !!failed(), { timeout: 30000 })
  expect(failed()).toBe(undefined)
  expect([peeked, step('phone tabs: '), step('fresh phone tabs: ')]).toEqual([0, 'phone tabs: 1, layout tabs: 1, label tab is one of them: true', 'fresh phone tabs: 1, running: live'])
  await app.waitUntil(() => !!step('copies left: ') || !!failed(), { timeout: 30000 })
  expect(failed()).toBe(undefined)
  expect(step('copies written: ')).toBe('copies written: 3, version true')
  expect(step('copies left: ')).toBe('copies left: phone 1, layout 1, running: live')
  await app.waitUntil(() => app.texts().includes('done') || !!failed(), { timeout: 15000 })
}, 120000)
