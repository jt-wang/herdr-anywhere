// The picture sheet on a phone with nothing else running: fixtures/fake.tsx's made-up computer, set up by the params
// e2e/zoom-package.sh imports first: e2e/picture-params.ts (the "busy" scene with the sample picture, a drawn sunset,
// open as if its path had just been tapped in t1) or e2e/chat-picture-params.ts (a chat with the sunset in it). The
// zoom scripts (android-zoom.sh, ios-zoom.sh) package it under another id, so an installed Herdr Anywhere stays as it is.
import '../src/fonts.ts'
import '../fixtures/fake.tsx'
import * as zoom from '../src/ui/picture-zoom.ts'

// What the picture's zoom did, as "hm-zoom …" lines in the device's log (android-zoom.sh reads them from logcat); a
// build from before the trace has none.
const trace = (zoom as { zoomTrace?: { log: ((line: string) => void) | null } }).zoomTrace
if (trace) trace.log = (line) => console.log(`hm-zoom ${line}`)
