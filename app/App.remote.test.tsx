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

// What a new tab can run is what its machine has: herdr-core's `machine.exec --on` asks that machine, as for this one.
const newAgents = (app: any) => ['claude', 'codex', 'pi'].filter((k) => app.count(`id:new-${k}`) > 0)

test("adding an agent on a server lists the server's agents, not this computer's", async () => {
  const app = await home('both')
  await app.waitFor('id:space-sp9')
  await app.press('id:add-agent-my-server/sp1')
  await app.waitFor('id:new-pi')
  expect(newAgents(app)).toEqual(['pi'])
})

test("adding an agent on this computer still lists this computer's agents", async () => {
  const app = await home('both')
  await app.waitFor('id:space-sp9')
  await app.waitFor('text:my-server')
  await app.press('id:add-agent-sp9')
  await app.waitFor('id:new-claude')
  expect(newAgents(app)).toEqual(['claude', 'codex'])
})

test("the switcher's new agents in a server's space are the server's", async () => {
  const app = await home('both')
  await app.waitFor('id:space-sp9')
  await app.press('id:space-my-server/sp1')
  await app.press('id:switcher')
  await app.waitFor('id:switch-new-pi')
  expect(['claude', 'codex', 'pi'].filter((k) => app.count(`id:switch-new-${k}`) > 0)).toEqual(['pi'])
})

test('an agent installed on the server later shows the next time the menu opens', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', remote: 'both', remoteInstalls: 'later' } } })
  await app.waitFor('id:space-sp9')
  await app.press('id:add-agent-my-server/sp1')
  await app.waitFor('id:new-pi')
  expect(newAgents(app)).toEqual(['pi'])
  await app.back()
  await app.wait(300)
  await app.press('id:add-agent-my-server/sp1')
  await app.waitFor('id:new-codex')
  expect(newAgents(app)).toEqual(['codex', 'pi'])
})
