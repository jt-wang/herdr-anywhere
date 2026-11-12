// The input box against a real companion and herdr-core, in a terminal with a half-typed line (herdr-core holds a
// send until that line is sent or cleared): three taps send one message, the box keeps it and says why until it
// arrives; a held send whose line drops says it did not go and is not sent later; sent again and left alone, it
// waits out herdr-core's draft wait on a line that stays up. Run by e2e/run.sh:
// HM_LIVE_TEST=e2e/live-draft.test.ts e2e/run.sh
import { expect, open, test } from 'nsh/test'

test('a send held by a half-typed line goes once, stays in the box until it arrives, and one whose line drops never goes', async () => {
  const app = await open('./live-draft-app.tsx', { network: 'live' })
  const step = (p: string) => app.texts().find((t) => t.startsWith(p))
  const failed = () => step('FAIL')
  await app.waitUntil(() => !!step('space: ') || !!failed(), { timeout: 40000 })
  expect(failed()).toBe(undefined)
  await app.press(`id:space-${step('space: ')!.slice(7)}`)
  await app.waitFor('id:compose-input', { timeout: 15000 })
  await app.type('id:compose-input', 'echo phone-1')
  await Promise.allSettled([app.press('id:compose-send'), app.press('id:compose-send'), app.press('id:compose-send')])
  await app.waitFor('id:compose-sending')
  expect(app.value('id:compose-input')).toBe('echo phone-1')
  await app.waitFor('id:send-held', { timeout: 15000 })
  expect(app.texts()).toContain('The terminal has a half-typed line, so this waits until that line is sent or cleared. Tap >_ to type in the terminal and send or delete it; left alone, this goes on the end of it in a while.')
  expect(app.value('id:compose-input')).toBe('echo phone-1')
  await app.waitUntil(() => !!step('arrived: ') || !!failed(), { timeout: 30000 })
  expect(step('arrived: ')).toBe('arrived: 1')
  expect(app.value('id:compose-input')).toBe('')
  expect(app.count('id:send-held')).toBe(0)

  await app.type('id:compose-input', 'echo phone-2')
  await app.press('id:compose-send')
  await app.waitUntil(() => !!step('after the drop: ') || !!failed(), { timeout: 90000 })
  expect(failed()).toBe(undefined)
  expect(step('after the drop: ')!.split(',')[0]).toBe('after the drop: 0 arrived')
  expect(step('phone says: ')).toBe("phone says: Couldn't send / The connection dropped while the computer was waiting, so this wasn't sent. It's still in the input box: send it again.")
  expect(app.value('id:compose-input')).toBe('echo phone-2')
  await app.waitUntil(() => !!step('typed again') || !!failed(), { timeout: 15000 })
  await app.press('id:compose-send')
  await app.waitFor('id:send-held', { timeout: 15000 })
  await app.waitUntil(() => !!step('long: ') || !!failed(), { timeout: 120000 })
  expect(failed()).toBe(undefined)
  expect(step('long: ')!.replace(/^long: (5[0-9]|6[0-9]) s/, 'long: about a minute')).toBe('long: about a minute, line steady, printed 1')
  expect(app.value('id:compose-input')).toBe('')
  await app.waitUntil(() => app.texts().includes('done') || !!failed(), { timeout: 15000 })
  expect(failed()).toBe(undefined)
}, 300000)
