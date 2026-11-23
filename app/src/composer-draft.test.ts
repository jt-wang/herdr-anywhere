import { expect, test } from 'nsh/test'
import { Store } from './store.ts'
import { FakeCore } from './testing/fakecore.ts'

test('a hook reload command cannot consume an unrelated unsent composer draft', async () => {
  const core = new FakeCore()
  const store = new Store({ pipe: () => core, setInterval: (() => 0) as any, clearInterval: (() => {}) as any })
  await store.connect()
  const r = await store.newSpace({})
  const draft = store.composerDraft(r.terminal)
  draft.set('/reload later, after checking the result')
  await store.compose(r.terminal, '/reload')
  expect(draft.get()).toBe('/reload later, after checking the result')
})
