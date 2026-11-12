// The menus and the toast on a phone with nothing else running, for e2e/ios-menus.sh: fixtures/fake.tsx's
// "busy" scene (a made-up computer) in the phone's own theme. 25 s after launch, and every 6 s after, Codex (t2) asks
// again, so a toast shows while another space is open; the menu pictures are taken before that, each from a fresh launch.
import { host } from 'nsh'
Object.defineProperty(host, 'params', { value: { scene: 'busy', prefs: JSON.stringify({ opened: 2 }) }, configurable: true })
import '../assets/fonts/Geist.ttf'
import '../assets/fonts/JetBrainsMono-Variable.ttf'
import { core } from '../fixtures/fake.tsx'

let n = 0
setTimeout(() => setInterval(() => {
  const ask = n++ % 2 === 0
  core.setStatus('t2', ask ? 'blocked' : 'working', core.terms.get('t2')?.status?.app, ask ? { kind: 'permission', msg: 'approve Bash: npm test' } : {})
}, 3000), 25000)
