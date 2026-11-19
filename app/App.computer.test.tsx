// Settings › Computer: which computer, whether it answers, how the connection really travels, both versions, and
// how to unpair. What it says about encryption follows the link: only TailPocket's tunnel may say end-to-end.
import { expect, open, test } from 'nsh/test'

const settings = async (params: Record<string, string>, more: Record<string, unknown> = {}) => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', ...params } }, ...more })
  await app.waitFor('id:settings')
  await app.press('id:settings')
  await app.waitFor('id:connection')
  return app
}
const E2E = 'End-to-end encrypted tunnel'

test('through TailPocket: the name, connected, end-to-end encrypted, direct, both versions and how to unpair', async () => {
  const app = await settings({ via: 'tailpocket', routes: 'direct' })
  await app.waitFor('text:Direct to your computer')
  const seen = app.texts()
  for (const text of ['studio', 'Status', 'Connected', 'Connection', E2E, 'Forget this computer']) expect(seen).toContain(text)
  await app.waitFor('text:9.9.9')
  expect(app.texts()).toContain('Companion version')
  expect(app.texts()).toContain('herdr-core version')
})

test('through a relay, with the region it reports', async () => {
  const app = await settings({ via: 'tailpocket', routes: 'relay:tok' })
  await app.waitFor('text:Through a relay (TOK)')
  expect(app.texts()).not.toContain('Direct to your computer')
})

test('a relay with no region it can name still says relay', async () => {
  const app = await settings({ via: 'tailpocket', routes: 'relay:' })
  await app.waitFor('text:Through a relay')
})

// TailPocket v0.3's local route: still TailPocket's own end-to-end encryption, never the companion's plain port.
test('over the local network, end-to-end encrypted', async () => {
  const app = await settings({ via: 'tailpocket', routes: 'lan' })
  await app.waitFor('text:Over your local network, end-to-end encrypted')
  expect(app.texts()).toContain(E2E)
  expect(app.texts()).not.toContain('Direct to your computer')
})

test('a route the tunnel cannot tell yet is not guessed', async () => {
  const app = await settings({ via: 'tailpocket', routes: 'unknown' })
  await app.waitFor('text:Finding the route…')
  expect(app.texts()).not.toContain('Direct to your computer')
  expect(app.texts().some((t: string) => t.startsWith('Through a relay'))).toBe(false)
})

test('the route line follows the tunnel: relay first, direct once it finds a path', async () => {
  const app = await settings({ via: 'tailpocket', routes: 'relay:tok,direct' }, { clock: {} })
  await app.clock.runFor(100)
  await app.waitFor('text:Through a relay (TOK)')
  await app.clock.runFor(30_000)
  await app.waitFor('text:Direct to your computer')
  expect(app.texts().some((t: string) => t.startsWith('Through a relay'))).toBe(false)
})

test('a tunnel that fails to report is not guessed either', async () => {
  const app = await settings({ via: 'tailpocket', routes: 'throws' })
  await app.waitFor('text:Finding the route…')
})

test('plain HTTP on the network says it is not encrypted, and says nothing about a tunnel or a route', async () => {
  const app = await settings({ url: 'http://192.0.2.10:7681' })
  await app.waitFor('text:Not encrypted')
  const seen = app.texts()
  expect(seen).toContain('HTTP to 192.0.2.10:7681. Others on that network can read it.')
  for (const text of [E2E, 'Direct to your computer', 'Finding the route…']) expect(seen).not.toContain(text)
  expect(seen.some((t: string) => /encrypted tunnel|relay/i.test(t))).toBe(false)
})

test('HTTPS says encrypted to that address, not end-to-end: whatever ends the TLS can read it', async () => {
  const app = await settings({ url: 'https://mac.example.test' })
  await app.waitFor('text:Encrypted with HTTPS')
  expect(app.texts()).toContain('To mac.example.test')
  expect(app.texts()).not.toContain(E2E)
})

test('a tailnet address is not called readable: the app cannot see that network, and says what it knows', async () => {
  for (const [url, host] of [['http://100.101.102.103:7681', '100.101.102.103:7681'], ['http://studio.tail1234.ts.net:7681', 'studio.tail1234.ts.net:7681']]) {
    const app = await settings({ url })
    await app.waitFor('text:Not encrypted by this app')
    expect(app.texts()).toContain(`HTTP to ${host}. Your tailnet encrypts it if this is a tailnet address.`)
    expect(app.texts()).not.toContain('Not encrypted')
  }
})

test('an address that only looks close to a tailnet is still plain HTTP', async () => {
  const app = await settings({ url: 'http://100.128.0.1:7681' })
  await app.waitFor('text:Not encrypted')
  expect(app.texts()).toContain('HTTP to 100.128.0.1:7681. Others on that network can read it.')
})

test('a computer that does not answer is not called connected, and shows no route', async () => {
  const app = await settings({ via: 'tailpocket', routes: 'direct', conn: 'lost' })
  await app.waitFor('text:Reconnecting…')
  expect(app.texts()).not.toContain('Connected')
  expect(app.texts()).not.toContain('Direct to your computer')
  expect(app.texts()).toContain(E2E)
})

test('a companion that cannot say its version leaves the row out rather than make one up', async () => {
  const app = await settings({ via: 'tailpocket', routes: 'direct', info: 'none' })
  await app.waitFor('text:Direct to your computer')
  expect(app.texts()).not.toContain('Companion version')
})

test('Forget still asks first, and says what it does only then: on the page it read like a warning', async () => {
  const app = await settings({ via: 'tailpocket', routes: 'direct' })
  expect(app.texts().some((t) => t.includes('pair again'))).toBe(false)
  const asked = new Promise<string>((said) => app.onDialog((d) => { said(d.title ?? ''); return 'Cancel' }))
  await app.press('id:forget')
  expect(await asked).toBe('Forget studio?')
  expect(app.texts()).toContain('studio')
})

const WORDS: Record<string, [string, string]> = {
  'zh-Hans': ['端到端加密隧道', '直连你的电脑'], 'zh-Hant': ['端對端加密通道', '直接連線到你的電腦'], es: ['Túnel cifrado de extremo a extremo', 'Directo a tu computadora'],
  ko: ['종단 간 암호화 터널', '컴퓨터에 직접 연결'], ja: ['エンドツーエンド暗号化トンネル', 'パソコンに直接接続'], ru: ['Туннель со сквозным шифрованием', 'Напрямую к компьютеру'],
}
for (const [locale, [tunnel, direct]] of Object.entries(WORDS)) {
  test(`${locale}: the section is in the phone's language`, async () => {
    const app = await settings({ via: 'tailpocket', routes: 'direct' }, { locale })
    await app.waitFor(`text:${direct}`)
    expect(app.texts()).toContain(tunnel)
    expect(app.texts()).not.toContain('Forget this computer')
  })
}

test('an e2e build that cannot tap can ask for Settings at launch; it opens once the computer is connected', async () => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', via: 'tailpocket', routes: 'direct', open: 'settings' } } })
  await app.waitFor('text:Direct to your computer')
  expect(app.texts()).toContain('Settings')
})
