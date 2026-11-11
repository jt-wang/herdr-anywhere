// A picture whose path was tapped in a terminal: its sheet (ui/picture-sheet.tsx). The tap itself is the page's
// (e2e/page-touch.mjs); here the sample computer opens the sheet as the tap does (fixtures/fake.tsx `picture`).
import { expect, open, test } from 'nsh/test'

const opened = (picture: string, handlers: Record<string, unknown> = { 'page.copyImage': true }, more: object = {}) =>
  open('./fixtures/fake.tsx', { host: { params: { scene: 'busy', picture }, handlers }, ...more })

test('a tapped path opens its picture under the full path, asked for with its terminal', async () => {
  const app = await opened('art/sunset.png')
  await app.waitFor('id:picture-image')
  // The path the computer read, after the one tapped (a text: selector starting with / is a regex).
  expect(app.texts()).toContain('/Users/sam/src/api/art/sunset.png')
  expect(app.texts()).not.toContain('art/sunset.png')
  expect(JSON.parse(app.storage()['fake-pictures'])).toEqual(['t1 art/sunset.png'])
  expect(app.shows('text:Touch and hold the picture to copy it')).toBe(true)
})

test('Copy copies the path; holding the picture copies the picture', async () => {
  const app = await opened('~/Desktop/shot.png')
  await app.waitFor('id:picture-image')
  await app.press('id:picture-copy-path')
  expect(app.clipboard()).toBe('/Users/sam/Desktop/shot.png')
  await app.longPress('id:picture')
  await app.waitFor('text:Picture copied')
  const call = app.host.calls().find((c: any) => c.name === 'page.copyImage') as any
  expect(String(call?.data?.url).startsWith('data:image/png;base64,')).toBe(true)
})

test("a picture the phone couldn't copy says so", async () => {
  const app = await opened('a.png', { 'page.copyImage': false })
  await app.waitFor('id:picture-image')
  await app.longPress('id:picture')
  await app.waitFor("text:Couldn't copy the picture")
})

test('the computer says why it shows no picture, in the phone\'s language', async () => {
  const app = await opened('notes.png')
  await app.waitFor("text:This file isn't a picture.")
  expect(app.shows('id:picture-image')).toBe(false)
  const zh = await opened('gone.png', undefined, { locale: 'zh-CN' })
  await zh.waitFor('text:电脑上没有这个文件。')
})

test('closing the sheet lets the picture go', async () => {
  const app = await opened('a.png')
  await app.waitFor('id:picture-image')
  await app.press('id:picture-close')
  await app.waitUntil(() => !app.shows('id:picture-sheet'))
  expect(app.shows('id:picture-image')).toBe(false)
})
