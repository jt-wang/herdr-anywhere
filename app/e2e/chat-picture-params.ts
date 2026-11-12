// What fixtures/fake.tsx reads from `host.params` for the zoom scripts' chat run (CHAT=1, e2e/zoom-package.sh): one
// Claude Code pane that opens in its chat view, where the prompt was sent with the drawn sunset (fixtures/picture.ts).
// Made-up words and path only.
import { host } from 'nsh'
const turns = [{ id: 'u1', from: 'you', said: 'What is in this picture?', images: [{ id: 'u1.1', source: '/work/pics/sunset.png' }],
  end: 'done', secs: 12, reply: 'A sunset over mountains, reflected in a lake.', steps: [] }]
Object.defineProperty(host, 'params', { value: { scene: 'single', argv: 'claude', status: 'done', chatPicture: '1',
  chat: JSON.stringify({ chat: true, turns }), prefs: JSON.stringify({ opened: 2, chatFirst: true }) }, configurable: true })
