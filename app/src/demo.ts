// Try it, on the pairing screen: the whole app over a made-up computer that lives in memory on the phone, for anyone
// without their own computer at hand, App Review too. No pairing, no network, no tunnel, and nothing saved.
import { Linker, type Link } from './link.ts'
import { PROGRAMS } from './data/programs.ts'
import { startable } from './model/programs.ts'
import { Store } from './store.ts'
import { t } from './strings/index.ts'
import { CLAUDE_EDIT_MENU, FakeCore } from './testing/fakecore.ts'
import { FakeFolders } from './testing/fake-folders.ts'

export type Demo = { link: Link; linker: Linker; store: Store; fetcher: typeof fetch; stop: () => void; /** The sample computer, for tests. */ core: FakeCore }

const ANSWER_FINISHES_MS = 3000
const CODEX_ASKS_MS = 12000

export function makeDemo(): Demo {
  const core = new FakeCore()
  core.echo = true
  const home = core.home
  const doc = { spaces: [] as any[] }
  core.addSpace(doc, 'sp1', `${home}/src/api`, '', [
    { id: 'tb1', name: 'fix login', argv: ['claude'], status: 'blocked', kind: 'permission', msg: `approve Write: ${home}/src/api/login.ts`, screen: CLAUDE_EDIT_MENU },
    { id: 'tb2', argv: ['codex'], status: 'working', screen: ['> run the tests and fix what fails', '', '• Running npm test', '  41 passed, 2 failed', '• Reading src/api/session.ts'].join('\n') },
  ])
  core.addSpace(doc, 'sp2', `${home}/src/web`, 'web', [
    { id: 'tb3', name: 'update docs', argv: ['claude'], status: 'done', screen: ['> update the README for the new login', '', '⏺ Updated README.md: the login section and its example.'].join('\n') },
    { id: 'tb4', argv: ['zsh'], screen: 'sam@studio web % ' },
  ])
  core.docs.set('', { value: doc, version: 1 })

  const timers = new Set<ReturnType<typeof setTimeout>>()
  const later = (ms: number, f: () => void) => {
    const id = setTimeout(() => { timers.delete(id); f() }, ms)
    timers.add(id)
  }
  // An answered agent goes on working, then finishes; what's typed to one reaches its terminal.
  const answered = (addr: string) => {
    const term = core.terms.get(addr)
    if (term?.status?.state !== 'blocked') return
    core.setStatus(addr, 'working', term.status.app)
    later(ANSWER_FINISHES_MS, () => core.terms.get(addr)?.state === 'live' && core.setStatus(addr, 'done', term.status?.app))
  }
  core.onKeys = (addr) => answered(addr)
  // An agent shows what it was told as its own line under its screen (the samples end mid-line), a shell after its prompt.
  core.onSend = (addr, text) => {
    core.output(addr, core.terms.get(addr)?.status?.app ? `\r\n\r\n> ${text}` : `${text}\r\n`)
    answered(addr)
  }
  // A new agent works for a moment and finishes; a shell has no status, as on a real computer.
  const run = core.run.bind(core)
  core.run = (params) => {
    const term = run(params)
    // The phone starts an agent inside the user's shell, with its task last (model/programs.ts agentArgv).
    const argv: string[] = params.argv ?? []
    const cmd = argv.length > 4 && argv[1].startsWith('-') && argv[2].includes('"$@"') ? argv.slice(4) : argv
    // An agent the new-tab menu offers, reporting its status under the name herdr-core gives it (data/programs.ts match).
    const p = startable().find((s) => s.run?.[0] === cmd[0])
    const app = p ? PROGRAMS.programs[p.key].match?.[0] ?? p.key : ''
    if (app) {
      const task = cmd.length > 1 ? cmd[cmd.length - 1] : ''
      core.screens.set(term.address, task ? `> ${task}` : '>')
      core.setStatus(term.address, 'working', app)
      later(ANSWER_FINISHES_MS, () => core.terms.get(term.address)?.state === 'live' && core.setStatus(term.address, 'done', app))
    }
    return term
  }
  // A little after it opens, another agent asks: its alert and sound can be seen and heard.
  later(CODEX_ASKS_MS, () => core.setStatus('t2', 'blocked', 'codex', { kind: 'permission', msg: 'approve Bash: npm test' }))

  const disk = new FakeFolders(home, ['src/api', 'src/web', 'src/notes', 'Documents'], ['src/api', 'src/web'])
  const answer = (status: number, body: unknown) => ({ ok: status < 300, status, json: async () => body })
  const fetcher = (async (url: string, init?: RequestInit) => {
    const file = /\/api\/files\?name=([^&]*)/.exec(url)
    if (file) return answer(200, { path: `~/.cache/herdr-anywhere/uploads/demo/${decodeURIComponent(file[1])}` })
    if (!url.includes('/api/folders')) return answer(200, {})
    if (init?.method === 'POST') return answer(200, { path: await disk.create(JSON.parse(String(init.body)).path), created: true })
    const query = url.split('?')[1] ?? ''
    const path = decodeURIComponent(/(?:^|&)path=([^&]*)/.exec(query)?.[1] ?? '')
    const listed = await disk.list(path, /(?:^|&)hidden=1/.test(query))
    return 'missing' in listed ? answer(404, { error: 'no such folder', ...listed }) : answer(200, listed)
  }) as unknown as typeof fetch

  const link: Link = { url: 'http://demo.invalid', token: 'demo', device: '', name: t('demoComputer'), enc: 'bin', via: 'demo' }
  const linker = new Linker({ load: async () => null, save: () => {}, remove: () => {} })
  linker.link.set(link)
  return {
    link, linker, fetcher, core, store: new Store({ pipe: () => core }),
    stop: () => { for (const id of timers) clearTimeout(id); timers.clear() },
  }
}
