import { expect, test } from 'nsh/test'
import { Alerts, SOUND_LIMIT_MS } from './alerts.ts'
import type { Toast } from './store.ts'

const toast = (title: string): Toast => ({ id: 1, title, level: 'error', action: { terminal: 't1' } })

function setup() {
  const timers: { f: () => void; ms: number }[] = []
  const notified: string[] = []
  const kinds: string[] = []
  const quiet: boolean[] = []
  const alerts = new Alerts({
    notify: (t, kind, q) => { notified.push(t.title); kinds.push(kind); quiet.push(!!q) },
    setTimeout: ((f: () => void, ms: number) => { timers.push({ f, ms }); return 0 }) as any,
  })
  return { alerts, timers, notified, kinds, quiet }
}

test("every alert plays Herdr's sound for it, as the desktop does", () => {
  const { alerts } = setup()
  alerts.ring('blocked', toast('Claude Code needs you'), true)
  alerts.ring('done', toast('Codex finished'), true)
  expect(alerts.sounds.get().map((s) => s.kind)).toEqual(['blocked', 'done'])
})

test('a sound goes away when it ends, or after the time a player gets on the desktop', () => {
  const { alerts, timers } = setup()
  alerts.ring('blocked', toast('a'), true)
  alerts.ring('done', toast('b'), true)
  alerts.ended(alerts.sounds.get()[0].id)
  expect(alerts.sounds.get().map((s) => s.kind)).toEqual(['done'])
  expect(timers[1].ms).toBe(SOUND_LIMIT_MS)
  timers[1].f()
  expect(alerts.sounds.get()).toEqual([])
})

test("the system's notification comes only while the app is not in front", () => {
  const { alerts, notified } = setup()
  alerts.ring('blocked', toast('Claude Code needs you'), true)
  expect(notified).toEqual([])
  alerts.ring('done', toast('Codex finished'), false)
  expect(notified).toEqual(['Codex finished'])
})

test('the notification says which kind it is, so a finished agent can post a quieter one', () => {
  const { alerts, kinds } = setup()
  alerts.ring('blocked', toast('Claude Code needs you'), false)
  alerts.ring('done', toast('Codex finished'), false)
  expect(kinds).toEqual(['blocked', 'done'])
})

test("new words for an alert change its notification without a sound, and only while the app isn't in front", () => {
  const { alerts, notified, quiet } = setup()
  alerts.ring('done', toast('Claude Code finished'), false)
  alerts.ring('done', toast('Claude Code finished'), false, true)
  alerts.ring('done', toast('Claude Code finished'), true, true)
  expect(alerts.sounds.get().length).toBe(1)
  expect(notified.length).toBe(2)
  expect(quiet).toEqual([false, true])
})

test("where the system's notification has the sound (Android), away from the app it is the only sound", () => {
  const notified: string[] = []
  const alerts = new Alerts({ notify: (t) => notified.push(t.title), soundAway: false, setTimeout: (() => 0) as any })
  alerts.ring('blocked', toast('Claude Code needs you'), false)
  alerts.ring('done', toast('Codex finished'), false)
  expect(notified).toEqual(['Claude Code needs you', 'Codex finished'])
  expect(alerts.sounds.get()).toEqual([])
  alerts.ring('blocked', toast('Claude Code needs you'), true)
  expect(alerts.sounds.get().map((s) => s.kind)).toEqual(['blocked'])
})
