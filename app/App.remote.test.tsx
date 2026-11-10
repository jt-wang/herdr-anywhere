// Spaces on a server that herdr-core reaches over ssh (`hc machine add my-server`): home names the server above its
// spaces, even when every space is there, so it is clear they are not on this computer.
import { expect, open, test } from 'nsh/test'

// The server's spaces are `my-server/sp1`…: herdr-core names a machine's ids after it.
const home = (remote: string) => open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', remote } } })

test('every space on a server: home still names the server above them', async () => {
  const app = await home('only')
  await app.waitFor('text:web')
  await app.waitFor('text:my-server')
  const t = app.texts()
  const spaces = t.indexOf('Spaces')
  expect(spaces < t.indexOf('my-server') && t.indexOf('my-server') < t.indexOf('api', spaces)).toBe(true)
  expect(t).not.toContain('studio')
})

test('spaces here and on a server: each group under its machine name', async () => {
  const app = await home('both')
  await app.waitFor('id:space-sp9')
  await app.waitFor('text:web')
  await app.waitFor('text:my-server')
  const t = app.texts()
  expect(t).toContain('studio')
  const spaces = t.indexOf('Spaces')
  expect(t.indexOf('studio') < t.indexOf('notes', spaces) && t.indexOf('notes', spaces) < t.indexOf('my-server') && t.indexOf('my-server') < t.indexOf('api', spaces)).toBe(true)
})

test('every space on this computer: no machine name, as before', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' } } })
  await app.waitFor('id:space-sp1')
  await app.wait(300)
  expect(app.texts()).not.toContain('studio')
})
