// What fixtures/fake.tsx reads from `host.params` in a test, set here because e2e/picture-app.tsx is opened from the
// launcher: the "busy" scene with the sample picture's sheet open, in the phone's own theme.
import { host } from 'nsh'
Object.defineProperty(host, 'params', { value: { scene: 'busy', picture: 'art/sunset.png', prefs: JSON.stringify({ opened: 2 }) }, configurable: true })
