import { expect, test } from 'nsh/test'
import { chatApi, Linker } from './link.ts'

test('title requests authenticate, encode addresses, and reject incomplete or unavailable answers', async () => {
  const linker = new Linker({ load: async () => null, save: () => {}, remove: () => {} })
  linker.set({ url: 'http://192.0.2.10:7681', token: 'synthetic', device: 'd', name: 'Example', enc: 'bin' })
  const requests: any[] = []
  const api = (status: number, body: any) => chatApi(linker, (async (url: string, init: any) => {
    requests.push({ url, auth: init.headers.authorization }); return { ok: status === 200, status, json: async () => body }
  }) as any)
  expect(await api(200, { title: ' Saved title ', session: 'opaque' }).title!('remote/t1')).toEqual({ title: 'Saved title', session: 'opaque' })
  expect(requests[0]).toEqual({ url: 'http://192.0.2.10:7681/api/chat/remote%2Ft1/title', auth: 'Bearer synthetic' })
  expect(await api(404, {}).title!('t1')).toEqual({ unsupported: true })
  expect(await api(200, { title: 'Unbound' }).title!('t1')).toEqual({})
  expect(await api(200, { title: '', session: 'opaque' }).title!('t1')).toEqual({})
  expect(await api(401, {}).title!('t1').then(() => false, () => true)).toBe(true)
})

test('a title response is discarded when pairing changes during the request', async () => {
  const linker = new Linker({ load: async () => null, save: () => {}, remove: () => {} })
  const pair = { url: 'http://192.0.2.10:7681', token: 'synthetic', device: 'd', name: 'Example', enc: 'bin' as const }
  linker.set(pair)
  const api = chatApi(linker, (async () => {
    linker.set({ ...pair, token: 'different-synthetic' })
    return { ok: true, status: 200, json: async () => ({ title: 'Old pairing', session: 'opaque' }) }
  }) as any)
  expect(await api.title!('t1')).toEqual({})
})

test('the title transport forwards the cancellation signal', async () => {
  const linker = new Linker({ load: async () => null, save: () => {}, remove: () => {} })
  linker.set({ url: 'http://192.0.2.10:7681', token: 'synthetic', device: 'd', name: 'Example', enc: 'bin' })
  const controller = new AbortController()
  let received: AbortSignal | undefined
  const api = chatApi(linker, ((_url: string, init: RequestInit) => {
    received = init.signal ?? undefined
    return new Promise((_resolve, reject) => { received!.addEventListener('abort', () => reject(new Error('aborted'))) })
  }) as any)
  const waiting = api.title!('t1', controller.signal).then(() => false, () => true)
  for (let i = 0; i < 8 && !received; i++) await Promise.resolve()
  expect(received).toBe(controller.signal)
  controller.abort()
  expect(await waiting).toBe(true)
})

test('pairing changes during URL resolution never send a token to the previous host', async () => {
  const linker = new Linker({ load: async () => null, save: () => {}, remove: () => {} })
  const pair = { url: 'http://192.0.2.10:7681', token: 'synthetic', device: 'd', name: 'Example', enc: 'bin' as const }
  linker.set(pair)
  let resolve!: (url: string) => void
  linker.baseUrl = () => new Promise<string>(r => { resolve = r })
  let calls = 0
  const api = chatApi(linker, (async () => { calls++; return { ok: true, status: 200, json: async () => ({}) } }) as any)
  const pending = api.title!('t1')
  linker.set({ ...pair, url: 'http://192.0.2.20:7681', token: 'new-synthetic' })
  resolve(pair.url)
  expect(await pending).toEqual({})
  expect(calls).toBe(0)
})
