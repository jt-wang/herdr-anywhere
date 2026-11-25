import { expect, open, test } from 'nsh/test'

for (const [program, name] of [
  ['codex', 'Codex'], ['claude', 'Claude Code'], ['pi', 'Pi'], ['omp', 'Oh My Pi'],
  ['opencode', 'OpenCode'], ['cursor-agent', 'Cursor'], ['agy', 'Antigravity'],
]) {
  test(`${name}: home, pinned and the pane header use the authenticated saved title without opening chat first`, async () => {
    const app = await open('./fixtures/fake.tsx', { width: 393, height: 880, colorScheme: 'dark', host: { params: {
      scene: 'saved-title', titleProgram: program, prefs: JSON.stringify({ showAgentNames: false }),
      savedTitle: JSON.stringify({ title: 'Review the sample index', session: 'opaque-session' }),
    } } })
    await app.waitFor('id:space-agent-t1 >> text:Review the sample index')
    expect(JSON.parse(app.storage()['fake-title-asks'])).toEqual(['/api/chat/t1/title'])
    expect(app.storage()['fake-chat-asks']).toBe(undefined)
    expect(app.count('id:activity-t1')).toBe(0)
    expect(app.count('id:context-t1')).toBe(1)
    expect(app.texts()).toContain('/synthetic/example')
    await app.host.send('fake.label', { address: 't1', labels: { 'herdr.pinned': '1' } })
    await app.waitFor('id:pinned-agent-t1 >> text:Review the sample index')
    expect(app.count('id:activity-t1')).toBe(0)
    expect(app.count('id:context-t1')).toBe(1)
    expect(app.texts()).toContain('/synthetic/example')
    await app.press('id:pinned-agent-t1')
    await app.waitFor('id:pane-title >> text:Review the sample index')
    expect(app.shows('id:pane-title >> text:Review the sample index')).toBe(true)
    expect(app.count('id:switcher >> text:Review the sample index')).toBe(1)
  })

  test(`${name}: an older companion keeps the program name and has no invented activity`, async () => {
    const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'saved-title', titleProgram: program } } })
    await app.waitFor(`id:space-agent-t1 >> text:${name}`)
    await app.waitUntil(() => !!app.storage()['fake-title-asks'])
    expect(app.shows(`id:space-agent-t1 >> text:${name}`)).toBe(true)
    expect(app.count('id:activity-t1')).toBe(0)
    expect(app.count('id:context-t1')).toBe(1)
    expect(app.texts()).toContain('/synthetic/example')
  })
}
