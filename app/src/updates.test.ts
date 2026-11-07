import { expect, test } from 'nsh/test'
import { Signal } from './store.ts'
import { APP_VERSION, installProblem, newer, Updates, type Conn } from './updates.ts'
import manifest from '../nsh.json'

const APP = { url: 'https://example.com/a.apk', digest: 'sha256:ab', bytes: 136_080_774 }

type Calls = { method: string; url: string; auth: string; body?: string }[]

function setup(o: { info?: any; os?: 'android' | 'ios' | 'web'; channel?: 'source' | 'store'; app?: string; answers?: Record<string, { status: number; body: any }>; install?: (a: any, p: (n: number) => void, ask?: boolean) => Promise<any>; waitMs?: number } = {}) {
  const calls: Calls = []
  const conn = new Signal<Conn>('ready')
  const box = { info: o.info }
  const toasts: { title: string; body?: string; level: string }[] = []
  const installs: any[] = []
  const fetcher = (async (url: string, init?: any) => {
    calls.push({ method: init?.method ?? 'GET', url, auth: init?.headers?.authorization ?? '', body: init?.body })
    if (url.endsWith('/api/update/checks') && box.info) box.info = { ...box.info, checks: JSON.parse(init.body).on ? 'on' : 'off' }
    const a = o.answers?.[url.replace('http://c', '')]
    if (a) return { ok: a.status < 300, status: a.status, json: async () => a.body }
    if (url.endsWith('/api/info')) {
      if (box.info === undefined) throw new Error('offline')
      return { ok: true, status: 200, json: async () => box.info }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as any
  const u = new Updates({
    baseUrl: async () => 'http://c',
    token: () => 'tok',
    conn,
    fetcher,
    os: o.os ?? 'android',
    channel: o.channel,
    appVersion: o.app ?? '0.1.5',
    toast: (t) => toasts.push(t),
    install: o.install ?? (async (a) => { installs.push(a); return 'installing' }),
    waitMs: o.waitMs ?? 5,
  })
  return { u, calls, conn, box, toasts, installs }
}

const settle = async () => { for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 2)) }

test('the app version is the one the app is built with', () => {
  expect(APP_VERSION).toBe((manifest as any).version)
})

test('versions compare as numbers, and something else is never newer', () => {
  expect(newer('0.1.10', '0.1.9')).toBe(true)
  expect(newer('v0.2.0', '0.1.9')).toBe(true)
  expect(newer('0.1.5', '0.1.5')).toBe(false)
  expect(newer('0.1.4', '0.1.5')).toBe(false)
  expect(newer('', '0.1.5')).toBe(false)
  expect(newer('dev', '0.1.5')).toBe(false)
  expect(newer('0.1.6', 'dev')).toBe(false)
})

test('nothing is shown before the computer answers, or when everything is current', async () => {
  const a = setup()
  await a.u.refresh()
  expect(a.u.notice()).toBe(null)
  const b = setup({ info: { version: '0.1.5', latest: '0.1.5', canUpdate: true, app: APP } })
  await b.u.refresh()
  expect(b.u.notice()).toBe(null)
})

test('the computer first: the app keeps its connection while it updates, and then the app', async () => {
  const { u, box } = setup({ info: { version: '0.1.5', latest: '0.1.6', canUpdate: true, app: APP } })
  await u.refresh()
  expect(u.notice()).toEqual({ kind: 'computer', version: '0.1.6' })
  box.info = { version: '0.1.6', latest: '0.1.6', canUpdate: true, app: APP }
  await u.refresh()
  expect(u.notice()).toEqual({ kind: 'app', version: '0.1.6' })
})

test('each phone gets the update it can use', async () => {
  const info = { version: '0.1.6', latest: '0.1.6', canUpdate: true, app: APP }
  for (const [os, want] of [['android', 'app'], ['ios', 'rebuild']] as const) {
    const { u } = setup({ os, info })
    await u.refresh()
    expect(u.notice()?.kind).toBe(want)
  }
  const web = setup({ os: 'web', info })
  await web.u.refresh()
  expect(web.u.notice()).toBe(null)
})

test('an app updated by the place it was installed from is not sent to build the app itself', async () => {
  const { u } = setup({ os: 'ios', channel: 'store', info: { version: '0.1.6', latest: '0.1.6', canUpdate: true, app: APP } })
  await u.refresh()
  expect(u.notice()).toBe(null)
  const computer = setup({ os: 'ios', channel: 'store', info: { version: '0.1.5', latest: '0.1.6', canUpdate: true, app: APP } })
  await computer.u.refresh()
  expect(computer.u.notice()).toEqual({ kind: 'computer', version: '0.1.6' })
})

test('an Android phone with a release that has no app is told nothing it cannot do', async () => {
  const { u } = setup({ info: { version: '0.1.6', latest: '0.1.6', canUpdate: true } })
  await u.refresh()
  expect(u.notice()).toBe(null)
})

test('a computer on an older companion, which knows of no releases, is updated by hand', async () => {
  const { u } = setup({ info: { version: '0.1.3' } })
  await u.refresh()
  expect(u.notice()).toEqual({ kind: 'computerByHand', version: '0.1.5' })
})

test('a computer no released file runs on is updated by hand', async () => {
  const { u } = setup({ info: { version: '0.1.5', latest: '0.1.6', canUpdate: false } })
  await u.refresh()
  expect(u.notice()).toEqual({ kind: 'computerByHand', version: '0.1.6' })
})

test('it asks when the connection comes back, and only then', async () => {
  const { u, calls, conn } = setup({ info: { version: '0.1.5', latest: '0.1.5', canUpdate: true } })
  u.start()
  await settle()
  expect(calls.filter((c) => c.url.endsWith('/api/info')).length).toBe(1)
  conn.set('reconnecting')
  await settle()
  expect(calls.filter((c) => c.url.endsWith('/api/info')).length).toBe(1)
  conn.set('ready')
  await settle()
  expect(calls.filter((c) => c.url.endsWith('/api/info')).length).toBe(2)
  u.stop()
  conn.set('reconnecting')
  conn.set('ready')
  await settle()
  expect(calls.filter((c) => c.url.endsWith('/api/info')).length).toBe(2)
})

test('updating the computer posts with the token, and says so once the new version answers', async () => {
  const { u, calls, box, toasts, conn } = setup({
    info: { version: '0.1.5', latest: '0.1.6', canUpdate: true, app: APP },
    answers: { '/api/update': { status: 200, body: { version: '0.1.6' } } },
    waitMs: 300,
  })
  u.start()
  await settle()
  const going = u.updateComputer()
  await settle()
  const post = calls.find((c) => c.method === 'POST' && c.url.endsWith('/api/update'))
  expect(post?.auth).toBe('Bearer tok')
  expect(u.busy.get()).toBe('computer')
  conn.set('reconnecting')
  box.info = { version: '0.1.6', latest: '0.1.6', canUpdate: true, app: APP }
  conn.set('ready')
  await going
  expect(u.busy.get()).toBe(null)
  expect(u.info.get()?.version).toBe('0.1.6')
  expect(toasts.length).toBe(1)
  expect(toasts[0].level).toBe('info')
})

test('a computer that fails to update says why and stays offered', async () => {
  const { u, toasts } = setup({
    info: { version: '0.1.5', latest: '0.1.6', canUpdate: true },
    answers: { '/api/update': { status: 502, body: { error: "the download doesn't match the release's digest" } } },
  })
  await u.refresh()
  await u.updateComputer()
  expect(u.busy.get()).toBe(null)
  expect(toasts[0].level).toBe('error')
  expect(u.notice()?.kind).toBe('computer')
})

test('a computer that never comes back with the new version is reported, and the busy state ends', async () => {
  const { u, toasts } = setup({
    info: { version: '0.1.5', latest: '0.1.6', canUpdate: true },
    answers: { '/api/update': { status: 200, body: { version: '0.1.6' } } },
  })
  await u.refresh()
  await u.updateComputer()
  expect(u.busy.get()).toBe(null)
  expect(toasts[0].level).toBe('error')
})

test('updating the app hands the file to the system and follows the download', async () => {
  const seen: number[] = []
  const { u, installs } = setup({
    info: { version: '0.1.6', latest: '0.1.6', canUpdate: true, app: APP },
    install: async (a, progress) => { seen.push(u0()); progress(0.5); seen.push(u0()); installs0.push(a); return 'installing' },
  })
  const installs0: any[] = []
  const u0 = () => u.progress.get()
  await u.refresh()
  await u.updateApp()
  expect(installs0).toEqual([APP])
  expect(seen).toEqual([0, 0.5])
  expect(installs.length).toBe(0)
  expect(u.busy.get()).toBe(null)
})

test('an app update the system must first allow says so, without an error', async () => {
  const { u, toasts } = setup({ info: { version: '0.1.6', latest: '0.1.6', canUpdate: true, app: APP }, install: async () => 'needsPermission' })
  await u.refresh()
  await u.updateApp()
  expect(toasts[0].level).toBe('info')
  expect(u.notice()?.kind).toBe('app')
})

test('back in the app after allowing installs, the download starts by itself', async () => {
  const asked: (boolean | undefined)[] = []
  const answers = ['needsPermission', 'installing']
  const { u, toasts } = setup({ info: { version: '0.1.6', latest: '0.1.6', canUpdate: true, app: APP }, install: async (_a, _p, ask?: boolean) => { asked.push(ask); return answers.shift() } })
  await u.refresh()
  await u.updateApp()
  await u.inFront()
  expect(asked).toEqual([true, false])
  expect(toasts.length).toBe(1)
  // Once: the next return to the app starts nothing.
  await u.inFront()
  expect(asked.length).toBe(2)
})

test('back in the app without allowing it, nothing opens again and nothing more is said', async () => {
  const asked: (boolean | undefined)[] = []
  const { u, toasts } = setup({ info: { version: '0.1.6', latest: '0.1.6', canUpdate: true, app: APP }, install: async (_a, _p, ask?: boolean) => { asked.push(ask); return 'needsPermission' } })
  await u.refresh()
  await u.updateApp()
  await u.inFront()
  await u.inFront()
  expect(asked).toEqual([true, false])
  expect(toasts.length).toBe(1)
  expect(u.busy.get()).toBe(null)
})

test('an app update that fails says so', async () => {
  const { u, toasts } = setup({ info: { version: '0.1.6', latest: '0.1.6', canUpdate: true, app: APP }, install: async () => { throw new Error('no network') } })
  await u.refresh()
  await u.updateApp()
  expect(toasts[0].level).toBe('error')
  expect(u.busy.get()).toBe(null)
})

test('automatic updates are the computer\'s to remember', async () => {
  const { u, calls } = setup({ info: { version: '0.1.5', latest: '0.1.5', canUpdate: true, autoUpdate: false } })
  await u.refresh()
  expect(u.info.get()?.autoUpdate).toBe(false)
  await u.setAuto(true)
  const post = calls.find((c) => c.url.endsWith('/api/update/auto'))
  expect(post?.method).toBe('POST')
  expect(post?.body).toBe('{"on":true}')
  expect(post?.auth).toBe('Bearer tok')
  expect(u.info.get()?.autoUpdate).toBe(true)
})

test('the system\'s words for a file under another key become plain ones', async () => {
  expect(installProblem('INSTALL_FAILED_UPDATE_INCOMPATIBLE: Existing package signatures do not match')).toBe('This file isn\'t signed like the app on this phone, so Android won\'t install it over it.')
  expect(installProblem('no space left')).toBe('no space left')
  const { u, toasts } = setup({ info: { version: '0.1.6', latest: '0.1.6', canUpdate: true, app: APP }, install: async () => { throw new Error('INSTALL_FAILED_UPDATE_INCOMPATIBLE: x') } })
  await u.refresh()
  await u.updateApp()
  expect((toasts[0] as any).body).toBe(installProblem('INSTALL_FAILED_UPDATE_INCOMPATIBLE'))
})

test('the computer asks GitHub nothing until a phone answers; the question waits for the second time the app opens', async () => {
  const { u, calls } = setup({ info: { version: '0.1.7', latest: '', canUpdate: true, autoUpdate: false, checks: 'ask' } })
  await u.refresh()
  expect(u.asksChecks(1)).toBe(false)
  expect(u.asksChecks(2)).toBe(true)
  await u.setChecks(true)
  const post = calls.find((c) => c.url.endsWith('/api/update/checks'))
  expect([post?.method, post?.body, post?.auth]).toEqual(['POST', '{"on":true}', 'Bearer tok'])
  expect(u.info.get()?.checks).toBe('on')
  expect(u.asksChecks(2)).toBe(false)
})

test('a yes checks right away, so a new version shows without waiting for the next connection; a no asks nothing', async () => {
  const { u, calls } = setup({ info: { version: '0.1.7', latest: '', canUpdate: true, autoUpdate: false, checks: 'ask' }, app: '0.1.7',
    answers: { '/api/update/check': { status: 200, body: { latest: '0.1.8' } } } })
  await u.refresh()
  await u.setChecks(true)
  expect(calls.filter((c) => c.url.endsWith('/api/update/check')).length).toBe(1)
  expect(u.notice()).toEqual({ kind: 'computer', version: '0.1.8' })
  const no = setup({ info: { version: '0.1.7', latest: '', canUpdate: true, autoUpdate: false, checks: 'ask' } })
  await no.u.refresh()
  await no.u.setChecks(false)
  expect(no.calls.filter((c) => c.url.endsWith('/api/update/check')).length).toBe(0)
})

test('no to checking turns installing by itself off too; a computer from before the question is never asked', async () => {
  const { u } = setup({ info: { version: '0.1.7', latest: '0.1.7', canUpdate: true, autoUpdate: true, checks: 'on' } })
  await u.refresh()
  await u.setChecks(false)
  expect([u.info.get()?.checks, u.info.get()?.autoUpdate]).toEqual(['off', false])
  const old = setup({ info: { version: '0.1.6', latest: '0.1.6', canUpdate: true, autoUpdate: false } })
  await old.u.refresh()
  expect(old.u.info.get()?.checks).toBe(null)
  expect(old.u.asksChecks(5)).toBe(false)
})

test('Check asks the computer to ask GitHub once, and what it found shows at once', async () => {
  const { u, calls } = setup({ info: { version: '0.1.7', latest: '', canUpdate: true, checks: 'off', app: APP }, app: '0.1.7',
    answers: { '/api/update/check': { status: 200, body: { latest: '0.1.8' } } } })
  await u.refresh()
  await u.checkNow()
  expect(calls.filter((c) => c.url.endsWith('/api/update/check')).map((c) => [c.method, c.auth])).toEqual([['POST', 'Bearer tok']])
  expect(u.checked.get()).toEqual({ latest: '0.1.8' })
  expect(u.notice()).toEqual({ kind: 'computer', version: '0.1.8' })
})

test('a check that fails says why', async () => {
  const { u } = setup({ info: { version: '0.1.7', latest: '', canUpdate: true, checks: 'off' },
    answers: { '/api/update/check': { status: 502, body: { error: 'no network' } } } })
  await u.refresh()
  await u.checkNow()
  expect(u.checked.get()).toEqual({ error: 'no network' })
})
