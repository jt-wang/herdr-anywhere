// The screens in the phone's language: seven languages, and no English left on any screen but what the computer named.
import { expect, open, test } from 'nsh/test'
import { en } from './src/strings/en.ts'

const LOCALES = ['zh-CN', 'zh-TW', 'es-ES', 'ko-KR', 'ja-JP', 'ru-RU']
// What isn't the app's to translate: what the fake computer and agents say, commands, and names.
const DATA = /^(Herdr|Claude Code|Codex|TailPocket|api|web|main|git|esc|tab|enter|ctrl|⇧tab|\^C|Documents|notes|src|clients|demo-app|studio|zsh|~.*|\/.*|http.*|herdr.*|<?\d.*|.*›.*|.*(fix login|login\.ts|npm test|Yes|No, |herdr-anywhere|herdr-core|herdr1|TailPocket|Claude Code|Codex|\{).*)$/

// Spanish reads in the same letters, so its check is the English table itself: a word the app knows, still in English.
const WORDS = Object.values(en).flatMap((v) => (typeof v === 'string' ? [v] : Object.values(v)))
  .map((w) => new RegExp(`^${w.replace(/[.*+?^$()|[\]\\]/g, '\\$&').replace(/\{\w+\}/g, '.*')}$`))
const SAME = ['Terminal', 'Shell', 'Panel', 'Experimental']

function english(texts: string[], locale: string): string[] {
  if (locale.startsWith('es')) return texts.filter((t) => WORDS.some((w) => w.test(t)) && !SAME.includes(t) && !DATA.test(t.trim()))
  return texts.filter((t) => /[A-Za-z]{2}/.test(t) && !/[^\x00-\x7f]{2}/.test(t) && !DATA.test(t.trim()))
}

for (const locale of LOCALES) {
  test(`${locale}: home, settings, a new space's folders and a pane have no English words left`, async () => {
    const app = await open('./fixtures/fake.tsx', { locale, host: { params: { scene: 'busy' } } })
    await app.waitFor('id:needs-t1')
    expect(english(app.texts(), locale)).toEqual([])
    await app.press('id:settings')
    await app.waitFor('id:alert-blocked')
    expect(english(app.texts(), locale)).toEqual([])
    await app.press('id:back')
    await app.waitFor('id:new')
    await app.press('id:new')
    await app.waitFor('id:folder-search')
    expect(english(app.texts(), locale)).toEqual([])
    await app.back()
    await app.waitUntil(() => !app.shows('id:folder-search'))
    await app.press('id:space-sp2')
    await app.waitFor('id:switcher')
    expect(english(app.texts(), locale)).toEqual([])
    await app.press('id:switcher')
    await app.wait(100)
    expect(english(app.texts(), locale)).toEqual([])
  })

  test(`${locale}: the pairing screen has no English words left`, async () => {
    const app = await open('./fixtures/fake.tsx', { locale, host: { params: { scene: 'unpaired' } } })
    await app.waitFor('id:pair')
    expect(english(app.texts(), locale)).toEqual([])
  })
}

test('counts and statuses read in the language, and a language the app lacks reads in English', async () => {
  const ru = await open('./fixtures/fake.tsx', { locale: 'ru-RU', host: { params: { scene: 'busy' } } })
  await ru.waitFor('id:needs-t1')
  expect(ru.texts()).toContain('2 агента · 1 ждёт вас')
  const de = await open('./fixtures/fake.tsx', { locale: 'de-DE', host: { params: { scene: 'busy' } } })
  await de.waitFor('id:needs-t1')
  expect(de.texts()).toContain('Needs you')
  expect(de.texts()).toContain('Spaces')
  const hant = await open('./fixtures/fake.tsx', { locale: 'zh-HK', host: { params: { scene: 'busy' } } })
  await hant.waitFor('id:needs-t1')
  expect(hant.texts()).toContain('空間')
  expect(hant.texts()).toContain('等你處理')
})

test('what the computer says stays as it is: an agent\'s own choices are not translated', async () => {
  const app = await open('./fixtures/fake.tsx', { locale: 'ja-JP', host: { params: { scene: 'busy' } } })
  await app.waitFor('id:needs-t1-choice-1')
  expect(app.count('label:1. Yes')).toBe(1)
})

for (const locale of LOCALES) {
  test(`${locale}: a LAN computer that refuses, and one reached by a name, have no English words left`, async () => {
    for (const url of ['http://192.0.2.10:7681', 'http://mac.example:7681']) {
      const app = await open('./fixtures/fake.tsx', { locale, host: { params: { scene: 'companion-refused', url } } })
      await app.waitFor('id:pair-again')
      expect(english(app.texts(), locale)).toEqual([])
    }
  })
}

for (const locale of LOCALES) {
  test(`${locale}: the notice that the unencrypted LAN port stops has no English words left`, async () => {
    const app = await open('./fixtures/fake.tsx', { locale, host: { params: { scene: 'busy', url: 'http://192.168.1.20:7681', tunnel: 'on', info: JSON.stringify({ version: '0.3.0', plainUntil: new Date(Date.now() + 2 * 86_400_000).toISOString() }) } } })
    await app.waitFor('id:plain-notice')
    expect(english(app.texts(), locale)).toEqual([])
  })
}

for (const locale of LOCALES) {
  test(`${locale}: a computer that doesn't answer, and pairing again from there, have no English words left`, async () => {
    const app = await open('./fixtures/fake.tsx', { locale, host: { params: { scene: 'companion-off', via: 'tailpocket' } } })
    await app.waitFor('id:pair-again')
    expect(app.shows('text:herdr-anywhere start')).toBe(true)
    expect(english(app.texts(), locale)).toEqual([])
    await app.press('id:pair-again')
    await app.waitFor('id:pair-back')
    expect(english(app.texts(), locale)).toEqual([])
  })
}

// Settings › Language: a pick shows at once on every screen and stays; Follow system goes back to the phone's.
test('picking a language changes the words at once, stays after a restart, and Follow system follows the phone again', async () => {
  const app = await open('./fixtures/fake.tsx', { locale: 'en-US', host: { params: { scene: 'busy' } } })
  await app.waitFor('id:needs-t1')
  await app.press('id:settings')
  await app.waitFor('id:settings-language')
  expect(app.texts()).toContain('Follow system')
  await app.press('id:settings-language')
  await app.waitFor('id:language-system')
  for (const w of ['Follow system', 'English', 'Español', 'Русский', '简体中文', '繁體中文', '日本語', '한국어']) expect(app.texts()).toContain(w)
  await app.press('id:language-zh-Hans')
  await app.waitFor('text:语言')
  expect(app.texts()).toContain('跟随系统')
  await app.press('id:language-back')
  await app.waitFor('id:settings-language-state')
  expect(app.texts()).toContain('设置')
  expect(app.texts()).toContain('简体中文')
  await app.press('id:back')
  await app.waitFor('text:等你处理')
  expect(english(app.texts(), 'zh-CN')).toEqual([])
  const saved = JSON.parse(app.storage()['fake-prefs'])
  expect(saved.language).toBe('zh-Hans')

  // Opened again with those settings: still Chinese on an English phone.
  const again = await open('./fixtures/fake.tsx', { locale: 'en-US', host: { params: { scene: 'busy', prefs: JSON.stringify({ language: 'es' }) } } })
  await again.waitFor('id:needs-t1')
  await again.press('id:settings')
  await again.waitFor('id:settings-language')
  expect(again.texts()).toContain('Ajustes')
  await again.press('id:settings-language')
  await again.waitFor('id:language-system')
  await again.press('id:language-system')
  await again.waitFor('text:Language')
  expect(JSON.parse(again.storage()['fake-prefs']).language).toBe('')
})

test('Follow system says the phone\'s language and reads in it', async () => {
  const app = await open('./fixtures/fake.tsx', { locale: 'ja-JP', host: { params: { scene: 'busy' } } })
  await app.waitFor('id:needs-t1')
  await app.press('id:settings')
  await app.press('id:settings-language')
  await app.waitFor('id:language-system')
  expect(app.texts()).toContain('システムに従う')
  expect(app.texts()).toContain('日本語') // the language Follow system reads in now, and its own row
  expect(app.count('text:日本語')).toBe(2)
  await app.press('id:language-ko')
  await app.waitFor('text:시스템 언어 사용')
  expect(app.count('text:日本語')).toBe(2) // under Follow system still the phone's
})

// The phone's own setting for this app (Android 13+, iOS): read at the start and when the app comes back, set with each pick.
test('the language the phone keeps for the app wins at the start, and a pick is set there too', async () => {
  const set: unknown[] = []
  let kept = { app: 'ru-RU', system: ['en-US'] }
  const app = await open('./fixtures/fake.tsx', { locale: 'en-US', host: { params: { scene: 'busy', prefs: JSON.stringify({ language: 'zh-Hans' }) },
    handlers: { 'page.language': () => kept, 'page.setLanguage': (d: any) => { set.push(d.tag); kept = { app: d.tag, system: ['en-US'] } } } } })
  await app.waitFor('id:needs-t1')
  await app.press('id:settings')
  await app.waitFor('id:settings-language')
  expect(app.texts()).toContain('Настройки')
  await app.press('id:settings-language')
  await app.waitFor('id:language-system')
  // Follow system: the phone's languages under the app's own.
  expect(app.count('text:English')).toBe(2)
  await app.press('id:language-ja')
  await app.waitFor('text:言語')
  expect(set).toEqual(['ja'])
  await app.press('id:language-system')
  await app.waitFor('text:Language')
  expect(set).toEqual(['ja', ''])
  // Changed in the phone's settings while the app was away: it reads it again on the way back.
  kept = { app: 'es', system: ['en-US'] }
  await app.host.send('page.front', { front: false })
  await app.host.send('page.front', { front: true })
  await app.waitFor('text:Idioma')
})
