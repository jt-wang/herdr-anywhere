// Where the toast's top goes: on iOS where the system draws its own notification banner. Measured on simulators with
// the frame SpringBoard gives a real banner: iPhone 17 Pro on iOS 26.5, safe area top 62 pt, banner top 59 pt; iPhone
// Air on iOS 27.0, safe area 68 pt, banner 65 pt; iPhone SE (no notch), safe area 20 pt, banner 8 pt, over the status bar.
import { expect, test } from 'nsh/test'
import { toastTop } from './toast-top.ts'

test('iOS: the top of the system notification banner', () => {
  expect([toastTop('ios', 62), toastTop('ios', 68), toastTop('ios', 20)]).toEqual([59, 65, 8])
})

test('Android and the web: below the status bar as before', () => {
  expect([toastTop('android', 24), toastTop('web', 0)]).toEqual([36, 12])
})
