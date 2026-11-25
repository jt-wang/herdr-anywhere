import { expect, test } from 'nsh/test'
import { imageApi } from './link.ts'
import { Signal } from './store.ts'

for (const mode of ['read', 'check'] as const) {
  test(`picture ${mode}: pairing change during base lookup sends no old request`, async () => {
    let base!: (url: string) => void
    const link = new Signal({ token: 'fake-first' })
    const linker = { link, baseUrl: () => new Promise<string>(r => { base = r }) }
    let sent = 0
    const api = imageApi(linker as any, (async () => { sent++; return { ok: true, status: 200, json: async () => ({ path: 'sample.png', url: 'data:image/png;base64,eA==', paths: [] }) } }) as any)
    const request = mode === 'read' ? api.read('sample.png', 't1') : api.check('t1', ['sample.png'])
    await Promise.resolve(); link.set({ token: 'fake-second' }); base('http://127.0.0.1:1')
    await request.catch(() => {})
    expect(sent).toBe(0)
  })
  test(`picture ${mode}: abort during response body settles without waiting for body`, async () => {
    const link = new Signal({ token: 'fake' })
    const controller = new AbortController()
    let reading!: () => void
    const started = new Promise<void>(r => { reading = r })
    const api = imageApi({ link, baseUrl: async () => 'http://127.0.0.1:1' } as any, (async () => ({ ok: true, status: 200, json: () => { reading(); return new Promise(() => {}) } })) as any)
    const request = mode === 'read' ? api.read('sample.png', 't1', controller.signal) : api.check('t1', ['sample.png'], controller.signal)
    await started; controller.abort()
    expect(await request.then(() => 'ok', () => 'cancelled')).toBe('cancelled')
  })
}

for (const mode of ['read', 'check'] as const) test(`picture ${mode}: immediate pairing switch cannot send the old path to a new computer`, async () => {
  const link = new Signal({ token: 'fake-first' }); let sent = 0
  const api = imageApi({ link, baseUrl: async () => 'http://127.0.0.1:1' } as any, (async () => { sent++; return { status: 200, ok: true, json: async () => ({ paths: [] }) } }) as any)
  const pending = mode === 'read' ? api.read('sample.png', 't1') : api.check('t1', ['sample.png'])
  link.set({ token: 'fake-second' })
  await pending.catch(() => {})
  expect(sent).toBe(0)
})
