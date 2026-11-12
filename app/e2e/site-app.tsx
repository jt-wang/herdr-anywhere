// The website's phone pictures on a phone: fixtures/fake.tsx's "site" scene (a made-up computer) as an app of its
// own, with the app's fonts and theme. The website's shot script packages it under another id, so an installed
// Herdr Anywhere stays as it is, and takes the pictures in each language and theme.
import './site-params.ts'
import '../src/fonts.ts'
import { core } from '../fixtures/fake.tsx'

// Claude Code asks too, once the first two pictures are taken (the shot script waits for its menu).
export const CLAUDE_ASKS = [' Bash command', '', '   npm test', '   Run the test suite', '', ' Do you want to proceed?',
  ' ❯ 1. Yes', `   2. Yes, and don't ask again for npm test commands in ${core.home}/src/acme-api`, '   3. No, and tell Claude what to do differently (esc)', ''].join('\n')
setTimeout(() => {
  core.screens.set('t1', CLAUDE_ASKS)
  core.setStatus('t1', 'blocked', 'claude-code', { kind: 'permission', msg: 'Bash: npm test' })
}, 24000)
