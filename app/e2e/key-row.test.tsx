// The key row stays on one line: every label on one line and whole, on a real phone's width and text size.
// `nsh test --target android e2e/key-row.test.tsx` (the device adb lists first); e2e/key-row.sh runs it at 360 dp
// and 411 dp and at the largest text size, and reads the same gallery built as the iOS app on the iPhone SE (375 pt).
// Headless runs lay out no text, so the test needs a real target. Saves e2e/shots/key-row.png.
import { expect, open, test } from 'nsh/test'
import { KEY_ROW_LOOK } from '../src/ui/pane-bar.tsx'
import { LOOKS } from './key-row-gallery.tsx'

test('the key row fits on one line, in terminal mode, with Shift and Ctrl held, and under the input box', async () => {
  const app = await open('./key-row-gallery.tsx')
  const verdict = (name: string) => app.texts().find((t) => t.startsWith(`${name}: `))?.slice(name.length + 2) ?? 'missing'
  await app.waitUntil(() => LOOKS.every((l) => verdict(l.name) !== 'waiting'), { timeout: 10000 })
  const png = await app.screenshot('e2e/shots/key-row.png')
  if (!png.length) throw new Error('no layout headless: run with --target android or --target ios')
  for (const l of LOOKS) console.log(`${l.name}: ${verdict(l.name)}`)
  // The look in use: every label on one line and whole (esc or tab broken over two lines, or cut to "e…", fails).
  // The equal-width looks cut esc and tab on the iPhone SE at the largest text (2026-11-07): their keys are about
  // 30 pt wide there.
  expect(verdict(`${KEY_ROW_LOOK.labels}-${KEY_ROW_LOOK.size}`)).toBe('fits')
}, 60000)
