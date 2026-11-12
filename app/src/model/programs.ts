// How a terminal shows: herdr-app's program table (defaults/programs.json) and its resolution (src/programs.rs).
import { PROGRAMS } from '../data/programs.ts'

export type Program = {
  key: string
  name: string
  icon?: string
  tint: boolean
  tile?: string
  color?: string
  shell: boolean
  agent: boolean
  run?: string[]
}

type Entry = (typeof PROGRAMS.programs)[string]

const AGENTS_FIRST = ['claude', 'codex', 'pi', 'opencode', 'cursor', 'copilot', 'amp', 'gemini', 'grok']
const PALETTE = ['#6d5bd0', '#2f80ed', '#0f9d8a', '#d9822b', '#c2417d', '#3a7d44', '#8a5a44', '#4e5d94']

function base(s: string) {
  let b = s.split(/[/\\]/).pop() ?? s
  b = b.replace(/^-+/, '')
  return b.toLowerCase().endsWith('.exe') ? b.slice(0, -4) : b
}

function find(name: string): [string, Entry] | undefined {
  const lower = name.toLowerCase()
  const all = Object.entries(PROGRAMS.programs)
  return all.find(([k]) => k === name || k.toLowerCase() === lower)
    ?? all.find(([, e]) => (e.match ?? []).some((a) => a === name || a.toLowerCase() === lower))
}

function shown(key: string, e: Entry | undefined, name: string | undefined, shell: boolean, agent: boolean): Program {
  return {
    key,
    name: name ?? e?.name ?? key,
    icon: e?.icon?.replace(/^icons\//, '').replace(/\.svg$/, ''),
    tint: e?.tint ?? true,
    tile: e?.tile,
    color: e?.color,
    shell,
    agent,
  }
}

function hashColor(s: string) {
  let h = 2166136261
  for (const b of new TextEncoder().encode(s)) h = Math.imul(h ^ b, 16777619) >>> 0
  return PALETTE[h % PALETTE.length]
}

/** The program a terminal shows as: the agent's own report (`app`), else what it runs. */
export function programOf(app: string | undefined | null, argv: string[]): Program {
  if (app) {
    const f = find(app)
    return f ? shown(f[0], f[1], undefined, false, true) : shown('agent', PROGRAMS.programs.agent, app, false, true)
  }
  if (argv.length > 4 && PROGRAMS.shells.includes(base(argv[0])) && argv[1].startsWith('-') && argv[1].endsWith('c') && argv[2].includes('"$@"')) {
    return programOf(undefined, argv.slice(4))
  }
  const first = argv[0] !== undefined ? base(argv[0]) : undefined
  if (first === undefined) return shown('shell', PROGRAMS.programs.shell, undefined, true, false)
  if (PROGRAMS.interpreters.includes(first)) {
    for (const arg of argv.slice(1).filter((a) => !a.startsWith('-'))) {
      const name = base(arg)
      const stem = ['.js', '.mjs', '.cjs', '.ts', '.py', '.rb'].reduce((s, x) => (s.endsWith(x) ? s.slice(0, -x.length) : s), name)
      const f = find(stem)
      if (f && !PROGRAMS.interpreters.includes(f[0])) return shown(f[0], f[1], undefined, false, false)
    }
  }
  const f = find(first)
  if (f) return shown(f[0], f[1], undefined, false, false)
  if (PROGRAMS.shells.includes(first)) return shown('shell', PROGRAMS.programs.shell, first, true, false)
  const e = { ...(PROGRAMS.programs.program ?? {}), tile: hashColor(first), color: '#ffffff' }
  return shown('program', e, first, false, false)
}

const UNTINTED = new Set(Object.values(PROGRAMS.programs).filter((e) => e.tint === false).map((e) => e.icon?.replace(/^icons\//, '').replace(/\.svg$/, '')))
/** Whether herdr draws this icon in one colour, the text's (herdr.d.ts `tint`), rather than in its own colours. */
export function iconTinted(icon: string | undefined): boolean {
  return !UNTINTED.has(icon)
}

/** Agents the new-tab menu can start, herdr's favourites first. */
export function startable(): Program[] {
  const keys = Object.entries(PROGRAMS.programs).filter(([, e]) => (e.run ?? []).length).map(([k]) => k)
  const rank = (k: string) => (AGENTS_FIRST.includes(k) ? AGENTS_FIRST.indexOf(k) : AGENTS_FIRST.length)
  keys.sort((a, b) => rank(a) - rank(b) || (a < b ? -1 : a > b ? 1 : 0))
  return keys.map((k) => ({ ...shown(k, PROGRAMS.programs[k], undefined, false, true), run: [...PROGRAMS.programs[k].run!] }))
}

/** An agent with its first message, in a shell that stays when the agent exits (herdr-app window.rs `agent_argv`). */
export function agentArgv(argv: string[] | undefined, task: string | undefined, on: string | undefined, shell: string): string[] | undefined {
  if (!argv || !argv.length) return undefined
  const t = task?.trim()
  const cmd = t ? [...argv, t] : [...argv]
  // Another machine: its own shell, interactive like this computer's, so its ~/.zshrc PATH is there (the menu's probe
  // asks the same way, store.ts refreshInstalled). sh when it has no SHELL (a container reached with docker exec).
  if (on) return ['sh', '-c', 'exec "${SHELL:-sh}" -ic \'"$@"; exec "$0" -i\' "${SHELL:-sh}" "$@"', 'sh', ...cmd]
  return [shell, '-ic', '"$@"; exec "$0" -i', shell, ...cmd]
}

/** The `command -v` probe herdr-app runs to see which agents are installed (programs.rs `installed`). */
export const INSTALLED_PROBE = 'for c; do command -v "$c" >/dev/null 2>&1 && echo "herdr-has:$c"; done'
export function parseInstalled(out: string, commands: string[]): string[] {
  const found = new Set(out.split('herdr-has:').slice(1).map((s) => s.trim().split(/\s+/)[0]))
  return commands.filter((c) => found.has(c))
}
