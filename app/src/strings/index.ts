// The app's words in seven languages: the one picked in Settings › Language, or the phone's. `t` reads both as
// signals, so a screen follows a change at once.
// Spans, dates and lists are written here: NSH's runtime has Intl.NumberFormat and DateTimeFormat only (measured
// with `nsh test` on 0.3.0: no PluralRules, RelativeTimeFormat or ListFormat).
import { locale, signal } from 'nsh'
import { en } from './en.ts'
import { es } from './es.ts'
import { ja } from './ja.ts'
import { ko } from './ko.ts'
import { ru } from './ru.ts'
import { zhHans } from './zh-Hans.ts'
import { zhHant } from './zh-Hant.ts'

export type Lang = 'en' | 'zh-Hans' | 'zh-Hant' | 'es' | 'ko' | 'ja' | 'ru'
export const LANGS: Lang[] = ['en', 'zh-Hans', 'zh-Hant', 'es', 'ko', 'ja', 'ru']
export type Key = keyof typeof en
type Forms = { one?: string; few?: string; many?: string; other?: string }
export type Table = Record<Key, string | Forms>
export const TABLES: Record<Lang, Table> = { en, 'zh-Hans': zhHans, 'zh-Hant': zhHant, es, ko, ja, ru }

/** The table for a BCP 47 tag: Chinese by script or region; a language the app doesn't have is English. */
export function langFor(tag: string): Lang {
  const t = tag.replace(/_/g, '-').toLowerCase()
  if (/^zh(-|$)/.test(t)) return /-hant(-|$)/.test(t) || (!/-hans(-|$)/.test(t) && /-(tw|hk|mo)(-|$)/.test(t)) ? 'zh-Hant' : 'zh-Hans'
  const base = t.split('-')[0] as Lang
  return (['es', 'ko', 'ja', 'ru'] as Lang[]).includes(base) ? base : 'en'
}

/** Each language's name in its own words, in the order Settings › Language lists them. */
export const LANG_NAMES: [Lang, string][] = [['en', 'English'], ['es', 'Español'], ['ru', 'Русский'], ['zh-Hans', '简体中文'], ['zh-Hant', '繁體中文'], ['ja', '日本語'], ['ko', '한국어']]
export const langName = (l: Lang): string => LANG_NAMES.find(([x]) => x === l)![1]

/** The table for the phone's languages in order: the first the app has, as the system picks one (English for none). */
export function langForList(tags: string[]): Lang {
  return langFor(tags.find((x) => langFor(x) !== 'en' || /^en(-|_|$)/i.test(x)) ?? 'en')
}

// Settings › Language: a language picked in the app ('' follows the phone), and the phone's own languages as the host
// reads them, past the app's own language in the system's settings (null: none read, `locale()` is the phone's).
const picked = signal<Lang | ''>('')
const phone = signal<string[] | null>(null)
export const setPicked = (l: Lang | '') => picked.set(l)
export const setPhoneLanguages = (tags: string[] | null) => phone.set(tags?.length ? tags : null)
/** The language "Follow system" reads in now. */
export const systemLang = (): Lang => (phone() ? langForList(phone()!) : langFor(locale()))
export const lang = (): Lang => picked() || systemLang()

/** Which plural form a count takes. */
export function plural(l: Lang, n: number): 'one' | 'few' | 'many' | 'other' {
  if (l === 'en' || l === 'es') return n === 1 ? 'one' : 'other'
  if (l !== 'ru') return 'other'
  const ten = n % 10, hundred = n % 100
  if (ten === 1 && hundred !== 11) return 'one'
  return ten >= 2 && ten <= 4 && !(hundred >= 12 && hundred <= 14) ? 'few' : 'many'
}

/** The word in the phone's language, its blanks filled in; a word that counts picks its form by `vars.n`. */
export function t(key: Key, vars?: Record<string, string | number>): string {
  const l = lang()
  const value = TABLES[l][key]
  const text = typeof value === 'string' ? value : value[plural(l, Number(vars?.n ?? 0))] ?? ''
  return text.replace(/\{(\w+)\}/g, (all, name) => (vars && name in vars ? String(vars[name]) : all))
}

export type Unit = 'm' | 'h' | 'd'
export type Formats = {
  /** How a day of a month is written; `month` from 0. */
  monthDay: (month: number, day: number) => string
  /** A span of time, short: under a minute when `n` is 0. */
  span: (unit: Unit, n: number) => string
  listSep: string
}

const units = (u: Record<Unit, string>, under: string) => (unit: Unit, n: number) => (unit === 'm' && n < 1 ? under : `${n}${u[unit]}`)
const CJK_DAY = (month: number, day: number) => `${month + 1}月${day}日`
const EN_MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const ES_MONTHS = ['ene', 'feb', 'mar', 'abr', 'may', 'jun', 'jul', 'ago', 'sep', 'oct', 'nov', 'dic']
const RU_MONTHS = ['янв.', 'февр.', 'мар.', 'апр.', 'мая', 'июн.', 'июл.', 'авг.', 'сент.', 'окт.', 'нояб.', 'дек.']

const FORMATS: Record<Lang, Formats> = {
  en: { monthDay: (m, d) => `${EN_MONTHS[m]} ${d}`, span: units({ m: 'm', h: 'h', d: 'd' }, '<1m'), listSep: ', ' },
  'zh-Hans': { monthDay: CJK_DAY, span: units({ m: '分钟', h: '小时', d: '天' }, '<1分钟'), listSep: '，' },
  'zh-Hant': { monthDay: CJK_DAY, span: units({ m: '分鐘', h: '小時', d: '天' }, '<1分鐘'), listSep: '，' },
  es: { monthDay: (m, d) => `${d} ${ES_MONTHS[m]}`, span: units({ m: ' min', h: ' h', d: ' d' }, '<1 min'), listSep: ', ' },
  ko: { monthDay: (m, d) => `${m + 1}월 ${d}일`, span: units({ m: '분', h: '시간', d: '일' }, '1분 미만'), listSep: ', ' },
  ja: { monthDay: CJK_DAY, span: units({ m: '分', h: '時間', d: '日' }, '1分未満'), listSep: '、' },
  ru: { monthDay: (m, d) => `${d} ${RU_MONTHS[m]}`, span: units({ m: ' мин', h: ' ч', d: ' д' }, '<1 мин'), listSep: ', ' },
}

export const formatsFor = (l: Lang): Formats => FORMATS[l]
export const formats = (): Formats => FORMATS[lang()]

/** A day and a time on this phone's clock, as the language writes the day; the time on a 24-hour clock. */
export function dayTime(d: Date, l: Lang = lang()): string {
  const two = (n: number) => String(n).padStart(2, '0')
  return `${FORMATS[l].monthDay(d.getMonth(), d.getDate())} ${two(d.getHours())}:${two(d.getMinutes())}`
}

/** Parts of a screen reader's sentence, in the phone's language. */
export const listOf = (...parts: string[]): string => parts.filter(Boolean).join(formats().listSep)
