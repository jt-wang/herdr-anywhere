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
