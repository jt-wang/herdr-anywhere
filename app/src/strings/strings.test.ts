// Every word in all seven languages, with the same blanks, and the formats each language writes its own way.
import { expect, test } from 'nsh/test'
import { en } from './en.ts'
import { formatsFor, LANGS, langFor, plural, TABLES, type Lang } from './index.ts'

const blanks = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort()
const FORMS: Record<Lang, string[]> = { en: ['one', 'other'], es: ['one', 'other'], ru: ['one', 'few', 'many'], 'zh-Hans': ['other'], 'zh-Hant': ['other'], ja: ['other'], ko: ['other'] }
// Words that read the same in a language, on purpose.
const SAME: Partial<Record<Lang, string[]>> = { es: ['terminal', 'shell', 'panel', 'points', 'experimental', 'withEvidence'], ru: ['withEvidence'] }

test('every language has every word and no others', () => {
  for (const l of LANGS) expect(Object.keys(TABLES[l]).sort()).toEqual(Object.keys(en).sort())
})

test('every word has the same blanks as the English, in each of its plural forms', () => {
  for (const l of LANGS) for (const [key, source] of Object.entries(en)) {
    const want = blanks(typeof source === 'string' ? source : source.other)
    const value = TABLES[l][key as keyof typeof en] as string | Record<string, string>
    const forms = typeof value === 'string' ? [value] : Object.values(value)
    for (const f of forms) expect(`${l}.${key}: ${blanks(f).join()}`).toBe(`${l}.${key}: ${want.join()}`)
  }
})

test('a word that counts has the plural forms its language needs', () => {
  for (const l of LANGS) for (const [key, source] of Object.entries(en)) {
    if (typeof source === 'string') { expect(`${l}.${key} ${typeof TABLES[l][key as keyof typeof en]}`).toBe(`${l}.${key} string`); continue }
    expect(`${l}.${key} ${Object.keys(TABLES[l][key as keyof typeof en]).sort().join()}`).toBe(`${l}.${key} ${[...FORMS[l]].sort().join()}`)
  }
})

test('no word is left in English', () => {
  for (const l of LANGS) if (l !== 'en') for (const key of Object.keys(en) as (keyof typeof en)[]) {
    if (SAME[l]?.includes(key)) continue
    expect(`${l}.${key} same: ${JSON.stringify(TABLES[l][key]) === JSON.stringify(en[key])}`).toBe(`${l}.${key} same: false`)
  }
})

test('the phone\'s language picks the table: traditional Chinese by script or region, anything else unknown is English', () => {
  const cases: [string, Lang][] = [
    ['en-US', 'en'], ['en', 'en'], ['de-DE', 'en'], ['pt-BR', 'en'], ['', 'en'],
    ['zh', 'zh-Hans'], ['zh-CN', 'zh-Hans'], ['zh-Hans', 'zh-Hans'], ['zh-Hans-TW', 'zh-Hans'], ['zh-SG', 'zh-Hans'], ['zh_CN', 'zh-Hans'],
    ['zh-Hant', 'zh-Hant'], ['zh-TW', 'zh-Hant'], ['zh-HK', 'zh-Hant'], ['zh-MO', 'zh-Hant'], ['zh-Hant-CN', 'zh-Hant'], ['zh_TW', 'zh-Hant'],
    ['es', 'es'], ['es-MX', 'es'], ['ko-KR', 'ko'], ['ja-JP', 'ja'], ['ru-RU', 'ru'], ['ru', 'ru'],
  ]
  for (const [tag, want] of cases) expect(`${tag} ${langFor(tag)}`).toBe(`${tag} ${want}`)
})

test('counting follows each language: Russian has three forms, Chinese, Japanese and Korean one', () => {
  const ru = [[1, 'one'], [2, 'few'], [4, 'few'], [5, 'many'], [11, 'many'], [12, 'many'], [21, 'one'], [22, 'few'], [25, 'many'], [111, 'many'], [0, 'many']] as const
  for (const [n, form] of ru) expect(`${n} ${plural('ru', n)}`).toBe(`${n} ${form}`)
  expect([plural('en', 1), plural('en', 2), plural('en', 0)]).toEqual(['one', 'other', 'other'])
  expect([plural('es', 1), plural('es', 2)]).toEqual(['one', 'other'])
  for (const l of ['zh-Hans', 'zh-Hant', 'ja', 'ko'] as const) expect([plural(l, 1), plural(l, 2)]).toEqual(['other', 'other'])
})

test('dates, spans and lists are written as each language writes them', () => {
  const day: Record<Lang, string> = { en: 'Mar 5', 'zh-Hans': '3月5日', 'zh-Hant': '3月5日', es: '5 mar', ko: '3월 5일', ja: '3月5日', ru: '5 мар.' }
  const span: Record<Lang, string[]> = {
    en: ['<1m', '5m', '3h', '2d'], 'zh-Hans': ['<1分钟', '5分钟', '3小时', '2天'], 'zh-Hant': ['<1分鐘', '5分鐘', '3小時', '2天'],
    es: ['<1 min', '5 min', '3 h', '2 d'], ko: ['1분 미만', '5분', '3시간', '2일'], ja: ['1分未満', '5分', '3時間', '2日'], ru: ['<1 мин', '5 мин', '3 ч', '2 д'],
  }
  const list: Record<Lang, string> = { en: 'a, b', 'zh-Hans': 'a，b', 'zh-Hant': 'a，b', es: 'a, b', ko: 'a, b', ja: 'a、b', ru: 'a, b' }
  for (const l of LANGS) {
    const f = formatsFor(l)
    expect(f.monthDay(2, 5)).toBe(day[l])
    expect([f.span('m', 0), f.span('m', 5), f.span('h', 3), f.span('d', 2)]).toEqual(span[l])
    expect(['a', 'b'].join(f.listSep)).toBe(list[l])
  }
})
