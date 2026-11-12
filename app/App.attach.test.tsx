// Photos and files from the phone: they land on the computer, and their path goes in the composer, to send with what
// you say about them.
import { expect, open, test } from 'nsh/test'

const FILE = { id: 'f1', name: 'notes.txt', size: 5 }

const composer = async (handlers: Record<string, unknown> = {}) => {
  const app = await open('./fixtures/fake.tsx', { host: { params: { scene: 'busy' }, handlers: { 'page.pickFiles': [FILE], 'page.uploadFile': { status: 200, body: '{"path":"/up/notes.txt"}' }, ...handlers } } })
  await app.waitFor('text:Needs you')
  await app.press('id:space-sp2')
  await app.waitFor('id:attach')
  return app
}
const puts = (app: any) => JSON.parse(app.storage()['fake-put'] ?? '[]')
const sent = (app: any) => JSON.parse(app.storage()['fake-sent'] ?? '[]')

test('the composer offers to attach, with text too; send comes with the text', async () => {
  const app = await composer()
  expect(app.count('id:show-keyboard')).toBe(0)
  expect(app.count('id:compose-send')).toBe(0)
  await app.type('id:compose-input', 'hello')
  await app.waitFor('id:compose-send')
  expect(app.count('id:attach')).toBe(1)
})

test('a photo goes to the computer over HTTP, its path in the composer, sent with the text after it', async () => {
  const app = await composer()
  app.onPick(() => [{ name: 'shot.png', type: 'image/png', data: new Uint8Array([1, 2, 3]) }])
  await app.press('id:attach')
  await app.waitFor('id:menu')
  await app.press('id:menu-photo')
  await app.waitUntil(() => puts(app).length === 1)
  expect(puts(app)[0]).toEqual(['shot.png', '', 3])
  // Scaled down on the phone: an agent needs no 48 megapixels.
  expect(app.picks()[0].maxDimension).toBe(2048)
  expect(app.picks()[0].multiple).toBe(true)
  await app.waitFor('id:compose-send')
  await app.press('id:compose-send')
  await app.waitUntil(() => sent(app).length === 1)
  expect(sent(app)[0][1].trim()).toBe('/fake/uploads/shot.png')
})

test('a file goes through page.uploadFile with the address and the token, and its path comes back', async () => {
  const uploads: any[] = []
  const app = await composer({ 'page.uploadFile': (a: any) => { uploads.push(a); return { status: 200, body: JSON.stringify({ path: '/up/notes.txt' }) } } })
  await app.press('id:attach')
  await app.waitFor('id:menu')
  await app.press('id:menu-file')
  await app.waitFor('id:compose-send')
  expect(uploads.length).toBe(1)
  expect(uploads[0].id).toBe('f1')
  expect(uploads[0].url.endsWith('/api/files?name=notes.txt')).toBe(true)
  await app.press('id:compose-send')
  await app.waitUntil(() => sent(app).length === 1)
  expect(sent(app)[0][1].trim()).toBe('/up/notes.txt')
})

test('a file over the limit says so and is not sent; the other file still is', async () => {
  const big = { id: 'b', name: 'big.zip', size: 3 * 1024 * 1024 * 1024 }
  const uploads: any[] = []
  const app = await composer({ 'page.pickFiles': [big, FILE], 'page.uploadFile': (a: any) => { uploads.push(a); return { status: 200, body: '{"path":"/up/notes.txt"}' } } })
  await app.press('id:attach')
  await app.waitFor('id:menu')
  await app.press('id:menu-file')
  await app.waitFor('text:big.zip is over 2 GB')
  await app.waitUntil(() => uploads.length === 1)
  expect(uploads[0].id).toBe('f1')
})

test('a companion from before uploads says to update it', async () => {
  const app = await composer({ 'page.uploadFile': { status: 404, body: '' } })
  await app.press('id:attach')
  await app.waitFor('id:menu')
  await app.press('id:menu-file')
  await app.waitFor('text:notes.txt: Update Herdr Anywhere on the computer to attach files.')
})

test('cancelling either picker changes nothing', async () => {
  const app = await composer({ 'page.pickFiles': [] })
  await app.press('id:attach')
  await app.waitFor('id:menu')
  await app.press('id:menu-file')
  await app.wait(100)
  app.onPick(() => null)
  await app.press('id:attach')
  await app.waitFor('id:menu')
  await app.press('id:menu-photo')
  await app.wait(100)
  expect(puts(app)).toEqual([])
  expect(app.count('id:attach')).toBe(1)
})

test('a tap on the terminal is not how to type there any more: ⋯ offers it', async () => {
  const app = await composer()
  await app.press('id:space-more')
  await app.waitFor('id:menu')
  expect(app.count('id:menu-type')).toBe(1)
})
