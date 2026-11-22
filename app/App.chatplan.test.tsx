// A plan the agent wrote for review (the Antigravity CLI's plan mode) in the chat view: a step with its own mark, the
// plan a tap away, shown as the formatted text it is.
import { expect, open, test } from 'nsh/test'

test('a plan is a step of its own, its text shown formatted', async () => {
  const plan = '# Merge the notes\n\n1. Read both files.\n2. Put the todo lines under the notes.'
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', argv: 'agy', status: 'done', chat: JSON.stringify({ chat: true, details: { '3.0': { input: plan } },
    turns: [{ id: 'u1', from: 'you', said: '/plan merge the notes', end: 'open', reply: 'The plan is ready for your review.', steps: [{ id: '3.0', kind: 'plan', title: 'Merge the notes' }] }] }) } }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.waitFor('id:terminal-t1')
  await app.clock.runFor(100)
  await app.press('id:show-chat')
  await app.press('id:fold-u1')
  await app.press('id:step-3.0')
  await app.waitFor('id:plan-3.0')
  // Formatted: the Markdown's marks are not shown as text.
  expect(app.texts().some((x: string) => x.includes('# Merge the notes'))).toBe(false)
})
