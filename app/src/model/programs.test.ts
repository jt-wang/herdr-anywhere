// Ported from herdr-app src/programs.rs tests.
import { expect, test } from 'nsh/test'
import { programOf, startable, agentArgv, iconTinted } from './programs.ts'

const key = (app: string | undefined, argv: string[]) => programOf(app, argv).key

test('agents, shells, tools and what interpreters run', () => {
  expect(key('claude-code', ['/bin/zsh'])).toBe('claude')
  expect(key('newagent', [])).toBe('agent')
  expect(key(undefined, ['/bin/zsh'])).toBe('shell')
  expect(key(undefined, ['-zsh'])).toBe('shell')
  expect(key(undefined, ['nvim', 'x.rs'])).toBe('nvim')
  expect(key(undefined, ['vi'])).toBe('vim')
  expect(key(undefined, ['node', '/x/node_modules/.bin/vite', 'dev'])).toBe('vite')
  expect(key(undefined, ['npx', 'vitest'])).toBe('vitest')
  expect(key(undefined, ['node', 'server.js'])).toBe('node')
  expect(key(undefined, ['python3', '-m', 'http.server', '8799'])).toBe('python')
  expect(key(undefined, ['uv', 'run', 'pytest'])).toBe('pytest')
  expect(key(undefined, ['mytool'])).toBe('program')
  expect(key(undefined, ['/bin/zsh', '-ic', '"$@"; exec "$0" -i', '/bin/zsh', 'vi', 'x'])).toBe('vim')
})

test('names: the table name, a shell its own name, an unknown program its command', () => {
  expect(programOf('claude-code', []).name).toBe('Claude Code')
  expect(programOf(undefined, ['/bin/zsh']).name).toBe('zsh')
  expect(programOf(undefined, ['mytool']).name).toBe('mytool')
  expect(programOf('claude-code', []).agent).toBe(true)
})

test('startable agents come first in herdr order', () => {
  expect(startable().slice(0, 4).map((p) => p.key)).toEqual(['claude', 'codex', 'pi', 'opencode'])
  expect(startable()[0].run).toEqual(['claude'])
})

test('an agent runs in a shell that stays after it exits, the task last', () => {
  expect(agentArgv(['claude'], ' fix it ', undefined, '/bin/zsh')).toEqual(['/bin/zsh', '-ic', '"$@"; exec "$0" -i', '/bin/zsh', 'claude', 'fix it'])
  // Another machine's agent starts in that machine's own interactive shell, as the menu's probe asked it there: an
  // agent on a PATH only ~/.zshrc sets is listed and starts (measured over ssh with sshd, zsh and omp).
  expect(agentArgv(['codex'], '', 'vps', '/bin/zsh')).toEqual(['sh', '-c', 'exec "${SHELL:-sh}" -ic \'"$@"; exec "$0" -i\' "${SHELL:-sh}" "$@"', 'sh', 'codex'])
  expect(agentArgv(undefined, 'x', undefined, '/bin/zsh')).toBe(undefined)
})

test('an icon herdr tints is one colour, drawn in the text colour; the others keep their own colours', () => {
  expect(iconTinted('shell')).toBe(true)
  expect(iconTinted('copilot')).toBe(true)
  expect(iconTinted('claude')).toBe(false)
  expect(iconTinted('codex')).toBe(false)
  expect(iconTinted(undefined)).toBe(true)
})

test('Kimi Code reports itself as kimi-code and shows as Kimi, with its icon (measured with Kimi 2.4.1)', () => {
  const p = programOf('kimi-code', ['kimi'])
  expect([p.key, p.name, p.icon, p.agent]).toEqual(['kimi', 'Kimi', 'kimi', true])
})

test('Oh My Pi reports itself as omp and shows as Oh My Pi, with its icon, in the new-tab menu (measured with omp 18.18.3 in herdr-core 0.1.20)', () => {
  const p = programOf('omp', ['/bin/zsh'])
  expect([p.key, p.name, p.icon, p.agent]).toEqual(['omp', 'Oh My Pi', 'omp', true])
  expect(key(undefined, ['omp'])).toBe('omp')
  expect(key('oh-my-pi', [])).toBe('omp')
  expect(key('pi', [])).toBe('pi')
  expect(startable().some((s) => s.key === 'omp' && s.run!.join(' ') === 'omp')).toBe(true)
})
