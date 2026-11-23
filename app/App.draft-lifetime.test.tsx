import { expect, open, test } from 'nsh/test'

for (const unmount of [false, true]) for (const later of ['', ' pwd later']) {
  test(`delivery clears only the submitted prefix, including while away: unmount=${unmount}, later=${!!later}`, async () => {
    const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', hold: 'terminal.send' } }, clock: {} })
    await app.clock.runFor(100)
    await app.press('id:space-sp1')
    await app.type('id:compose-input', 'pwd')
    await app.press('id:compose-send')
    if (later) await app.type('id:compose-input', later)
    await app.clock.runFor(100)
    if (unmount) await app.press('id:back')
    await app.host.send('fake.release', {})
    await app.clock.runFor(100)
    if (unmount) await app.press('id:space-sp1')
    await app.waitFor('id:compose-input')
    expect(app.value('id:compose-input')).toBe(later.trimStart())
    expect(JSON.parse(app.storage()['fake-sent'] ?? '[]').length).toBe(1)
  })
}

test('a remounted composer follows the acknowledgement of its predecessor without resending', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'single', hold: 'terminal.send' } }, clock: {} })
  await app.clock.runFor(100)
  await app.press('id:space-sp1')
  await app.type('id:compose-input', 'pwd')
  await app.press('id:compose-send')
  await app.clock.runFor(100)
  await app.press('id:back')
  await app.press('id:space-sp1')
  expect(app.value('id:compose-input')).toBe('pwd')
  await app.host.send('fake.release', {})
  await app.clock.runFor(100)
  expect(app.value('id:compose-input')).toBe('')
  expect(JSON.parse(app.storage()['fake-sent'] ?? '[]').length).toBe(1)
})
